package plugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Install limits, shared by the folder copy, the archive and the digest.
const (
	maxPluginFiles = 200
	maxPluginTotal = 8 << 20
	maxPluginFile  = 2 << 20
)

// Archive is a downloaded plugin that has been unpacked and verified, but
// not installed: the shell shows its permissions first, and only the user's
// answer moves it into the plugins folder.
type Archive struct {
	Manifest Manifest
	Entry    RegistryEntry

	dir   string
	store *Store
	done  bool
}

// Prepare unpacks a signed archive and checks it against its registry entry:
// the manifest's id and version must match, the tree's digest must match the
// entry's hash, and the signature must be the publisher's over that hash. It
// also refuses an install that would replace a newer version, replay the
// same version with other contents, or change the publisher's key.
func (s *Store) Prepare(data []byte, entry RegistryEntry) (*Archive, error) {
	if err := entry.Validate(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(entry.ID, "local.") {
		return nil, fmt.Errorf("%s: a signed install cannot use a local.* id", entry.ID)
	}
	if len(data) > MaxArchiveBytes {
		return nil, fmt.Errorf("the download is larger than %d bytes", MaxArchiveBytes)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(s.dir, ".install-*")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Archive, error) {
		os.RemoveAll(tmp)
		return nil, err
	}
	if err := extractZip(data, tmp); err != nil {
		return fail(err)
	}
	m, err := LoadManifest(tmp)
	if err == nil {
		err = m.ValidateDir(tmp)
	}
	if err == nil && m.ID != entry.ID {
		err = fmt.Errorf("the archive holds %s, not %s", m.ID, entry.ID)
	}
	if err == nil && m.Version != entry.Version {
		err = fmt.Errorf("the archive is version %s, not %s", m.Version, entry.Version)
	}
	if err == nil {
		var digest string
		if digest, err = DigestDir(tmp); err == nil && digest != entry.Hash {
			err = errors.New("the download does not match the registry's hash")
		}
	}
	if err == nil {
		err = VerifyDigest(entry.Hash, entry.Signature, entry.Publisher)
	}
	if err == nil {
		err = s.checkInstall(entry)
	}
	if err != nil {
		return fail(err)
	}
	return &Archive{Manifest: m, Entry: entry, dir: tmp, store: s}, nil
}

// UnpackLocal unpacks a local plugin archive into a hidden folder under the
// store, so the shell can show its permissions before installing it. The
// caller installs the folder with Install, or discards it with
// DiscardUnpacked. A local archive is unsigned: there is no registry entry
// to check it against.
func (s *Store) UnpackLocal(data []byte) (string, Manifest, error) {
	if len(data) > MaxArchiveBytes {
		return "", Manifest{}, fmt.Errorf("the archive is larger than %d bytes", MaxArchiveBytes)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", Manifest{}, err
	}
	dir, err := os.MkdirTemp(s.dir, ".local-*")
	if err != nil {
		return "", Manifest{}, err
	}
	if err := extractZip(data, dir); err != nil {
		os.RemoveAll(dir)
		return "", Manifest{}, err
	}
	m, err := LoadManifest(dir)
	if err == nil {
		err = m.ValidateDir(dir)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", Manifest{}, err
	}
	return dir, m, nil
}

// DiscardUnpacked deletes a folder UnpackLocal made.
func (s *Store) DiscardUnpacked(dir string) {
	clean := filepath.Clean(dir)
	if filepath.Dir(clean) == filepath.Clean(s.dir) &&
		strings.HasPrefix(filepath.Base(clean), ".local-") {
		os.RemoveAll(clean)
	}
}

// checkInstall refuses an update that goes backwards, a same-version
// archive with other contents (a replay), and a publisher key that changed
// since the plugin was installed. A folder with no readable manifest is
// treated as absent: a store install may repair it.
func (s *Store) checkInstall(entry RegistryEntry) error {
	inst, err := s.Get(entry.ID)
	if err != nil {
		return nil
	}
	old := inst.Manifest
	switch cmp := CompareVersions(entry.Version, old.Version); {
	case cmp < 0:
		return fmt.Errorf("version %s is older than the installed %s", entry.Version, old.Version)
	case cmp == 0:
		info, ierr := loadInstallInfo(inst.Dir)
		if ierr == nil && info.Hash == entry.Hash {
			return fmt.Errorf("plugin %s is already installed", entry.ID)
		}
		return fmt.Errorf("version %s is already installed with different content", entry.Version)
	}
	info, ierr := loadInstallInfo(inst.Dir)
	if ierr == nil && info.Publisher != "" && info.Publisher != entry.Publisher {
		return errors.New("the publisher key changed since the last install")
	}
	return nil
}

// Install moves the prepared archive into the plugins folder, carrying the
// plugin's settings and stored data over an update. The old folder is only
// replaced once the new one is in place.
func (ar *Archive) Install() (Manifest, error) {
	if ar.done || ar.dir == "" || ar.store == nil {
		return Manifest{}, errors.New("this archive was already installed or closed")
	}
	if strings.HasPrefix(ar.Manifest.ID, "local.") || strings.HasPrefix(ar.Entry.ID, "local.") {
		return Manifest{}, fmt.Errorf("%s: a signed install cannot use a local.* id", ar.Manifest.ID)
	}
	release, err := ar.store.tryLease(ar.Manifest.ID)
	if err != nil {
		return Manifest{}, err
	}
	defer release()
	if err := ar.store.checkInstall(ar.Entry); err != nil {
		return Manifest{}, err
	}
	digest, err := DigestDir(ar.dir)
	if err != nil || digest != ar.Entry.Hash {
		return Manifest{}, errors.New("the prepared archive changed after verification")
	}
	dest := filepath.Join(ar.store.dir, ar.Manifest.ID)
	st := state{Enabled: true}
	if _, err := os.Stat(dest); err == nil {
		if old, err := loadState(dest); err == nil {
			st = old
		}
		if data, err := os.ReadFile(filepath.Join(dest, "kv.json")); err == nil {
			if err := writeFileAtomic(filepath.Join(ar.dir, "kv.json"), data, 0o600); err != nil {
				return Manifest{}, err
			}
		}
	}
	if err := saveState(ar.dir, st); err != nil {
		return Manifest{}, err
	}
	info := installInfo{
		Source:    ar.Entry.URL,
		Hash:      ar.Entry.Hash,
		Signature: ar.Entry.Signature,
		Publisher: ar.Entry.Publisher,
		Installed: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveInstallInfo(ar.dir, info); err != nil {
		return Manifest{}, err
	}
	if _, err := os.Stat(dest); err == nil {
		backup := dest + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := os.Rename(dest, backup); err != nil {
			return Manifest{}, err
		}
		if err := os.Rename(ar.dir, dest); err != nil {
			_ = os.Rename(backup, dest)
			return Manifest{}, err
		}
		_ = os.RemoveAll(backup)
	} else if err := os.Rename(ar.dir, dest); err != nil {
		return Manifest{}, err
	}
	if err := ar.store.rememberInstall(ar.Manifest.ID, info, dest); err != nil && !errors.Is(err, errProvenanceUnconfirmed) {
		return Manifest{}, err
	}
	ar.done = true
	ar.dir = ""
	return ar.Manifest, nil
}

// Close discards an archive the user did not approve.
func (ar *Archive) Close() {
	if ar.done || ar.dir == "" {
		return
	}
	os.RemoveAll(ar.dir)
	ar.dir = ""
}

// installInfo is the shell's record of an install, kept beside the plugin's
// state. It is not part of the plugin's digest. Official marks a first-party
// example the shell seeded; it is not a publisher signature.
type installInfo struct {
	Source       string `json:"source,omitempty"`
	Hash         string `json:"hash"`
	Signature    string `json:"signature"`
	Publisher    string `json:"publisher"`
	Installed    string `json:"installed,omitempty"`
	Official     bool   `json:"official,omitempty"`
	InstalledVia string `json:"installedVia,omitempty"`
	PackageHash  string `json:"packageHash,omitempty"`
}

var errInstallNotRegular = errors.New("install record is not a regular file")

func loadInstallInfo(dir string) (installInfo, error) {
	record, _, err := readInstallBytes(dir)
	// A missing or non-regular record does not make Get fail. Host provenance
	// decides whether the API may treat the app as unsigned. Opening a FIFO
	// here would block.
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errInstallNotRegular) {
		return installInfo{}, nil
	}
	return record, err
}

// readInstallInfoNoFollow 只接受普通 install.json。缺失、符号链接、目录和其他
// 非普通文件都是错误，而且不会打开链接目标。
func readInstallInfoNoFollow(dir string) (installInfo, error) {
	record, _, err := readInstallBytes(dir)
	return record, err
}

// readInstallBytes rejects anything that is not a regular file before opening
// it, so a FIFO cannot block the caller. The unix opener is also non-blocking.
func readInstallBytes(dir string) (installInfo, []byte, error) {
	recordPath := filepath.Join(dir, "install.json")
	info, err := os.Lstat(recordPath)
	if err != nil {
		return installInfo{}, nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return installInfo{}, nil, errInstallNotRegular
	}
	f, err := openInstallRecord(recordPath)
	if err != nil {
		return installInfo{}, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return installInfo{}, nil, errors.New("install record changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return installInfo{}, nil, err
	}
	var record installInfo
	if err := json.Unmarshal(data, &record); err != nil {
		return installInfo{}, nil, err
	}
	return record, data, nil
}

func marshalInstallInfo(info installInfo) ([]byte, error) {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func saveInstallInfo(dir string, info installInfo) error {
	data, err := marshalInstallInfo(info)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "install.json"), data, 0o600)
}

// extractZip unpacks one plugin archive into dest. A single top-level folder
// is stripped, so a GitHub archive of the repository works as it comes. The
// archive may not escape dest, carry links or special files, or bring more
// than a plugin may.
func extractZip(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("the archive is not a zip file: %w", err)
	}
	type member struct {
		file *zip.File
		name string
	}
	var members []member
	var names []string
	for _, f := range zr.File {
		name, err := cleanZipName(f.Name)
		if err != nil {
			return err
		}
		if name == "" || isJunk(name) || f.FileInfo().IsDir() {
			continue
		}
		if f.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("the archive contains a symlink: %s", name)
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("the archive contains a special file: %s", name)
		}
		members = append(members, member{file: f, name: name})
		names = append(names, name)
	}
	prefix := commonTopDir(names)
	total := uint64(0)
	seen := make(map[string]bool, len(members))
	for _, m := range members {
		name := strings.TrimPrefix(m.name, prefix)
		if name == "" {
			continue
		}
		if isShellFile(name) {
			return fmt.Errorf("the archive may not contain %s", name)
		}
		if seen[name] {
			return fmt.Errorf("the archive contains %s twice", name)
		}
		seen[name] = true
		if m.file.UncompressedSize64 > maxPluginFile {
			return fmt.Errorf("%s is larger than %d bytes", name, maxPluginFile)
		}
		if len(seen) > maxPluginFiles {
			return fmt.Errorf("the plugin has more than %d files", maxPluginFiles)
		}
		rc, err := m.file.Open()
		if err != nil {
			return err
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxPluginFile+1))
		rc.Close()
		if err != nil {
			return err
		}
		if len(content) > maxPluginFile {
			return fmt.Errorf("%s is larger than %d bytes", name, maxPluginFile)
		}
		// Count what was actually decompressed: a zip may lie about the
		// sizes it declares.
		total += uint64(len(content))
		if total > maxPluginTotal {
			return fmt.Errorf("the plugin is larger than %d bytes", maxPluginTotal)
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
			return fmt.Errorf("the archive escapes its folder: %s", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// cleanZipName keeps an archive member inside the folder it is unpacked
// into, and refuses the names zip allows but a filesystem should not.
func cleanZipName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) {
		return "", fmt.Errorf("the archive contains an invalid file name %q", name)
	}
	if path.IsAbs(name) {
		return "", fmt.Errorf("the archive contains an absolute path: %s", name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("the archive escapes its folder: %s", name)
	}
	return clean, nil
}

// commonTopDir returns "dir/" when every name sits under one top-level
// folder, as a GitHub archive does, and "" otherwise.
func commonTopDir(names []string) string {
	top := ""
	for _, name := range names {
		first, rest, ok := strings.Cut(name, "/")
		if !ok || rest == "" {
			return ""
		}
		if top == "" {
			top = first
			continue
		}
		if first != top {
			return ""
		}
	}
	if top == "" {
		return ""
	}
	return top + "/"
}
