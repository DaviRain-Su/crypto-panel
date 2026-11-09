package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// provenanceIOTimeout bounds one credential-store read or write. A slow
// store must not stall startup or every authorization for longer than this.
const provenanceIOTimeout = time.Second

// provenanceBackoff is how long one store waits before trying the
// credential store again after a timeout. It is not process-wide: another
// store keeps using its own backend. Tests may shorten it.
var provenanceBackoff = 5 * time.Second

// errProvTimeout is a credential-store call that did not finish in time.
var errProvTimeout = errors.New("provenance credential store timed out")

// errProvenanceUnconfirmed means the app is installed but its provenance
// record was not durably stored. Callers keep the install; the local API
// fails closed until a later confirmed write.
var errProvenanceUnconfirmed = errors.New("provenance was not confirmed")

// errProvenanceRejected means install.json is not the bytes the host wrote.
// The record is not updated.
var errProvenanceRejected = errors.New("install record does not match the host copy")

// hostProvenance is the shell's record of an install. Each record is an
// account in the existing plugin credential service (ProvenanceAccount).
// That is a higher bar than a file next to the app: rewriting install.json
// or an index file is not enough. It is not a hard boundary against another
// process of the same user. On Linux and Windows that process can write the
// same credential service without a prompt. On macOS the login keychain's
// default ACL may prompt before revealing secret bytes; listing or creating
// items usually does not. The coding agent is a same-user process with no
// OS sandbox, so these records must not be described as something it cannot
// forge.
//
// A missing record, a read error, a record that does not parse, or an
// install.json that does not match fails closed for the local API. Native
// install and removal do not require the credential store.
//
// Apps installed before these records existed are classified once, and only
// when that pass can be made durable. The completion marker is written last.
// A missing, linked, directory, or unparsable install.json is not a source.
// If the credential store cannot confirm the pass, those apps stay forbidden
// to the local API until a native reinstall writes a new record. See
// migrateProvenance for the leftover case where neither the sentinel nor the
// credential store remembers that an attempt already failed.
type hostProvenance struct {
	ID            string `json:"id"`
	Signed        bool   `json:"signed"`
	Official      bool   `json:"official"`
	Via           string `json:"via,omitempty"`
	PackageHash   string `json:"packageHash,omitempty"`
	Source        string `json:"source,omitempty"`
	InstallDigest string `json:"installDigest,omitempty"`
}

// provenanceMigrationAccount is written only after every migrated record has
// been stored. provenanceMigrationStarted is written before install.json is
// read, so a pass that dies in the middle is not retried against a later file.
const (
	provenanceMigrationAccount = "provenance:migration"
	provenanceMigrationStarted = "provenance:migration-started"
)

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

func (s *Store) inProvenanceBackoff() bool {
	s.provenanceMu.Lock()
	defer s.provenanceMu.Unlock()
	return time.Now().Before(s.provBackoff)
}

