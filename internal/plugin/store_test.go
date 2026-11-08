package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memSecrets struct{ m map[string][]byte }

func (s *memSecrets) Get(key string) ([]byte, error) {
	if v, ok := s.m[key]; ok {
		return v, nil
	}
	return nil, os.ErrNotExist
}

func (s *memSecrets) Set(key string, value []byte) error {
	s.m[key] = value
	return nil
}

func (s *memSecrets) Delete(key string) error {
	delete(s.m, key)
	return nil
}

// writePlugin writes a plugin folder into dir with the given manifest body
// and source.
func writePlugin(t *testing.T, dir, manifest, source string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

const storeManifest = `{
  "id": "com.example.store",
  "name": "Store test",
  "version": "1.0.0",
  "main": "index.js",
  "permissions": { "storageKb": 1 },
  "settings": [
    { "key": "plain", "label": "Plain" },
    { "key": "token", "label": "Token", "secret": true }
  ]
}`

func TestStoreInstallEnableRemove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	secrets := &memSecrets{m: map[string][]byte{}}
	store := NewStore(filepath.Join(root, "plugins"), secrets)

	m, err := store.Install(src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if m.ID != "com.example.store" {
		t.Fatalf("id = %q", m.ID)
	}
	if _, err := store.Install(src); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("second install: %v", err)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 || !list[0].Enabled {
		t.Fatalf("List = %+v, %v", list, err)
	}

	if err := store.SetEnabled(m.ID, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	inst, err := store.Get(m.ID)
	if err != nil || inst.Enabled {
		t.Fatalf("Get = %+v, %v", inst, err)
	}

	if err := store.SetSetting(m.ID, "plain", "hello"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := store.SetSetting(m.ID, "token", "s3cret"); err != nil {
		t.Fatalf("SetSetting secret: %v", err)
	}
	if v, _ := store.Setting(m.ID, "plain"); v != "hello" {
		t.Fatalf("plain = %q", v)
	}
	if v, _ := store.Setting(m.ID, "token"); v != "s3cret" {
		t.Fatalf("token = %q", v)
	}
	if _, err := store.Setting(m.ID, "undeclared"); err == nil {
		t.Fatal("an undeclared setting was accepted")
	}

	if err := store.Remove(m.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("List after Remove = %+v", list)
	}
	if _, err := store.Get(m.ID); err != ErrNotInstalled {
		t.Fatalf("Get after Remove: %v", err)
	}
	if len(secrets.m) != 0 {
		t.Fatalf("secrets left: %v", secrets.m)
	}
}

func TestStoreRefusesBrokenPlugin(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, `{"id": "com.example.broken", "name": "Broken", "version": "1.0.0", "main": "missing.js"}`, "")
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Install(src); err == nil {
		t.Fatal("a plugin whose main is missing was installed")
	}
}

func TestStoreListsBrokenFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(root, "plugins"), nil)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Err == "" {
		t.Fatalf("List = %+v", list)
	}
}

func TestKVQuota(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Install(src); err != nil {
		t.Fatalf("Install: %v", err)
	}
	kv, err := store.KV("com.example.store")
	if err != nil {
		t.Fatalf("KV: %v", err)
	}
	if err := kv.Put("a", []byte(`"x"`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	big := make([]byte, 2<<10)
	for i := range big {
		big[i] = 'a'
	}
	if err := kv.Put("b", append(append([]byte(`"`), big...), '"')); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("quota error = %v", err)
	}
	if err := kv.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, ok := kv.Get("a"); ok {
		t.Fatalf("a = %s after Delete", got)
	}
	if err := kv.Put("b", []byte(`"small"`)); err != nil {
		t.Fatalf("Put after Delete: %v", err)
	}
}

func TestKVRefusesWithoutPermission(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, `{
  "id": "com.example.nostorage",
  "name": "No storage",
  "version": "1.0.0",
  "main": "index.js"
}`, `export function page() { return {}; }`)
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Install(src); err != nil {
		t.Fatalf("Install: %v", err)
	}
	kv, err := store.KV("com.example.nostorage")
	if err != nil {
		t.Fatalf("KV: %v", err)
	}
	if err := kv.Put("a", []byte(`1`)); err == nil || !strings.Contains(err.Error(), "no storage permission") {
		t.Fatalf("err = %v", err)
	}
}
