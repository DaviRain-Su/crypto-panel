package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
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
		w, err := zw.Create("lintel-1.0.0/" + filepath.ToSlash(rel))
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

func TestSignedInstallRejectsLocalIDs(t *testing.T) {
	if err := (RegistryEntry{ID: "local.personal.store"}).Validate(); err == nil || !strings.Contains(err.Error(), "local.*") {
		t.Fatalf("Validate: %v", err)
	}
	store := NewStore(t.TempDir(), nil)
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	localDir := t.TempDir()
	writeVersion(t, localDir, "local.personal.store", "1.0.0", `export function page(){return {title:"Local"};}`)
	localEntry := signEntry(t, localDir, "https://example.com/local.zip", priv)
	localZip, err := ZipDir(localDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prepare(localZip, localEntry); err == nil || !strings.Contains(err.Error(), "local.*") {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := store.Install(localDir); err != nil {
		t.Fatalf("目录安装仍应允许 local.*: %v", err)
	}
	signedDir := t.TempDir()
	writeVersion(t, signedDir, "com.example.signedapp", "1.0.0", `export function page(){return {title:"Signed"};}`)
	signedEntry := signEntry(t, signedDir, "https://example.com/signed.zip", priv)
	signedZip, err := ZipDir(signedDir)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := store.Prepare(signedZip, signedEntry)
	if err != nil {
		t.Fatal(err)
	}
	ready.Close()
}

func TestParseRegistrySkipsLocalEntries(t *testing.T) {
	dir := t.TempDir()
	writeVersion(t, dir, "com.example.catalog", "1.0.0", `export function page(){return {title:"Catalog"};}`)
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	good := signEntry(t, dir, "https://example.com/catalog.zip", priv)
	local := good
	local.ID = "local.personal.legacy"
	broken := good
	broken.ID = "com.example.broken"
	broken.Hash = ""
	body, err := json.Marshal(Registry{Version: 1, Name: "Mixed", Plugins: []RegistryEntry{local, good}})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := ParseRegistry(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Plugins) != 1 || reg.Plugins[0].ID != good.ID {
		t.Fatalf("目录 = %+v", reg.Plugins)
	}
	onlyLocal, err := json.Marshal(Registry{Version: 1, Name: "Old", Plugins: []RegistryEntry{local}})
	if err != nil {
		t.Fatal(err)
	}
	reg, err = ParseRegistry(onlyLocal)
	if err != nil || len(reg.Plugins) != 0 {
		t.Fatalf("只有 local.* 的目录: %+v %v", reg.Plugins, err)
	}
	bad, err := json.Marshal(Registry{Version: 1, Name: "Bad", Plugins: []RegistryEntry{broken}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRegistry(bad); err == nil {
		t.Fatal("非 local.* 的无效条目被跳过了")
	}
}

func TestParseRegistrySkipLogIsQuotedAndCapped(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	nasty := "local.bad\r\ninjected\x1b[31m"
	long := "local." + strings.Repeat("a", 80)
	plugins := []RegistryEntry{{ID: nasty}, {ID: long}}
	for i := 0; i < 4; i++ {
		plugins = append(plugins, RegistryEntry{ID: fmt.Sprintf("local.personal.extra%d", i)})
	}
	body, err := json.Marshal(Registry{Version: 1, Name: "Skip", Plugins: plugins})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := ParseRegistry(body)
	if err != nil || len(reg.Plugins) != 0 {
		t.Fatalf("跳过 local.* 失败: %+v %v", reg.Plugins, err)
	}
	out := buf.String()
	if strings.Contains(out, "\ninjected") || strings.Contains(out, "\r") {
		t.Fatalf("跳过日志写入了未引用的控制字符: %q", out)
	}
	if !strings.Contains(out, `\n`) || !strings.Contains(out, `\r`) || !strings.Contains(out, `\x1b`) {
		t.Fatalf("跳过日志没有按引用转义: %q", out)
	}
	if strings.Contains(out, strings.Repeat("a", 80)) {
		t.Fatalf("跳过日志没有截断 id: %q", out)
	}
	if got := strings.Count(out, "skipping registry entry"); got != 5 {
		t.Fatalf("跳过日志条数 = %d, 正文 %q", got, out)
	}
	if !strings.Contains(out, "skipped 1 more local.* registry entries") {
		t.Fatalf("没有汇总被省略的条目: %q", out)
	}
	buf.Reset()
	wide := "local." + strings.Repeat("中", 80)
	body, err = json.Marshal(Registry{Version: 1, Name: "Wide", Plugins: []RegistryEntry{{ID: wide}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRegistry(body); err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	if !strings.Contains(logged, strings.Repeat("中", 58)) || strings.Contains(logged, strings.Repeat("中", 59)) {
		t.Fatalf("截断没有按字符而不是字节: %q", logged)
	}
}
