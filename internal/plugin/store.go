package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrBrokenManifest is the error a folder gets when its plugin.json cannot
// be read. The shell translates it; other errors stay as the checker wrote them.
const ErrBrokenManifest = "the app manifest could not be read"

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
// registry and its files still match the hash that was signed. Official is
// set when it is a first-party example shipped with the shell and its files
// still match the copy embedded in the app. Source is where it came from.
// Err is set when its files cannot be read, so the manager can show a broken
// plugin instead of hiding it.
type Installed struct {
	Manifest     Manifest
	Dir          string
	Enabled      bool
	Signed       bool
	Official     bool
	Source       string
	InstalledVia string
	Err          string
}

// Store owns the plugins folder: one directory per plugin, holding
// plugin.json, its source, and state.json. The folder is the install unit;
// removing it removes the plugin.
type Store struct {
	dir           string
	secrets       Secrets
	provenanceMu  sync.Mutex
	provenanceMem map[string][]byte
	provBackoff   time.Time
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
			out = append(out, Installed{Dir: dir, Err: ErrBrokenManifest})
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
	if err := validatePluginID(id); err != nil {
		return Installed{}, err
	}
	dir := filepath.Join(s.dir, id)
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Installed{}, ErrNotInstalled
	}
	if err != nil {
		return Installed{}, err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return Installed{}, errors.New("invalid plugin directory")
	}
	m, err := LoadManifest(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Installed{}, ErrNotInstalled
	}
	if err != nil {
		return Installed{}, err
	}
	st, _ := loadState(dir)
	inst := Installed{Manifest: m, Dir: dir, Enabled: st.Enabled}
	if m.ID != id {
		inst.Err = "the manifest id does not match its installed directory"
		return inst, nil
	}
	if err := verifyInstall(dir, &inst); err != nil {
		inst.Err = err.Error()
	}
	return inst, nil
}

// verifyInstall reads a plugin's install record. An official example must
// still match the copy embedded in the app, and that match does not make it
// signed. A signed plugin must still match the hash that was signed. A
// folder install has no record and is left unsigned, even when its files are
// the same bytes as an official example.
func verifyInstall(dir string, inst *Installed) error {
	info, err := loadInstallInfo(dir)
	if err != nil {
		return fmt.Errorf("install record: %w", err)
	}
	inst.InstalledVia = info.InstalledVia
	if inst.InstalledVia == "" {
		switch {
		case info.Official:
			inst.InstalledVia = "official"
		case info.Hash != "":
			inst.InstalledVia = "store"
		default:
			inst.InstalledVia = "unknown"
		}
	}
	if err := verifyOfficial(dir, inst, info); err != nil {
		return err
	}
	if inst.Official {
		return nil
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
	release, err := s.tryLease(m.ID)
	if err != nil {
		return Manifest{}, err
	}
	defer release()
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
	via := "folder"
	if filepath.Dir(filepath.Clean(src)) == filepath.Clean(s.dir) && strings.HasPrefix(filepath.Base(src), ".local-") {
		via = "zip"
	}
	info := installInfo{InstalledVia: via}
	if err := saveInstallInfo(tmp, info); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return Manifest{}, err
	}
	if err := s.rememberInstall(m.ID, info, dest); err != nil && !errors.Is(err, errProvenanceUnconfirmed) {
		_ = os.RemoveAll(dest)
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
	release, err := s.tryLease(id)
	if err != nil {
		return err
	}
	defer release()
	return s.remove(id)
}

func (s *Store) remove(id string) error {
	dir := filepath.Join(s.dir, id)
	kv := &KV{path: filepath.Join(dir, "kv.json")}
	lock := kv.fileLock()
	lock.Lock()
	defer lock.Unlock()
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return s.reportProvenanceDelete(id)
	} else if err != nil {
		return managementError("REMOVE_FAILED", "无法读取应用目录", 500)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		return managementError("BROKEN_PLUGIN", "应用清单损坏，无法安全清理声明凭据", 409)
	}
	if s.secrets != nil {
		for _, field := range m.Settings {
			if field.Secret {
				if err := s.secrets.Delete(secretKey(id, field.Key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return managementError("REMOVE_FAILED", "无法删除应用凭据，请重试卸载", 500)
				}
			}
		}
	}
	if os.RemoveAll(dir) != nil {
		return managementError("REMOVE_FAILED", "无法删除应用目录", 500)
	}
	return s.reportProvenanceDelete(id)
}

// SetEnabled turns a plugin on or off.
func (s *Store) SetEnabled(id string, enabled bool) error {
	release, err := s.tryLease(id)
	if err != nil {
		return err
	}
	defer release()
	return s.setEnabled(id, enabled)
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
	_, err := s.SetSettings(id, map[string]string{key: value})
	return err
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
	data  map[string]json.RawMessage
	size  int
}

const (
	maxKVKey   = 128
	maxKVValue = 64 << 10
)

// 同一进程内按文件协调所有 Runner 和 Store；固定锁表不会随插件安装次数增长。
// 每次持锁后重读文件，避免独立实例的旧快照覆盖 API 或界面刚保存的内容。
var kvFileLocks [64]sync.Mutex

func (kv *KV) fileLock() *sync.Mutex {
	path, err := filepath.Abs(kv.path)
	if err != nil {
		path = filepath.Clean(kv.path)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	return &kvFileLocks[h.Sum32()%uint32(len(kvFileLocks))]
}

// Get returns the value stored under key.
func (kv *KV) Get(key string) (json.RawMessage, bool) {
	mu := kv.fileLock()
	mu.Lock()
	defer mu.Unlock()
	if err := kv.load(); err != nil {
		return nil, false
	}
	value, ok := kv.data[key]
	return append(json.RawMessage(nil), value...), ok
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
	mu := kv.fileLock()
	mu.Lock()
	defer mu.Unlock()
	if err := kv.load(); err != nil {
		return err
	}
	old := 0
	if previous, ok := kv.data[key]; ok {
		old = len(previous) + len(key)
	}
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
	mu := kv.fileLock()
	mu.Lock()
	defer mu.Unlock()
	if err := kv.load(); err != nil {
		return err
	}
	if _, ok := kv.data[key]; !ok {
		return nil
	}
	kv.size -= len(kv.data[key]) + len(key)
	delete(kv.data, key)
	return kv.save()
}

// Keys returns the stored keys, for tests and the manager.
func (kv *KV) Keys() []string {
	mu := kv.fileLock()
	mu.Lock()
	defer mu.Unlock()
	if err := kv.load(); err != nil {
		return nil
	}
	out := make([]string, 0, len(kv.data))
	for key := range kv.data {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (kv *KV) load() error {
	kv.data, kv.size = nil, 0
	data, err := os.ReadFile(kv.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read plugin storage: %w", err)
	}
	if err := json.Unmarshal(data, &kv.data); err != nil {
		return fmt.Errorf("read plugin storage: %w", err)
	}
	for key, value := range kv.data {
		kv.size += len(key) + len(value)
	}
	return nil
}

func (kv *KV) save() error {
	data, err := json.Marshal(kv.data)
	if err != nil {
		return err
	}
	return writeFileAtomic(kv.path, data, 0o600)
}
