package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVersion writes a plugin folder with the given id and version.
func writeVersion(t *testing.T, dir, id, version, source string) {
	t.Helper()
	manifest := fmt.Sprintf(`{
  "id": %q,
  "name": "Test plugin",
  "version": %q,
  "main": "index.js",
  "permissions": { "storageKb": 1 },
  "settings": [ { "key": "plain", "label": "Plain" } ]
}`, id, version)
	writePlugin(t, dir, manifest, source)
}

// signEntry builds the registry entry for dir, signed with key.
func signEntry(t *testing.T, dir, url string, key ed25519.PrivateKey) RegistryEntry {
	t.Helper()
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	digest, err := DigestDir(dir)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	return RegistryEntry{
		ID:        m.ID,
		Name:      m.Name,
		Version:   m.Version,
		URL:       url,
		Hash:      digest,
		Signature: SignDigest(key, digest),
		Publisher: PublicKeyString(key.Public().(ed25519.PublicKey)),
	}
}

func TestDigestSkipsShellFilesAndJunk(t *testing.T) {
	dir := t.TempDir()
	writeVersion(t, dir, "com.example.digest", "1.0.0", "export function page() { return {}; }")
	first, err := DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"state.json":   `{"enabled":true}`,
		"kv.json":      `{}`,
		"install.json": `{}`,
		".DS_Store":    "junk",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	second, err := DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("the shell's files changed the digest: %s != %s", first, second)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("a changed module kept the digest")
	}
}

func TestSignAndVerifyDigest(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("ab", 32)
	sig := SignDigest(priv, digest)
	if err := VerifyDigest(digest, sig, PublicKeyString(pub)); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if err := VerifyDigest(digest, sig, PublicKeyString(other)); err == nil {
		t.Fatal("a signature verified under another key")
	}
	if err := VerifyDigest(digest+"ff", sig, PublicKeyString(pub)); err == nil {
		t.Fatal("a signature verified over another digest")
	}
}

