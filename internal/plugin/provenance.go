package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// provenanceKeychainDown is process-wide. A credential store that does not
// answer is not retried for every plugin, and records then stay in memory
// for this process only.
var provenanceKeychainDown atomic.Bool

// errProvTimeout is returned when the credential store does not answer.
// Provenance then stays in process memory, so a later process fails closed
// instead of trusting a file the coding agent can rewrite.
var errProvTimeout = errors.New("provenance credential store timed out")

const provenanceWriteTimeout = time.Second

// hostProvenance is the shell's record of an install. Each record is stored
// in the existing plugin credential service under ProvenanceAccount, not in
// a file the app or the coding agent can rewrite. API mutations and update
// proposals trust that record. A missing record, a record that does not
// parse, or an install.json that does not match it fails closed.
//
// Apps installed before these records existed are classified once, when the
// manager first starts. Only a regular, parseable install.json is a trusted
// source for that pass: it is recorded as signed or unsigned from its
// contents, and later edits must still match its digest. A missing, linked,
// directory, or otherwise unreadable install.json is not migrated and does
// not become unsigned. The completion marker lives in the same credential
// service (provenanceMigrationAccount). Deleting a record or any on-disk
// index does not run the migration again.
type hostProvenance struct {
	ID            string `json:"id"`
	Signed        bool   `json:"signed"`
	Official      bool   `json:"official"`
	Via           string `json:"via,omitempty"`
	PackageHash   string `json:"packageHash,omitempty"`
	Source        string `json:"source,omitempty"`
	InstallDigest string `json:"installDigest,omitempty"`
}

// provenanceMigrationAccount is the credential-service account that records
// that the one-time migration has finished. It is not a plugin id.
const provenanceMigrationAccount = "provenance:migration"

// ProvenanceAccount is the credential-service account for one app's install
// provenance. The service name is unchanged; this is an account within it.
func ProvenanceAccount(id string) string { return "provenance:" + id }

// ProvenancePath is the on-disk location an index file would occupy beside
// settings.json. It is not a trust store: a regular file, an empty file, or
// a symlink there is never read and never authorizes an API mutation.
func (s *Store) ProvenancePath() string {
	path, err := s.provenancePath()
	if err != nil {
		return ""
	}
	return path
}

func (s *Store) provenancePath() (string, error) {
	dir, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	base := filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", errors.New("plugin store directory is invalid")
	}
	return filepath.Join(filepath.Dir(dir), "."+base+"-provenance.json"), nil
}

func installDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Store) keychainEnabled() bool {
	return s.secrets != nil && !provenanceKeychainDown.Load()
}

func (s *Store) markKeychainDown() {
	provenanceKeychainDown.Store(true)
}

