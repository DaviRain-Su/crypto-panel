package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// hostProvenance is the shell's own record of an install. It lives outside
// every app directory, so a plugin file cannot replace it. API mutations and
// update proposals trust this record, not install.json by itself.
//
// Apps installed before this record existed are classified once, when the
// manager starts. A regular install.json is recorded as signed or unsigned
// from its contents, and later edits must still match that digest. An
// install.json that is missing, linked, or otherwise not a regular parseable
// file cannot be classified; it is recorded LegacyUnsigned and stays unsigned
// for the API while the package hash still matches. That is a one-time
// migration limitation: a pre-upgrade install.json that was already forged
// into a regular unsigned record is indistinguishable from a real unsigned
// install and is trusted once. The native remove path does not consult this
// decision.
type hostProvenance struct {
	ID             string `json:"id"`
	Signed         bool   `json:"signed"`
	Official       bool   `json:"official"`
	Via            string `json:"via,omitempty"`
	PackageHash    string `json:"packageHash,omitempty"`
	Source         string `json:"source,omitempty"`
	InstallDigest  string `json:"installDigest,omitempty"`
	LegacyUnsigned bool   `json:"legacyUnsigned,omitempty"`
}

type provenanceIndex struct {
	Records map[string]hostProvenance `json:"records"`
}

// ProvenancePath is the host-owned index for this store. It sits beside
// settings.json, named from the plugins directory, and never inside an app
// directory. A store at <data>/plugins uses <data>/.plugins-provenance.json.
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

func (s *Store) loadProvenanceIndex() (provenanceIndex, error) {
	path, err := s.provenancePath()
	if err != nil {
		return provenanceIndex{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return provenanceIndex{Records: map[string]hostProvenance{}}, nil
	}
	if err != nil {
		return provenanceIndex{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return provenanceIndex{}, errors.New("provenance index is not a regular file")
	}
	f, err := openInstallRecord(path)
	if err != nil {
		return provenanceIndex{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return provenanceIndex{}, errors.New("provenance index changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return provenanceIndex{}, err
	}
	var index provenanceIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return provenanceIndex{}, err
	}
	if index.Records == nil {
		index.Records = map[string]hostProvenance{}
	}
	return index, nil
}

func (s *Store) saveProvenanceIndex(index provenanceIndex) error {
	if index.Records == nil {
		index.Records = map[string]hostProvenance{}
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path, err := s.provenancePath()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func (s *Store) getProvenance(id string) (hostProvenance, bool, error) {
	s.provenanceMu.Lock()
	defer s.provenanceMu.Unlock()
	index, err := s.loadProvenanceIndex()
	if err != nil {
		return hostProvenance{}, false, err
	}
	rec, ok := index.Records[id]
	return rec, ok, nil
}

func (s *Store) putProvenance(rec hostProvenance) error {
	if rec.ID == "" || validatePluginID(rec.ID) != nil {
		return errors.New("provenance id is invalid")
	}
	s.provenanceMu.Lock()
	defer s.provenanceMu.Unlock()
	index, err := s.loadProvenanceIndex()
	if err != nil {
		return err
	}
	index.Records[rec.ID] = rec
	return s.saveProvenanceIndex(index)
}

func (s *Store) forgetProvenance(id string) error {
	s.provenanceMu.Lock()
	defer s.provenanceMu.Unlock()
	index, err := s.loadProvenanceIndex()
	if err != nil {
		return err
	}
	if _, ok := index.Records[id]; !ok {
		return nil
	}
	delete(index.Records, id)
	return s.saveProvenanceIndex(index)
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

func (s *Store) migrateProvenance() error {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.IsDir() || validatePluginID(name) != nil {
			continue
		}
		_, ok, err := s.getProvenance(name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if err := s.migrateOne(name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateOne(id string) error {
	dir := filepath.Join(s.dir, id)
	hash, hashErr := normalizedPackageHash(dir)
	record, raw, readErr := readInstallBytes(dir)
	prov := hostProvenance{ID: id}
	// A file the host did not write, and cannot read as a regular record,
	// is unsigned for the API. See hostProvenance. The package hash ignores
	// that one unreadable install.json and still covers the rest of the tree.
	if readErr != nil || hashErr != nil {
		prov.LegacyUnsigned = true
		if hashErr != nil {
			hash, hashErr = legacyTreeHash(dir)
		}
		if hashErr == nil {
			prov.PackageHash = hash
		}
		return s.putProvenance(prov)
	}
	prov.Signed = record.Official || record.Hash != "" || record.Publisher != "" || record.Signature != ""
	prov.Official = record.Official
	prov.Via = record.InstalledVia
	prov.PackageHash = hash
	prov.Source = record.Source
	prov.InstallDigest = installDigest(raw)
	return s.putProvenance(prov)
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
	// LegacyUnsigned is the one-time migration of an install.json the host
	// could not read. The API treats that app as unsigned while the package
	// hash still matches; native removal does not depend on this record.
	if prov.LegacyUnsigned {
		return nil
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
	hashTree := normalizedPackageHash
	if prov.LegacyUnsigned {
		hashTree = legacyTreeHash
	}
	hash, hashErr := hashTree(inst.Dir)
	if hashErr != nil || hash == "" || hash != prov.PackageHash {
		return managementError("BASE_CHANGED", "已安装内容发生变化，请重新读取后提议更新", 409)
	}
	if prov.LegacyUnsigned {
		return nil
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
	hashTree := normalizedPackageHash
	if prov.LegacyUnsigned {
		hashTree = legacyTreeHash
	}
	hash, hashErr := hashTree(inst.Dir)
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
