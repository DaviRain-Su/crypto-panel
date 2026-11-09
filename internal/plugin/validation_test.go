package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const personalManifest = `{"id":"local.personal.gas","name":"个人应用","version":"1.0.0","main":"index.js","permissions":{"chains":["eip155:8453"],"storageKb":4},"settings":[{"key":"plain","label":"普通"},{"key":"token","label":"凭据","secret":true}],"navigation":[{"title":"概览","page":"overview"}]}`
const personalSource = `export function page() { return { title: "个人应用" }; }`

func personalPackage(t *testing.T, id, version string) Package {
	t.Helper()
	m, err := ParseManifest([]byte(personalManifest))
	if err != nil {
		t.Fatal(err)
	}
	m.ID, m.Version = id, version
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return Package{Plugin: data, Main: personalSource}
}

func newTestManager(t *testing.T, secrets Secrets) (*Store, *Manager) {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "plugins"), secrets)
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, m
}

func TestDecodeJSONStrict(t *testing.T) {
	type payload struct {
		Values map[string]string `json:"values"`
	}
	for _, body := range []string{
		`{"values":{"x":"a","x":"b"}}`,
		`{"values":{"token":"a","token":"b"}}`,
		`{"values":{},"values":{}}`,
		`{"Values":{}}`, `{"values":{},"unknown":true}`,
		`{"values":{"x":null}}`, `{"values":{"x":1}}`,
		`{"values":{}} {}`, `{"values":{}} true`, `null`,
	} {
		t.Run(body, func(t *testing.T) {
			var p payload
			if err := DecodeJSON([]byte(body), &p); err == nil {
				t.Fatal("接受了不严格的 JSON")
			}
		})
	}
	var good payload
	if err := DecodeJSON([]byte(`{"values":{"x":"a"}}  `), &good); err != nil || good.Values["x"] != "a" {
		t.Fatalf("正常 JSON: %v", err)
	}
	for _, body := range []string{
		strings.Replace(personalManifest, `"name":"个人应用"`, `"name":"个人应用","name":"伪造"`, 1),
		strings.Replace(personalManifest, `"storageKb":4`, `"storageKb":4,"storageKb":0`, 1),
		personalManifest + `true`,
	} {
		if _, err := ParseManifest([]byte(body)); err == nil {
			t.Fatal("清单未共用严格 JSON")
		}
	}
}

func TestValidationNormalizesSourcesWithoutExecuting(t *testing.T) {
	s, manager := newTestManager(t, nil)
	pkg := personalPackage(t, "local.personal.gas", "1.0.0")
	pkg.Main = `while (true) {} host.fetch("https://must-not-run.example"); ` + personalSource
	root := t.TempDir()
	folder := filepath.Join(root, "app")
	writePlugin(t, folder, string(pkg.Plugin), pkg.Main)
	if err := os.WriteFile(filepath.Join(folder, "state.json"), []byte(`{"enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "install.json"), []byte(`{"official":true,"hash":"forged"}`), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(pkg)
	file := filepath.Join(root, "app.cryptoapp.json")
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	var hash string
	for _, source := range []Source{{Kind: "inline", Package: &pkg}, {Kind: "folder", Path: folder}, {Kind: "file", Path: file}} {
		res, err := manager.Validate(source)
		if err != nil || !res.Valid || res.Summary == nil || len(res.Errors) != 0 {
			t.Fatalf("校验失败: %+v %v", res, err)
		}
		if hash == "" {
			hash = res.PackageHash
		}
		if res.PackageHash != hash || len(hash) != 71 {
			t.Fatalf("规范化摘要不一致: %+v", res)
		}
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatal("校验创建了活跃应用")
	}
}

func TestValidationRejectsAttacksAndReportsSafeErrors(t *testing.T) {
	_, manager := newTestManager(t, nil)
	for _, tc := range []struct {
		name string
		edit func(*Package)
	}{
		{"syntax", func(p *Package) { p.Main = `export function page( {` }},
		{"unknown", func(p *Package) {
			p.Plugin = []byte(strings.Replace(string(p.Plugin), `"name":`, `"official":true,"name":`, 1))
		}},
		{"duplicate", func(p *Package) {
			p.Plugin = []byte(strings.Replace(string(p.Plugin), `"name":`, `"id":"local.attacker.alt","name":`, 1))
		}},
		{"main", func(p *Package) { p.Plugin = []byte(strings.Replace(string(p.Plugin), `index.js`, `../bad.js`, 1)) }},
		{"chain", func(p *Package) {
			p.Plugin = []byte(strings.Replace(string(p.Plugin), `eip155:8453`, `eip155:9999999`, 1))
		}},
		{"single path", func(p *Package) {
			p.Plugin = []byte(strings.Replace(string(p.Plugin), `index.js`, `nested/main.mjs`, 1))
		}},
		{"duplicate setting", func(p *Package) {
			p.Plugin = []byte(strings.Replace(string(p.Plugin), `"key":"token"`, `"key":"plain"`, 1))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := personalPackage(t, "local.personal.gas", "1.0.0")
			tc.edit(&p)
			r, err := manager.Validate(Source{Kind: "inline", Package: &p})
			if err != nil || r.Valid || len(r.Errors) == 0 || r.PackageHash != "" {
				t.Fatalf("错误应用未结构化拒绝: %+v %v", r, err)
			}
		})
	}
	for _, src := range []Source{{Kind: "folder", Path: "relative"}, {Kind: "file", Path: "https://example.com/a"}, {Kind: "inline", Path: "/tmp/nope"}, {Kind: "folder", Path: "/missing/does-not-exist"}} {
		if _, err := manager.Validate(src); err == nil {
			t.Fatalf("接受非法来源 %+v", src)
		}
	}
	for _, name := range []string{"index.js", "state.json", "nested.js"} {
		t.Run("symlink "+name, func(t *testing.T) {
			dir := t.TempDir()
			writePlugin(t, dir, personalManifest, personalSource)
			_ = os.Remove(filepath.Join(dir, name))
			if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			r, err := manager.Validate(Source{Kind: "folder", Path: dir})
			if err == nil && r.Valid {
				t.Fatal("接受链接")
			}
		})
	}
	p := personalPackage(t, "local.personal.gas", "1.0.0")
	p.Main = strings.Repeat(" ", maxSourceBytes+1)
	if _, err := manager.Validate(Source{Kind: "inline", Package: &p}); err == nil {
		t.Fatal("超限主模块未拒绝")
	}
}

func TestOfficialPackageUsesEmbeddedSource(t *testing.T) {
	_, manager := newTestManager(t, nil)
	for _, id := range officialExampleIDs {
		p, hash, err := OfficialPackage(id)
		if err != nil || len(p.Main) == 0 || len(p.Plugin) == 0 {
			t.Fatalf("模板读取: %v", err)
		}
		v, err := manager.Validate(Source{Kind: "inline", Package: &p})
		if err != nil || !v.Valid || v.PackageHash != hash || v.Summary.ID != id {
			t.Fatalf("模板摘要不一致: %+v %v", v, err)
		}
	}
	if _, _, err := OfficialPackage("local.personal.gas"); err == nil {
		t.Fatal("任意应用被当成模板")
	}
}
