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
		`{"values":{"x":1}}`,
		`{"values":{}} {}`, `{"values":{}} true`,
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
	// 显式 null 与 encoding/json 一致：no-op，后续字段仍正常解码。
	var nullable payload
	if err := DecodeJSON([]byte(`{"values":{"x":null,"y":"a"}}`), &nullable); err != nil || nullable.Values["y"] != "a" || nullable.Values["x"] != "" {
		t.Fatalf("null 语义: %v %#v", err, nullable.Values)
	}
	var top payload
	if err := DecodeJSON([]byte(`null`), &top); err != nil || top.Values != nil {
		t.Fatalf("顶层 null 语义: %v %#v", err, top)
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

func TestPackageHashMatchesCanonicalDigestWithoutCopying(t *testing.T) {
	hashTmp := t.TempDir()
	t.Setenv("TMPDIR", hashTmp)
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	src := filepath.Join(root, "src")
	writePlugin(t, src, personalManifest, personalSource)
	if err := os.MkdirAll(filepath.Join(src, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "lib", "helper.js"), []byte("export const n = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(store.Dir(), "local.personal.gas")
	manifest, err := LoadManifest(installed)
	if err != nil {
		t.Fatal(err)
	}
	pretty, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	pretty = append(pretty, '\n')
	if err := os.WriteFile(filepath.Join(installed, "plugin.json"), pretty, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "state.json"), []byte(`{"enabled":true,"settings":{"plain":"secret-looking"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.PackageHash(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(installed, "plugin.json"))
	if err != nil || string(after) != string(pretty) {
		t.Fatal("摘要计算改写了应用文件")
	}
	oracle := filepath.Join(root, "oracle")
	if err := filepath.WalkDir(installed, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(installed, path)
		if err != nil {
			return err
		}
		target := filepath.Join(oracle, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oracle, "plugin.json"), append(canonical, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := DigestDir(oracle)
	if err != nil || got != want {
		t.Fatalf("摘要 %s，规范化结果 %s (%v)", got, want, err)
	}
	again, err := store.PackageHash(manifest.ID)
	if err != nil || again != got {
		t.Fatalf("重复摘要 %s %v", again, err)
	}
	entries, err := os.ReadDir(hashTmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "lintel-hash-") {
			t.Fatal("计算摘要时复制了整棵应用树")
		}
	}
}

// 深度优先遍历会先看到 n/a.js，再看到 n.js；按路径排序则相反。
func TestPackageHashFollowsSortedPathOrder(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "plugins"), nil)
	src := filepath.Join(root, "src")
	writePlugin(t, src, personalManifest, personalSource)
	if err := os.Mkdir(filepath.Join(src, "n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "n", "a.js"), []byte("export const nested = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "n.js"), []byte("export const sibling = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(store.Dir(), "local.personal.gas")
	manifest, err := LoadManifest(installed)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "plugin.json"), append(canonical, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.PackageHash(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := DigestDir(installed)
	if err != nil || got != want {
		t.Fatalf("摘要 %s，按路径排序 %s (%v)", got, want, err)
	}
}

func TestPackageHashRejectsSymlinkedShellFiles(t *testing.T) {
	s, m := newTestManager(t, nil)
	installed := installTest(t, m, personalPackage(t, "local.personal.gas", "1.0.0"))
	appDir := filepath.Join(s.Dir(), installed.PluginID)
	outside := filepath.Join(t.TempDir(), "outside.json")
	const marker = "kvlink-probe-secret"
	if err := os.WriteFile(outside, []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"state.json", "kv.json"} {
		path := filepath.Join(appDir, name)
		_ = os.Remove(path)
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PackageHash(installed.PluginID); err == nil {
		t.Fatal("壳文件符号链接仍可计算摘要")
	}
	next := personalPackage(t, installed.PluginID, "2.0.0")
	if _, err := m.Propose(Source{Kind: "inline", Package: &next}, "update", installed.PluginID, installed.PackageHash, "local-api"); err == nil {
		t.Fatal("更新被批准")
	}
	got, err := s.Get(installed.PluginID)
	if err != nil || got.Manifest.Version != "1.0.0" {
		t.Fatalf("已安装版本被替换: %+v %v", got.Manifest.Version, err)
	}
	for _, name := range []string{"state.json", "kv.json"} {
		info, err := os.Lstat(filepath.Join(appDir, name))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s 不再是指向外部文件的符号链接", name)
		}
	}
	outsideData, err := os.ReadFile(outside)
	if err != nil || string(outsideData) != marker {
		t.Fatal("外部文件被改写")
	}
	err = filepath.WalkDir(s.Dir(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), marker) {
			t.Fatalf("外部内容被复制进 %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackageHashRejectsSpecialOrJunkLinksBeforeSkip(t *testing.T) {
	for _, kind := range []string{"backslash", "junk-symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, m := newTestManager(t, nil)
			installed := installTest(t, m, personalPackage(t, "local.personal.gas", "1.0.0"))
			appDir := filepath.Join(s.Dir(), installed.PluginID)
			switch kind {
			case "backslash":
				if err := os.WriteFile(filepath.Join(appDir, `bad\name.js`), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "junk-symlink":
				outside := filepath.Join(t.TempDir(), "junk")
				if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(appDir, ".DS_Store")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.PackageHash(installed.PluginID); err == nil {
				t.Fatal("被旧摘要规则拒绝的文件树通过了")
			}
		})
	}
}
