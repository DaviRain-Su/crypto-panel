package plugin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenManifest and goldenSource are a fixed personal app. The digest below
// was produced by main's normalizedPackageHash: SettingField.Secret is
// omitempty, so a plain setting does not contribute "secret":false.
const (
	goldenManifest = `{"id":"local.personal.golden","name":"Golden","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"settings":[{"key":"label","label":"Label"},{"key":"token","label":"Token","secret":true}],"navigation":[{"title":"Overview","page":"overview"}]}`
	goldenSource   = `export function page() { return { title: "Golden" }; }`
	// goldenPackageHash is normalizedPackageHash of the two files above when
	// SettingField.Secret uses omitempty, which is main's manifest encoding.
	goldenPackageHash = "sha256:5cf8746b06d7136f8ca2f0c52f4e4a4d50f030806ecf3094146be8c6ba8bbb08"
)

func TestCanonicalPackageHashMatchesMain(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, goldenManifest, goldenSource)
	got, err := normalizedPackageHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != goldenPackageHash {
		t.Fatalf("package hash %s, main produced %s", got, goldenPackageHash)
	}
	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canon), `"secret":false`) {
		t.Fatalf("canonical manifest changed shape: %s", canon)
	}
	store := NewStore(t.TempDir(), &memSecrets{m: map[string][]byte{}})
	if _, err := store.Install(dir); err != nil {
		t.Fatal(err)
	}
	installed, err := store.PackageHash("local.personal.golden")
	if err != nil || installed != goldenPackageHash {
		t.Fatalf("installed hash %s %v", installed, err)
	}
	statuses, err := store.DescribeSettings("local.personal.golden")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"key":"label"`) || !strings.Contains(string(body), `"secret":false`) {
		t.Fatalf("settings-status API dropped secret:false: %s", body)
	}
	if !strings.Contains(string(body), `"key":"token"`) || !strings.Contains(string(body), `"secret":true`) {
		t.Fatalf("settings-status API dropped secret:true: %s", body)
	}
}

// keychainErrorSecrets serves provenance records and fails every other read,
// the way a keychain does when a secret lookup errors instead of missing.
type keychainErrorSecrets struct{ m map[string][]byte }

func (s *keychainErrorSecrets) Get(key string) ([]byte, error) {
	if strings.HasPrefix(key, "provenance:") {
		if v, ok := s.m[key]; ok {
			return append([]byte(nil), v...), nil
		}
		return nil, fs.ErrNotExist
	}
	return nil, errors.New("keychain locked")
}
func (s *keychainErrorSecrets) Set(key string, value []byte) error {
	if strings.HasPrefix(key, "provenance:") {
		if s.m == nil {
			s.m = map[string][]byte{}
		}
		s.m[key] = append([]byte(nil), value...)
	}
	return nil
}
func (s *keychainErrorSecrets) Delete(string) error { return nil }

func packageWithoutSetting(t *testing.T, id, version, drop string) Package {
	t.Helper()
	p := personalPackage(t, id, version)
	var manifest Manifest
	if err := json.Unmarshal(p.Plugin, &manifest); err != nil {
		t.Fatal(err)
	}
	kept := manifest.Settings[:0]
	for _, field := range manifest.Settings {
		if field.Key != drop {
			kept = append(kept, field)
		}
	}
	manifest.Settings = kept
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	p.Plugin = raw
	return p
}

func TestUpdateDropsUnsetSecretWhenKeychainReportsNotFound(t *testing.T) {
	sec := &notFoundSecrets{m: map[string][]byte{}}
	_, m := newTestManager(t, sec)
	first := installTest(t, m, personalPackage(t, "local.personal.unset", "1.0.0"))
	next := packageWithoutSetting(t, first.PluginID, "2.0.0", "token")
	pending, err := m.Propose(Source{Kind: "inline", Package: &next}, "update", first.PluginID, first.PackageHash, "local-api")
	if err != nil {
		t.Fatalf("removing a never-set secret returned %v", err)
	}
	if _, err := m.Cancel(pending.RequestID); err != nil {
		t.Fatal(err)
	}
	sec.m[secretKey(first.PluginID, "token")] = []byte("kept")
	again := packageWithoutSetting(t, first.PluginID, "3.0.0", "token")
	_, err = m.Propose(Source{Kind: "inline", Package: &again}, "update", first.PluginID, first.PackageHash, "local-api")
	requireCode(t, err, "SETTINGS_MIGRATION_REQUIRED")
}

func TestUpdateUnsetSecretStillFailsWhenKeychainErrors(t *testing.T) {
	_, m := newTestManager(t, &keychainErrorSecrets{})
	first := installTest(t, m, personalPackage(t, "local.personal.locked", "1.0.0"))
	next := packageWithoutSetting(t, first.PluginID, "2.0.0", "token")
	_, err := m.Propose(Source{Kind: "inline", Package: &next}, "update", first.PluginID, first.PackageHash, "local-api")
	requireCode(t, err, "CREDENTIALS_UNAVAILABLE")
}

func TestInstallClearsLeftoverSecrets(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	sec := &memSecrets{m: map[string][]byte{secretKey("com.example.store", "token"): []byte("leftover")}}
	store := NewStore(filepath.Join(root, "plugins"), sec)
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.m[secretKey("com.example.store", "token")]; ok {
		t.Fatal("install kept a leftover credential")
	}
	if v, err := store.Setting("com.example.store", "token"); err != nil || v != "" {
		t.Fatalf("new install inherited %q (%v)", v, err)
	}
}

func TestRemoveAtDropsProvenanceAndSeenHash(t *testing.T) {
	sec := &memSecrets{m: map[string][]byte{}}
	store, manager := newTestManager(t, sec)
	src := filepath.Join(t.TempDir(), "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	id := "com.example.store"
	if _, ok := sec.m[ProvenanceAccount(id)]; !ok {
		t.Fatal("install did not record provenance")
	}
	store.rememberSeenHash(id, "sha256:stale")
	inst, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveAt(inst.Dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.m[ProvenanceAccount(id)]; ok {
		t.Fatal("RemoveAt left the provenance record")
	}
	if _, ok := store.seenPackageHash(id); ok {
		t.Fatal("RemoveAt left the seen package hash")
	}
}

func TestRemoveAtHonorsLeaseAndCancelsPending(t *testing.T) {
	sec := &memSecrets{m: map[string][]byte{}}
	store, manager := newTestManager(t, sec)
	installed := installTest(t, manager, personalPackage(t, "local.personal.removed", "1.0.0"))
	pending := proposeTest(t, manager, personalPackage(t, installed.PluginID, "2.0.0"), "update", installed.PackageHash)
	other := proposeTest(t, manager, personalPackage(t, "local.personal.stay", "1.0.0"), "install", "")
	inst, err := store.Get(installed.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	release, err := manager.TryExecution(installed.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	err = manager.RemoveAt(inst.Dir)
	requireCode(t, err, "APP_BUSY")
	release()
	if _, statErr := os.Lstat(inst.Dir); statErr != nil {
		t.Fatal("busy RemoveAt deleted the folder")
	}
	if got, _ := manager.GetInstallation(pending.RequestID); got.Status != "pending" {
		t.Fatal("a refused RemoveAt cancelled the pending update")
	}
	if err := manager.RemoveAt(inst.Dir); err != nil {
		t.Fatal(err)
	}
	if got, getErr := manager.GetInstallation(pending.RequestID); getErr != nil || got.Status != "cancelled" {
		t.Fatalf("RemoveAt left the pending update %s %v", got.Status, getErr)
	}
	if got, getErr := manager.GetInstallation(other.RequestID); getErr != nil || got.Status != "pending" {
		t.Fatalf("RemoveAt cancelled another app: %s %v", got.Status, getErr)
	}
}

func TestRemoveAtRejectsSymlink(t *testing.T) {
	store := NewStore(t.TempDir(), &memSecrets{m: map[string][]byte{}})
	target := t.TempDir()
	writePlugin(t, target, storeManifest, `export function page() { return {}; }`)
	link := filepath.Join(store.Dir(), "com.example.store")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := store.RemoveAt(link)
	requireCode(t, err, "INVALID_PLUGIN_DIR")
	if _, statErr := os.Lstat(filepath.Join(target, "plugin.json")); statErr != nil {
		t.Fatal("RemoveAt followed the symlink")
	}
	if info, statErr := os.Lstat(link); statErr != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatal("RemoveAt removed the symlink instead of refusing it")
	}
}

func TestRemoveAtDeletesSecretsByFolderIDNotManifestID(t *testing.T) {
	sec := &memSecrets{m: map[string][]byte{
		secretKey("com.example.victim", "token"): []byte("keep-victim"),
		secretKey("com.example.evil", "token"):   []byte("evil-own"),
	}}
	store := NewStore(t.TempDir(), sec)
	manifest := strings.Replace(storeManifest, "com.example.store", "com.example.victim", 1)
	evil := filepath.Join(store.Dir(), "com.example.evil")
	writePlugin(t, evil, manifest, `export function page() { return {}; }`)
	if err := store.RemoveAt(evil); err != nil {
		t.Fatal(err)
	}
	if string(sec.m[secretKey("com.example.victim", "token")]) != "keep-victim" {
		t.Fatal("RemoveAt deleted the credential of the id claimed in the manifest")
	}
	if _, ok := sec.m[secretKey("com.example.evil", "token")]; ok {
		t.Fatal("RemoveAt left the credential keyed by the folder id")
	}
}

func TestRemoveRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	sec := &memSecrets{m: map[string][]byte{}}
	store := NewStore(filepath.Join(root, "plugins"), sec)
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	id := "com.example.store"
	if err := store.SetSetting(id, "token", "keep"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(store.Dir(), id)
	real := dir + ".real"
	if err := os.Rename(dir, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, dir); err != nil {
		t.Fatal(err)
	}
	err := store.Remove(id)
	requireCode(t, err, "INVALID_PLUGIN_DIR")
	if _, statErr := os.Lstat(filepath.Join(real, "plugin.json")); statErr != nil {
		t.Fatal("remove deleted the symlink target")
	}
	if string(sec.m[secretKey(id, "token")]) != "keep" {
		t.Fatal("remove deleted credentials through the symlink")
	}
}
