package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Secrets keeps the values of settings a manifest marks secret, outside the
// plugin's folder, in the OS credential store. The app passes mygo.Secrets;
// tests pass a memory store.
type Secrets interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

// ErrNotInstalled reports a plugin id with no folder.
var ErrNotInstalled = errors.New("plugin is not installed")

// Installed is one plugin in the store. Signed is set when it came from a
// registry and its files still match the hash that was signed; Source is
// where it came from. Err is set when its files cannot be read, so the
// manager can show a broken plugin instead of hiding it.
type Installed struct {
	Manifest Manifest
	Dir      string
	Enabled  bool
	Signed   bool
	Source   string
	Err      string
}

// Store owns the plugins folder: one directory per plugin, holding
// plugin.json, its source, and state.json. The folder is the install unit;
// removing it removes the plugin.
type Store struct {
	dir     string
	secrets Secrets
}

// NewStore returns a store rooted at dir, which is created on first write.
// secrets may be nil, in which case secret settings read as empty and
// cannot be saved.
func NewStore(dir string, secrets Secrets) *Store {
	return &Store{dir: dir, secrets: secrets}
}

// Dir returns the plugins folder.
func (s *Store) Dir() string { return s.dir }

// List returns every installed plugin, sorted by id. A folder whose
// manifest cannot be read is returned with Err set.
func (s *Store) List() ([]Installed, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Installed, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		dir := filepath.Join(s.dir, name)
		m, err := LoadManifest(dir)
		if err != nil {
			out = append(out, Installed{Dir: dir, Err: err.Error()})
			continue
		}
		st, _ := loadState(dir)
		inst := Installed{Manifest: m, Dir: dir, Enabled: st.Enabled}
		if err := verifyInstall(dir, &inst); err != nil {
			inst.Err = err.Error()
		}
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.ID < out[j].Manifest.ID
	})
	return out, nil
}

// Get returns one installed plugin.
func (s *Store) Get(id string) (Installed, error) {
	dir := filepath.Join(s.dir, id)
	m, err := LoadManifest(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Installed{}, ErrNotInstalled
	}
	if err != nil {
		return Installed{}, err
	}
	st, _ := loadState(dir)
	inst := Installed{Manifest: m, Dir: dir, Enabled: st.Enabled}
	if err := verifyInstall(dir, &inst); err != nil {
		inst.Err = err.Error()
	}
	return inst, nil
}

// verifyInstall reads a plugin's install record and, for a signed plugin,
// checks that its files still are what was signed. A plugin installed from
// a folder has no record and is left unsigned.
func verifyInstall(dir string, inst *Installed) error {
	info, err := loadInstallInfo(dir)
	if err != nil {
		return fmt.Errorf("install record: %w", err)
	}
	if info.Hash == "" {
		return nil
	}
	inst.Signed = true
	inst.Source = info.Source
	digest, err := DigestDir(dir)
	if err != nil {
		return err
	}
	if digest != info.Hash {
		return errors.New("the plugin's files changed after it was installed")
	}
	return nil
}

// Install copies the plugin in src into the store and enables it. A local
// folder is unsigned — it has no registry entry to check it against — and
// the shell marks such a plugin wherever it draws it.
func (s *Store) Install(src string) (Manifest, error) {
	m, err := LoadManifest(src)
	if err != nil {
		return Manifest{}, err
	}
	if err := m.ValidateDir(src); err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Manifest{}, err
	}
	dest := filepath.Join(s.dir, m.ID)
	if _, err := os.Stat(dest); err == nil {
		return Manifest{}, fmt.Errorf("plugin %s is already installed", m.ID)
	}
	tmp, err := os.MkdirTemp(s.dir, ".install-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		return Manifest{}, err
	}
	if err := m.ValidateDir(tmp); err != nil {
		return Manifest{}, err
	}
	if err := saveState(tmp, state{Enabled: true}); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// RemoveAt deletes one folder the store lists, including a folder whose
// manifest cannot be read. The folder must sit directly under the store.
func (s *Store) RemoveAt(dir string) error {
	clean := filepath.Clean(dir)
	if filepath.Dir(clean) != filepath.Clean(s.dir) {
		return errors.New("not a plugin folder of this store")
	}
	return os.RemoveAll(clean)
}