func (s *Store) readMem(key string) ([]byte, bool) {
	s.provenanceMu.Lock()
	defer s.provenanceMu.Unlock()
	b, ok := s.provenanceMem[key]
	if !ok || len(b) == 0 {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

func (s *Store) writeMem(key string, value []byte) {
	s.provenanceMu.Lock()
	defer s.provenanceMu.Unlock()
	if s.provenanceMem == nil {
		s.provenanceMem = map[string][]byte{}
	}
	s.provenanceMem[key] = append([]byte(nil), value...)
}

func (s *Store) readProv(key string) ([]byte, bool) {
	if s.keychainEnabled() {
		b, err := s.secrets.Get(key)
		if err == nil && len(b) > 0 {
			return append([]byte(nil), b...), true
		}
		return nil, false
	}
	return s.readMem(key)
}

func (s *Store) writeProv(key string, value []byte) error {
	value = append([]byte(nil), value...)
	if s.keychainEnabled() {
		err := s.secretSet(key, value)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errProvTimeout) {
			return err
		}
		s.markKeychainDown()
	}
	s.writeMem(key, value)
	return nil
}

func (s *Store) secretSet(key string, value []byte) error {
	ch := make(chan error, 1)
	go func() { ch <- s.secrets.Set(key, value) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(provenanceWriteTimeout):
		return errProvTimeout
	}
}

func (s *Store) deleteProv(key string) {
	if s.keychainEnabled() {
		ch := make(chan struct{}, 1)
		go func() {
			_ = s.secrets.Delete(key)
			ch <- struct{}{}
		}()
		select {
		case <-ch:
		case <-time.After(provenanceWriteTimeout):
			s.markKeychainDown()
		}
	}
	s.provenanceMu.Lock()
	delete(s.provenanceMem, key)
	s.provenanceMu.Unlock()
}

func (s *Store) getProvenance(id string) (hostProvenance, bool, error) {
	raw, ok := s.readProv(ProvenanceAccount(id))
	if !ok {
		return hostProvenance{}, false, nil
	}
	var rec hostProvenance
	if json.Unmarshal(raw, &rec) != nil || rec.ID != id {
		return hostProvenance{}, false, nil
	}
	return rec, true, nil
}

func (s *Store) putProvenance(rec hostProvenance) error {
	if rec.ID == "" || validatePluginID(rec.ID) != nil {
		return errors.New("provenance id is invalid")
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.writeProv(ProvenanceAccount(rec.ID), data)
}

func (s *Store) forgetProvenance(id string) {
	s.deleteProv(ProvenanceAccount(id))
}

func (s *Store) migrationDone() bool {
	_, ok := s.readProv(provenanceMigrationAccount)
	return ok
}

// rememberInstall records the install the host just wrote. The on-disk
// install.json must be the bytes the host marshaled; a swapped file is not
// adopted.
func (s *Store) rememberInstall(id string, info installInfo, dir string) error {
	_, raw, err := readInstallBytes(dir)
	if err != nil {
		return err
	}
	want, err := marshalInstallInfo(info)
	if err != nil || !bytes.Equal(raw, want) {
		return errors.New("install record does not match the host copy")
	}
	hash, err := normalizedPackageHash(dir)
	if err != nil {
		return err
	}
	if info.PackageHash != "" && info.PackageHash != hash {
		return errors.New("package hash does not match the installed tree")
	}
	return s.putProvenance(hostProvenance{
		ID:            id,
		Signed:        info.Official || info.Hash != "" || info.Publisher != "" || info.Signature != "",
		Official:      info.Official,
		Via:           info.InstalledVia,
		PackageHash:   hash,
		Source:        info.Source,
		InstallDigest: installDigest(raw),
	})
}

// adoptPersonalRecord records a personal install the host journal already
// verified. It does not accept a record that claims a signature.
func (s *Store) adoptPersonalRecord(id, dir, packageHash string) error {
	record, raw, err := readInstallBytes(dir)
	if err != nil {
		return err
	}
	if record.Official || record.Hash != "" || record.Publisher != "" || record.Signature != "" {
		return errors.New("not a personal install record")
	}
	if record.PackageHash != packageHash || (record.InstalledVia != "local-api" && record.InstalledVia != "acp") {
		return errors.New("install record does not match the recovered install")
	}
	hash, err := normalizedPackageHash(dir)
	if err != nil || hash != packageHash {
		return errors.New("recovered tree does not match the journal")
	}
	return s.putProvenance(hostProvenance{
		ID:            id,
		Via:           record.InstalledVia,
		PackageHash:   hash,
		Source:        record.Source,
		InstallDigest: installDigest(raw),
	})
}

// migrateProvenance classifies installs that predate host records. It never
// fails the manager. Unreadable install.json files are skipped, and the pass
// does not run again once the credential-service marker is set.
func (s *Store) migrateProvenance() {
	if s.migrationDone() {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	failed := false
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.IsDir() || validatePluginID(name) != nil {
			continue
		}
		if err := s.migrateOne(name); err != nil {
			failed = true
		}
	}
	if !failed {
		_ = s.writeProv(provenanceMigrationAccount, []byte("done"))
	}
}

func (s *Store) migrateOne(id string) error {
	if _, ok, _ := s.getProvenance(id); ok {
		return nil
	}
	dir := filepath.Join(s.dir, id)
	record, raw, err := readInstallBytes(dir)
	if err != nil {
		// Missing, linked, a directory, or unparsable: not a trusted source.
		return nil
	}
	hash, err := normalizedPackageHash(dir)
	if err != nil || hash == "" {
		return nil
	}
	return s.putProvenance(hostProvenance{
		ID:            id,
		Signed:        record.Official || record.Hash != "" || record.Publisher != "" || record.Signature != "",
		Official:      record.Official,
		Via:           record.InstalledVia,
		PackageHash:   hash,
		Source:        record.Source,
		InstallDigest: installDigest(raw),
	})
}

func apiMutationForbidden() *Error {
	return managementError("API_MUTATION_FORBIDDEN", "本机 API 只能在宿主安装记录与磁盘应用一致时修改未签名的 local.* 个人应用", 403)
}

func updateSourceForbidden() *Error {
	return managementError("SOURCE_NOT_ALLOWED", "只能更新有宿主安装记录且安装记录未被改动的未签名个人应用", 422)
}

func (s *Store) authorizeAPIMutation(inst Installed) error {
	if inst.Official || inst.Signed || !strings.HasPrefix(inst.Manifest.ID, "local.") {
		return apiMutationForbidden()
	}
	prov, ok, err := s.provenanced(inst)
	if err != nil || !ok || prov.Signed || prov.Official {
		return apiMutationForbidden()
	}
	if inst.Err != "" {
		return apiMutationForbidden()
	}
	if err := provenanceMatchesInstall(prov, inst.Dir); err != nil {
		return apiMutationForbidden()
	}
	return nil
}

func (s *Store) authorizeUpdate(inst Installed) error {
	if inst.Official || inst.Signed {
		return updateSourceForbidden()
	}
	prov, ok, err := s.getProvenance(inst.Manifest.ID)
	if err != nil || !ok || prov.ID != inst.Manifest.ID || prov.Signed || prov.Official {
		return updateSourceForbidden()
	}
	hash, hashErr := normalizedPackageHash(inst.Dir)
	if hashErr != nil || hash == "" || hash != prov.PackageHash {
		return managementError("BASE_CHANGED", "已安装内容发生变化，请重新读取后提议更新", 409)
	}
	if err := provenanceMatchesInstall(prov, inst.Dir); err != nil {
		return updateSourceForbidden()
	}
	return nil
}

func (s *Store) provenanced(inst Installed) (hostProvenance, bool, error) {
	prov, ok, err := s.getProvenance(inst.Manifest.ID)
	if err != nil || !ok || prov.ID != inst.Manifest.ID {
		return hostProvenance{}, false, err
	}
	hash, hashErr := normalizedPackageHash(inst.Dir)
	if hashErr != nil || hash == "" || hash != prov.PackageHash {
		return hostProvenance{}, false, hashErr
	}
	return prov, true, nil
}

func provenanceMatchesInstall(prov hostProvenance, dir string) error {
	record, raw, err := readInstallBytes(dir)
	if err != nil || installDigest(raw) != prov.InstallDigest {
		return errors.New("install record does not match provenance")
	}
	if record.Official || record.Hash != "" || record.Publisher != "" || record.Signature != "" {
		return errors.New("install record claims a signature")
	}
	if prov.Via != "" && record.InstalledVia != prov.Via {
		return errors.New("install record origin changed")
	}
	if record.PackageHash != "" && record.PackageHash != prov.PackageHash {
		return errors.New("install record package hash changed")
	}
	return nil
}