func (s *Store) startProvenanceBackoff() {
	s.provenanceMu.Lock()
	s.provBackoff = time.Now().Add(provenanceBackoff)
	s.provenanceMu.Unlock()
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

// readProv reports found, not-found, and error separately. An error is not
// a missing record: callers must not migrate or authorize from it. Memory
// filled in after a failed credential-store write is not a result.
func (s *Store) readProv(key string) ([]byte, bool, error) {
	if s.secrets == nil {
		b, ok := s.readMem(key)
		if !ok {
			return nil, false, nil
		}
		return b, true, nil
	}
	if s.inProvenanceBackoff() {
		return nil, false, errProvTimeout
	}
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := s.secrets.Get(key)
		ch <- result{b, err}
	}()
	var b []byte
	var err error
	select {
	case got := <-ch:
		b, err = got.b, got.err
	case <-time.After(provenanceIOTimeout):
		s.startProvenanceBackoff()
		return nil, false, errProvTimeout
	}
	if err == nil && len(b) > 0 {
		return append([]byte(nil), b...), true, nil
	}
	if err == nil || provenanceAbsent(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func (s *Store) writeProv(key string, value []byte) error {
	value = append([]byte(nil), value...)
	if s.secrets == nil {
		s.writeMem(key, value)
		return nil
	}
	if s.inProvenanceBackoff() {
		return errProvTimeout
	}
	ch := make(chan error, 1)
	go func() { ch <- s.secrets.Set(key, value) }()
	select {
	case err := <-ch:
		if err != nil {
			return err
		}
		return nil
	case <-time.After(provenanceIOTimeout):
		s.startProvenanceBackoff()
		return errProvTimeout
	}
}

func (s *Store) deleteProv(key string) error {
	if s.secrets == nil {
		s.provenanceMu.Lock()
		delete(s.provenanceMem, key)
		s.provenanceMu.Unlock()
		return nil
	}
	if s.inProvenanceBackoff() {
		return errProvTimeout
	}
	ch := make(chan error, 1)
	go func() { ch <- s.secrets.Delete(key) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(provenanceIOTimeout):
		s.startProvenanceBackoff()
		return errProvTimeout
	}
}

func provenanceAbsent(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, mygo.ErrSecretNotFound)
}

func (s *Store) getProvenance(id string) (hostProvenance, bool, error) {
	raw, found, err := s.readProv(ProvenanceAccount(id))
	if err != nil || !found {
		return hostProvenance{}, false, err
	}
	var rec hostProvenance
	if json.Unmarshal(raw, &rec) != nil || rec.ID != id {
		return hostProvenance{}, false, errors.New("provenance record is unreadable")
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
	if err := s.writeProv(ProvenanceAccount(rec.ID), data); err != nil {
		return fmt.Errorf("%w: %w", errProvenanceUnconfirmed, err)
	}
	return nil
}

func (s *Store) forgetProvenance(id string) error {
	err := s.deleteProv(ProvenanceAccount(id))
	if err != nil {
		log.Printf("lintel: delete %s: %v", ProvenanceAccount(id), err)
	}
	return err
}

func (s *Store) reportProvenanceDelete(id string) error {
	if err := s.forgetProvenance(id); err != nil {
		return managementError("PROVENANCE_DELETE_FAILED", "应用已删除，但未能移除凭据服务账户 "+ProvenanceAccount(id), 500)
	}
	return nil
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
		return errProvenanceRejected
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
// fails the manager and never authorizes from a write that did not land.
//
// The completion marker is written only after every record chosen for this
// pass has been stored. A read error is not "missing": that app is not
// migrated, and an existing record is never replaced. If the pass cannot
// finish, the started marker or the on-disk sentinel keeps a later start
// from trusting install.json as it reads then. Apps left without a confirmed
// record stay forbidden to the local API until a native reinstall.
//
// Residual risk: a failed sentinel write alone is enough. The started and
// done markers are only attempted after the sentinel is created, so if that
// create fails the next start looks like a first upgrade and may classify
// install.json as it is then. Re-migration also happens if the sentinel,
// both migration markers, and the target app's provenance record are all
// gone. That is the same surface as forging install.json before the first
// upgrade. Deleting only the sentinel, or only the markers, is not enough.
func (s *Store) migrateProvenance() {
	done, found, err := s.readProv(provenanceMigrationAccount)
	if err != nil || (found && len(done) > 0) {
		return
	}
	_, startedFound, err := s.readProv(provenanceMigrationStarted)
	if err != nil {
		return
	}
	sentinel, serr := s.migrationSentinelExists()
	if serr != nil || startedFound || sentinel {
		// An earlier pass did not finish. Do not read install.json again.
		return
	}
	if err := s.createMigrationSentinel(); err != nil {
		return
	}
	if err := s.writeProv(provenanceMigrationStarted, []byte("1")); err != nil {
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
	_, found, err := s.getProvenance(id)
	if err != nil {
		return err
	}
	if found {
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

func (s *Store) migrationSentinelPath() (string, error) {
	dir, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	base := filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", errors.New("plugin store directory is invalid")
	}
	return filepath.Join(filepath.Dir(dir), "."+base+"-migration-attempted"), nil
}

func (s *Store) migrationSentinelExists() (bool, error) {
	path, err := s.migrationSentinelPath()
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("migration sentinel is not a regular file")
	}
	return true, nil
}

// createMigrationSentinelFile writes the sentinel. Tests replace it to inject
// a failure without changing directory permissions.
var createMigrationSentinelFile = writeMigrationSentinelFile

func writeMigrationSentinelFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte("1\n")); err != nil {
		return err
	}
	return f.Sync()
}

func (s *Store) createMigrationSentinel() error {
	path, err := s.migrationSentinelPath()
	if err != nil {
		return err
	}
	return createMigrationSentinelFile(path)
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

// migrationStuck reports an unfinished provenance migration. A read error is
// not treated as "finished".
func (s *Store) migrationStuck() (bool, error) {
	_, found, err := s.readProv(provenanceMigrationAccount)
	if err != nil {
		return false, err
	}
	if found {
		return false, nil
	}
	_, started, err := s.readProv(provenanceMigrationStarted)
	if err != nil {
		return false, err
	}
	sentinel, serr := s.migrationSentinelExists()
	if serr != nil {
		return true, nil
	}
	return started || sentinel, nil
}

// ErrProvenanceChanged means the app on disk is not the one the confirm
// window showed. ErrProvenanceSigned means a verified signature is present,
// so the local API stays closed. ErrProvenanceUnhashed means there is no
// package hash to pin. ErrProvenanceLinked means install.json has more than
// one name.
var (
	ErrProvenanceChanged  = errors.New("the app changed before it was confirmed")
	ErrProvenanceSigned   = errors.New("a signed app cannot be opened to the local API")
	ErrProvenanceUnhashed = errors.New("the app has no package hash")
	ErrProvenanceLinked   = errors.New("install record has more than one link")
)

// ProvenanceView is the snapshot shown in the confirm window.
type ProvenanceView struct {
	ID          string
	Name        string
	Path        string
	PackageHash string
	Signed      bool
	dev         uint64
	ino         uint64
	nlink       uint64
	digest      string
}

// LockedLocalApps are local.* personal apps whose origin cannot be confirmed
// while migration did not finish. The list does not call them unsigned:
// install.json is not signature evidence. A package whose signature still
// verifies is omitted, because confirming it cannot make it editable. A
// local.* app that was signed before local.* ids were rejected, and whose
// install.json a same-user process rewrites before the window opens, can
// still appear here. The user's confirm click is the only gate. This does
// not follow links.

// readInstallIdentity is installIdentity, swapped in tests that need a link
// count the kernel will not produce.
var readInstallIdentity = installIdentity

func (s *Store) LockedLocalApps() ([]Installed, error) {
	stuck, err := s.migrationStuck()
	if err != nil || !stuck {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.IsDir() || !strings.HasPrefix(name, "local.") || validatePluginID(name) != nil {
			continue
		}
		_, found, err := s.getProvenance(name)
		if err == nil && found {
			continue
		}
		inst, err := s.Get(name)
		if err != nil || inst.Manifest.ID != name || inst.Official {
			continue
		}
		signed, _ := verifiedSignature(inst.Dir)
		if signed || inst.Signed {
			continue
		}
		out = append(out, inst)
	}
	return out, nil
}

// SnapshotProvenance records what the confirm window is about to show.
func (s *Store) SnapshotProvenance(id string) (ProvenanceView, error) {
	view, _, err := s.inspectProvenance(id)
	return view, err
}

// ConfirmProvenance writes a host record only when the app still matches the
// snapshot from the open window. A verified signature is stored as signed
// and never turned into an editable personal app. The caller holds no API
// grant until this returns nil.
func (s *Store) ConfirmProvenance(view ProvenanceView) error {
	if !strings.HasPrefix(view.ID, "local.") || validatePluginID(view.ID) != nil {
		return errors.New("only a local app can be confirmed")
	}
	release, err := s.tryLease(view.ID)
	if err != nil {
		return err
	}
	defer release()
	stuck, err := s.migrationStuck()
	if err != nil {
		return err
	}
	if !stuck {
		return errors.New("provenance migration is not waiting for confirmation")
	}
	current, raw, err := s.inspectProvenance(view.ID)
	if err != nil {
		return err
	}
	if current.PackageHash == "" || view.PackageHash == "" {
		return ErrProvenanceUnhashed
	}
	if view.ID != current.ID || view.Path != current.Path || view.PackageHash != current.PackageHash || view.digest != current.digest || view.Signed != current.Signed || view.dev != current.dev || view.ino != current.ino || view.nlink != current.nlink || view.nlink > 1 || current.nlink > 1 {
		return ErrProvenanceChanged
	}
	if current.Signed {
		return ErrProvenanceSigned
	}
	record, _, err := readInstallBytes(filepath.Join(s.dir, current.ID))
	if err != nil {
		return err
	}
	return s.putProvenance(hostProvenance{
		ID:            current.ID,
		Signed:        false,
		Via:           record.InstalledVia,
		PackageHash:   current.PackageHash,
		InstallDigest: installDigest(raw),
	})
}

func (s *Store) inspectProvenance(id string) (ProvenanceView, []byte, error) {
	dir := filepath.Join(s.dir, id)
	inst, err := s.Get(id)
	if err != nil || inst.Manifest.ID != id {
		return ProvenanceView{}, nil, errors.New("app is not installed")
	}
	if inst.Official {
		return ProvenanceView{}, nil, ErrProvenanceSigned
	}
	path := filepath.Join(dir, "install.json")
	info, err := os.Lstat(path)
	if err != nil {
		return ProvenanceView{}, nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ProvenanceView{}, nil, errInstallNotRegular
	}
	dev, ino, nlink, ok := readInstallIdentity(path, info)
	if !ok || nlink == 0 || nlink > 1 {
		return ProvenanceView{}, nil, ErrProvenanceLinked
	}
	_, raw, err := readInstallBytes(dir)
	if err != nil {
		return ProvenanceView{}, nil, err
	}
	opened, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, opened) {
		return ProvenanceView{}, nil, ErrProvenanceChanged
	}
	hash, err := normalizedPackageHash(dir)
	if err != nil {
		return ProvenanceView{}, nil, err
	}
	if hash == "" {
		return ProvenanceView{}, nil, ErrProvenanceUnhashed
	}
	signed, _ := verifiedSignature(dir)
	if inst.Signed {
		signed = true
	}
	return ProvenanceView{
		ID: id, Name: inst.Manifest.Name, Path: dir, PackageHash: hash, Signed: signed,
		dev: dev, ino: ino, nlink: nlink, digest: installDigest(raw),
	}, raw, nil
}

// verifiedSignature reports whether this directory carries a signature that
// checks out against its own bytes. A publisher claim that does not verify
// is not evidence. Official examples are signed for this purpose.
func verifiedSignature(dir string) (bool, error) {
	record, _, err := readInstallBytes(dir)
	if err != nil {
		return false, err
	}
	if record.Official {
		return true, nil
	}
	if record.Publisher == "" || record.Signature == "" || record.Hash == "" {
		return false, nil
	}
	digest, err := DigestDir(dir)
	if err != nil || digest != record.Hash {
		return false, nil
	}
	if err := VerifyDigest(record.Hash, record.Signature, record.Publisher); err != nil {
		return false, nil
	}
	return true, nil
}