// Remove deletes the plugin's folder and the secrets of its settings.
func (s *Store) Remove(id string) error {
	dir := filepath.Join(s.dir, id)
	m, err := LoadManifest(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if s.secrets != nil {
		for _, field := range m.Settings {
			if field.Secret {
				_ = s.secrets.Delete(secretKey(id, field.Key))
			}
		}
	}
	return nil
}

// SetEnabled turns a plugin on or off.
func (s *Store) SetEnabled(id string, enabled bool) error {
	dir := filepath.Join(s.dir, id)
	if _, err := os.Stat(dir); err != nil {
		return ErrNotInstalled
	}
	st, err := loadState(dir)
	if err != nil {
		return err
	}
	st.Enabled = enabled
	return saveState(dir, st)
}

// Setting returns the value of a declared setting: from the OS credential
// store when the manifest marks it secret, else from state.json.
func (s *Store) Setting(id, key string) (string, error) {
	m, err := LoadManifest(filepath.Join(s.dir, id))
	if err != nil {
		return "", err
	}
	field, ok := m.setting(key)
	if !ok {
		return "", fmt.Errorf("setting %q is not declared in plugin.json", key)
	}
	if field.Secret {
		if s.secrets == nil {
			return "", nil
		}
		value, err := s.secrets.Get(secretKey(id, key))
		if err != nil {
			return "", nil
		}
		return string(value), nil
	}
	st, err := loadState(filepath.Join(s.dir, id))
	if err != nil {
		return "", err
	}
	return st.Settings[key], nil
}

// SetSetting saves a declared setting. A secret goes to the credential
// store and never to state.json.
func (s *Store) SetSetting(id, key, value string) error {
	m, err := LoadManifest(filepath.Join(s.dir, id))
	if err != nil {
		return err
	}
	field, ok := m.setting(key)
	if !ok {
		return fmt.Errorf("setting %q is not declared in plugin.json", key)
	}
	if field.Secret {
		if s.secrets == nil {
			return errors.New("no credential store is available")
		}
		return s.secrets.Set(secretKey(id, key), []byte(value))
	}
	dir := filepath.Join(s.dir, id)
	st, err := loadState(dir)
	if err != nil {
		return err
	}
	if st.Settings == nil {
		st.Settings = map[string]string{}
	}
	st.Settings[key] = value
	return saveState(dir, st)
}

// KV opens a plugin's key-value store, enforcing its quota.
func (s *Store) KV(id string) (*KV, error) {
	m, err := LoadManifest(filepath.Join(s.dir, id))
	if err != nil {
		return nil, err
	}
	return &KV{
		path:  filepath.Join(s.dir, id, "kv.json"),
		quota: m.Permissions.StorageKb * 1024,
	}, nil
}

func (m Manifest) setting(key string) (SettingField, bool) {
	for _, field := range m.Settings {
		if field.Key == key {
			return field, true
		}
	}
	return SettingField{}, false
}

func secretKey(id, key string) string { return id + "/" + key }

// state is the per-plugin file the shell owns: whether the plugin runs, and
// the values of its non-secret settings.
type state struct {
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings,omitempty"`
}

func loadState(dir string) (state, error) {
	var st state
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return state{}, err
	}
	return st, nil
}

func saveState(dir string, st state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Join(dir, "state.json"), data, 0o600)
}

// copyTree copies src into dst, refusing symlinks and bounding what one
// plugin may bring. The shell's own files and a file manager's junk are not
// copied: a folder that was itself installed elsewhere stays unsigned.
func copyTree(src, dst string) error {
	files, total := 0, int64(0)
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "__MACOSX" {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, filepath.FromSlash(rel)), 0o755)
		}
		if isShellFile(rel) || isJunk(rel) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("plugin contains a symlink: %s", rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("plugin contains a special file: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxPluginFile {
			return fmt.Errorf("%s is larger than %d bytes", rel, maxPluginFile)
		}
		files++
		total += info.Size()
		if files > maxPluginFiles {
			return fmt.Errorf("plugin has more than %d files", maxPluginFiles)
		}
		if total > maxPluginTotal {
			return fmt.Errorf("plugin is larger than %d bytes", maxPluginTotal)
		}
		return copyFile(p, filepath.Join(dst, filepath.FromSlash(rel)), info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// KV is one plugin's key-value store: JSON values under string keys, kept
// in kv.json, bounded by the manifest's storageKb.
type KV struct {
	path  string
	quota int

	mu     sync.Mutex
	loaded bool
	data   map[string]json.RawMessage
	size   int
}

const (
	maxKVKey   = 128
	maxKVValue = 64 << 10
)

// Get returns the value stored under key.
func (kv *KV) Get(key string) (json.RawMessage, bool) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.load()
	value, ok := kv.data[key]
	return value, ok
}

// Put stores value under key, refusing a write that would pass the quota.
func (kv *KV) Put(key string, value json.RawMessage) error {
	if len(key) == 0 || len(key) > maxKVKey {
		return fmt.Errorf("key must be 1 to %d bytes", maxKVKey)
	}
	if len(value) > maxKVValue {
		return fmt.Errorf("value is larger than %d bytes", maxKVValue)
	}
	if kv.quota == 0 {
		return errors.New("plugin has no storage permission")
	}
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.load()
	old := len(kv.data[key]) + len(key)
	next := kv.size - old + len(value) + len(key)
	if next > kv.quota {
		return fmt.Errorf("storage quota of %d bytes is full", kv.quota)
	}
	if kv.data == nil {
		kv.data = map[string]json.RawMessage{}
	}
	kv.data[key] = value
	kv.size = next
	return kv.save()
}

// Delete removes key.
func (kv *KV) Delete(key string) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.load()
	if _, ok := kv.data[key]; !ok {
		return nil
	}
	kv.size -= len(kv.data[key]) + len(key)
	delete(kv.data, key)
	return kv.save()
}

// Keys returns the stored keys, for tests and the manager.
func (kv *KV) Keys() []string {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.load()
	out := make([]string, 0, len(kv.data))
	for key := range kv.data {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (kv *KV) load() {
	if kv.loaded {
		return
	}
	kv.loaded = true
	data, err := os.ReadFile(kv.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &kv.data); err != nil {
		kv.data = nil
		return
	}
	for key, value := range kv.data {
		kv.size += len(key) + len(value)
	}
}

func (kv *KV) save() error {
	data, err := json.Marshal(kv.data)
	if err != nil {
		return err
	}
	return writeFileAtomic(kv.path, data, 0o600)
}
