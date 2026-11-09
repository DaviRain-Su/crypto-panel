package plugin

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed testdata/examples/gas testdata/examples/fear-greed
var officialFS embed.FS

// OfficialSource is the install record of an example that shipped with the
// shell. It is not a registry URL and not a folder or zip the user picked.
const OfficialSource = "official"

// officialSeedName is the marker that the plugins folder has already been
// seeded. Removing an example after that does not bring it back.
const officialSeedName = ".seeded-official"

// errOfficialChanged is the integrity failure for an install record that
// claims to be an official example but does not match the embedded copy.
var errOfficialChanged = errors.New("the official example's files changed after it was installed")

// officialExampleIDs are the only examples the shell ships. Adding another
// directory to the embed without listing it here is refused.
var officialExampleIDs = []string{
	"com.example.feargreed",
	"com.example.gas",
}

func isOfficialID(id string) bool {
	for _, want := range officialExampleIDs {
		if id == want {
			return true
		}
	}
	return false
}

var (
	officialOnce  sync.Once
	officialErr   error
	officialByID  map[string]string // id → digest of the embedded tree
	officialRoots map[string]string // id → path inside officialFS
)

func loadOfficial() {
	officialByID = map[string]string{}
	officialRoots = map[string]string{}
	entries, err := fs.ReadDir(officialFS, "testdata/examples")
	if err != nil {
		officialErr = err
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root := path.Join("testdata/examples", entry.Name())
		tmp, err := os.MkdirTemp("", "crypto-panel-official-*")
		if err != nil {
			officialErr = err
			return
		}
		err = materialize(officialFS, root, tmp)
		var m Manifest
		var digest string
		if err == nil {
			m, err = LoadManifest(tmp)
		}
		if err == nil {
			err = m.ValidateDir(tmp)
		}
		if err == nil {
			digest, err = DigestDir(tmp)
		}
		os.RemoveAll(tmp)
		if err != nil {
			officialErr = err
			return
		}
		if !isOfficialID(m.ID) {
			officialErr = fmt.Errorf("refusing to ship unknown example %s", m.ID)
			return
		}
		if _, ok := officialByID[m.ID]; ok {
			officialErr = fmt.Errorf("duplicate official example %s", m.ID)
			return
		}
		officialByID[m.ID] = digest
		officialRoots[m.ID] = root
	}
	for _, id := range officialExampleIDs {
		if _, ok := officialByID[id]; !ok {
			officialErr = fmt.Errorf("official example %s is not embedded", id)
			return
		}
	}
}

func prepareOfficial() error {
	officialOnce.Do(loadOfficial)
	return officialErr
}

// officialDigest is the digest of the embedded example id.
func officialDigest(id string) (string, error) {
	if err := prepareOfficial(); err != nil {
		return "", err
	}
	digest, ok := officialByID[id]
	if !ok {
		return "", errOfficialChanged
	}
	return digest, nil
}

// EnsureOfficial copies the shell's official examples into the store the
// first time the plugins folder is seeded. An example the user has removed
// stays removed. An id that is already installed is left as it is, so a
// folder or store install is not rewritten as official.
func (s *Store) EnsureOfficial() error {
	if err := prepareOfficial(); err != nil {
		return err
	}
	if s.officialSeeded() {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".official-") {
				_ = os.RemoveAll(filepath.Join(s.dir, entry.Name()))
			}
		}
	}
	ids := make([]string, 0, len(officialByID))
	for id := range officialByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := s.installOfficial(id); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(s.dir, officialSeedName), []byte("1\n"), 0o644)
}

func (s *Store) officialSeeded() bool {
	_, err := os.Stat(filepath.Join(s.dir, officialSeedName))
	return err == nil
}

// installOfficial copies one embedded example into the store and records it
// as official. The record is not a publisher signature. Later reads check
// the files against the embedded digest, and a mismatch is a broken plugin.
func (s *Store) installOfficial(id string) error {
	dest := filepath.Join(s.dir, id)
	if _, err := os.Stat(dest); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	root, ok := officialRoots[id]
	if !ok {
		return fmt.Errorf("official example %s is not embedded", id)
	}
	tmp, err := os.MkdirTemp(s.dir, ".official-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmp)
		}
	}()
	if err := materialize(officialFS, root, tmp); err != nil {
		return err
	}
	m, err := LoadManifest(tmp)
	if err != nil {
		return err
	}
	if m.ID != id {
		return fmt.Errorf("official example %s has id %s", id, m.ID)
	}
	if err := m.ValidateDir(tmp); err != nil {
		return err
	}
	digest, err := DigestDir(tmp)
	if err != nil {
		return err
	}
	want, err := officialDigest(id)
	if err != nil {
		return err
	}
	if digest != want {
		return fmt.Errorf("official example %s does not match the embedded copy", id)
	}
	if err := saveState(tmp, state{Enabled: true}); err != nil {
		return err
	}
	info := installInfo{
		Source:    OfficialSource,
		Hash:      digest,
		Official:  true,
		Installed: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveInstallInfo(tmp, info); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// materialize copies a tree from fsys into dest. Paths in fsys use slashes.
func materialize(fsys fs.FS, root, dest string) error {
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("official example contains a symlink: %s", rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("official example contains a special file: %s", rel)
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// verifyOfficial reports whether dir is an official example whose files still
// match the embedded copy. A record that claims to be official and does not
// match is an error, so the plugin does not run.
func verifyOfficial(dir string, inst *Installed, info installInfo) error {
	if !info.Official {
		return nil
	}
	if !isOfficialID(inst.Manifest.ID) {
		return errOfficialChanged
	}
	want, err := officialDigest(inst.Manifest.ID)
	if err != nil {
		return err
	}
	digest, err := DigestDir(dir)
	if err != nil {
		return err
	}
	if digest != want || info.Hash != want {
		return errOfficialChanged
	}
	inst.Official = true
	inst.Source = OfficialSource
	return nil
}