func TestParseRegistry(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.registry", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/p.zip", priv)
	valid := Registry{Version: 1, Name: "Test", Plugins: []RegistryEntry{entry}}
	data, _ := json.Marshal(valid)
	if _, err := ParseRegistry(data); err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}

	bad := func(mutate func(*Registry)) []byte {
		reg := valid
		reg.Plugins = append([]RegistryEntry(nil), valid.Plugins...)
		mutate(&reg)
		out, _ := json.Marshal(reg)
		return out
	}
	cases := map[string][]byte{
		"version":   bad(func(r *Registry) { r.Version = 2 }),
		"duplicate": bad(func(r *Registry) { r.Plugins = append(r.Plugins, entry) }),
		"hash":      bad(func(r *Registry) { r.Plugins[0].Hash = "sha256:xyz" }),
		"signature": bad(func(r *Registry) { r.Plugins[0].Signature = "ed25519:nope" }),
		"publisher": bad(func(r *Registry) { r.Plugins[0].Publisher = "ed25519:nope" }),
		"remote http": bad(func(r *Registry) {
			r.Plugins[0].URL = "http://example.com/p.zip"
		}),
		"embedded path": bad(func(r *Registry) {
			r.Plugins[0].URL = "embedded:../p.zip"
		}),
		"trailing": append(append([]byte(nil), data...), []byte("{}")...),
	}
	for name, data := range cases {
		if _, err := ParseRegistry(data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestValidURLs(t *testing.T) {
	for _, raw := range []string{
		"https://example.com/registry.json",
		"http://127.0.0.1:8080/registry.json",
		"http://localhost:8080/registry.json",
		"embedded:gas.zip",
	} {
		if !ValidEntryURL(raw) {
			t.Errorf("ValidEntryURL(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"http://example.com/registry.json",
		"ftp://example.com/registry.json",
		"https://user:pass@example.com/registry.json",
		"embedded:../gas.zip",
		"embedded:gas.txt",
		"not a url",
	} {
		if ValidEntryURL(raw) {
			t.Errorf("ValidEntryURL(%q) = true", raw)
		}
	}
	if ValidRegistryURL("embedded:gas.zip") {
		t.Error("a registry URL may not be embedded")
	}
}

func TestPrepareInstallsSignedPlugin(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.signed", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/signed.zip", priv)
	data, err := ZipDir(src)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(root, "plugins"), nil)
	ar, err := store.Prepare(data, entry)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if ar.Manifest.ID != entry.ID {
		t.Fatalf("manifest = %+v", ar.Manifest)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	inst, err := store.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !inst.Signed || inst.Official || inst.Source != entry.URL {
		t.Fatalf("installed = %+v", inst)
	}
	if inst.Err != "" {
		t.Fatalf("Err = %q", inst.Err)
	}

	// Changing a file after the install breaks the hash the entry signed.
	if err := os.WriteFile(filepath.Join(inst.Dir, "index.js"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst, err = store.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get after tampering: %v", err)
	}
	if !strings.Contains(inst.Err, "changed") {
		t.Fatalf("Err after tampering = %q", inst.Err)
	}
}

func TestPrepareRefusesUnsignedEntry(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.unsigned", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/p.zip", priv)
	entry.Signature = ""
	data, _ := ZipDir(src)
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Prepare(data, entry); err == nil {
		t.Fatal("an unsigned entry was accepted")
	}
}

func TestPrepareRefusesTamperedArchive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.tampered", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/p.zip", priv)
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := ZipDir(src)
	store := NewStore(filepath.Join(root, "plugins"), nil)
	_, err := store.Prepare(data, entry)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareRefusesZipTraversal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.traversal", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/p.zip", priv)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../evil.js")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("x"))
	zw.Close()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Prepare(buf.Bytes(), entry); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.symlink", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/p.zip", priv)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: "index.js"}
	header.SetMode(fs.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("plugin.json"))
	zw.Close()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Prepare(buf.Bytes(), entry); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareRefusesSameVersionOtherContent(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	_, priv, _ := ed25519.GenerateKey(nil)
	first := filepath.Join(root, "first")
	writeVersion(t, first, "com.example.replay", "1.0.0", "export function page() { return 1; }")
	entry := signEntry(t, first, "https://example.com/p.zip", priv)
	data, _ := ZipDir(first)
	ar, err := store.Prepare(data, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(root, "second")
	writeVersion(t, second, "com.example.replay", "1.0.0", "export function page() { return 2; }")
	replay := signEntry(t, second, "https://example.com/p.zip", priv)
	data, _ = ZipDir(second)
	if _, err := store.Prepare(data, replay); err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareRefusesDowngrade(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	_, priv, _ := ed25519.GenerateKey(nil)
	newer := filepath.Join(root, "newer")
	writeVersion(t, newer, "com.example.downgrade", "1.1.0", "export function page() { return 1; }")
	entry := signEntry(t, newer, "https://example.com/p.zip", priv)
	data, _ := ZipDir(newer)
	ar, err := store.Prepare(data, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatal(err)
	}

	older := filepath.Join(root, "older")
	writeVersion(t, older, "com.example.downgrade", "1.0.0", "export function page() { return 2; }")
	oldEntry := signEntry(t, older, "https://example.com/p.zip", priv)
	data, _ = ZipDir(older)
	if _, err := store.Prepare(data, oldEntry); err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareRefusesPublisherChange(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	_, firstKey, _ := ed25519.GenerateKey(nil)
	first := filepath.Join(root, "first")
	writeVersion(t, first, "com.example.publisher", "1.0.0", "export function page() { return 1; }")
	entry := signEntry(t, first, "https://example.com/p.zip", firstKey)
	data, _ := ZipDir(first)
	ar, err := store.Prepare(data, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatal(err)
	}

	_, secondKey, _ := ed25519.GenerateKey(nil)
	second := filepath.Join(root, "second")
	writeVersion(t, second, "com.example.publisher", "1.1.0", "export function page() { return 2; }")
	next := signEntry(t, second, "https://example.com/p.zip", secondKey)
	data, _ = ZipDir(second)
	if _, err := store.Prepare(data, next); err == nil || !strings.Contains(err.Error(), "publisher") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateCarriesSettingsAndData(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	_, priv, _ := ed25519.GenerateKey(nil)
	first := filepath.Join(root, "first")
	writeVersion(t, first, "com.example.update", "1.0.0", "export function page() { return 1; }")
	entry := signEntry(t, first, "https://example.com/p.zip", priv)
	data, _ := ZipDir(first)
	ar, err := store.Prepare(data, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(entry.ID, "plain", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEnabled(entry.ID, false); err != nil {
		t.Fatal(err)
	}
	kv, err := store.KV(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Put("k", []byte(`"v"`)); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(root, "second")
	writeVersion(t, second, "com.example.update", "1.1.0", "export function page() { return 2; }")
	next := signEntry(t, second, "https://example.com/p.zip", priv)
	data, _ = ZipDir(second)
	ar, err = store.Prepare(data, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.Setting(entry.ID, "plain"); v != "hello" {
		t.Fatalf("setting = %q", v)
	}
	inst, err := store.Get(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Enabled {
		t.Fatal("the update turned the plugin back on")
	}
	if inst.Manifest.Version != "1.1.0" {
		t.Fatalf("version = %q", inst.Manifest.Version)
	}
	kv, err = store.KV(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kv.Get("k"); !ok {
		t.Fatal("the plugin's data did not survive the update")
	}
}

func TestGitHubStyleArchive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.github", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	entry := signEntry(t, src, "https://example.com/p.zip", priv)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		w, err := zw.Create("crypto-panel-1.0.0/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()

	store := NewStore(filepath.Join(root, "plugins"), nil)
	ar, err := store.Prepare(buf.Bytes(), entry)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := ar.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	inst, err := store.Get(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inst.Dir, "index.js")); err != nil {
		t.Fatalf("the archive was not unpacked at the plugin's root: %v", err)
	}
}

func TestFetchRegistryAndArchive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVersion(t, src, "com.example.fetch", "1.0.0", "export function page() { return {}; }")
	_, priv, _ := ed25519.GenerateKey(nil)
	archive, err := ZipDir(src)
	if err != nil {
		t.Fatal(err)
	}
	var entry RegistryEntry
	mux := http.NewServeMux()
	mux.HandleFunc("/registry.json", func(w http.ResponseWriter, r *http.Request) {
		entry.URL = "http://" + r.Host + "/p.zip"
		reg := Registry{Version: 1, Name: "Test", Plugins: []RegistryEntry{entry}}
		json.NewEncoder(w).Encode(reg)
	})
	mux.HandleFunc("/p.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	entry = signEntry(t, src, srv.URL+"/p.zip", priv)

	reg, err := FetchRegistry(t.Context(), srv.Client(), srv.URL+"/registry.json")
	if err != nil {
		t.Fatalf("FetchRegistry: %v", err)
	}
	if len(reg.Plugins) != 1 || reg.Plugins[0].ID != entry.ID {
		t.Fatalf("registry = %+v", reg)
	}
	data, err := FetchArchive(t.Context(), srv.Client(), reg.Plugins[0].URL)
	if err != nil {
		t.Fatalf("FetchArchive: %v", err)
	}
	store := NewStore(filepath.Join(root, "plugins"), nil)
	ar, err := store.Prepare(data, reg.Plugins[0])
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	ar.Close()

	if _, err := FetchRegistry(t.Context(), srv.Client(), srv.URL+"/missing.json"); err == nil {
		t.Fatal("a 404 was accepted")
	}
}
