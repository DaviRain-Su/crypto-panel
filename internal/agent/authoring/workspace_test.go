package authoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/plugin"
)

// 凭据探针让修改准备阶段的意外读取直接失败。
type secretProbe struct{ reads int }

func (s *secretProbe) Get(string) ([]byte, error) {
	s.reads++
	return nil, errors.New("不应读取凭据")
}
func (*secretProbe) Set(string, []byte) error { return nil }
func (*secretProbe) Delete(string) error      { return nil }

func TestUpdateCopiesCompleteCodeWithoutStateOrSecrets(t *testing.T) {
	probe := &secretProbe{}
	s, p, _, store := harnessSecrets(t, probe)
	// 使用现有的本地文件夹安装接口准备旧版，不调用 Decide。
	storeWithSecrets := store
	old := t.TempDir()
	if err := writeApp(old, "local.personal.existing", "1.0.0", testCode); err != nil {
		t.Fatal(err)
	}
	if _, err := storeWithSecrets.Install(old); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(store.Dir(), "local.personal.existing")
	if err := os.MkdirAll(filepath.Join(existing, "src", "lib"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"src/main.mjs": testCode, "src/lib/helper.js": "export const helper = 1;", "asset.txt": "non-secret resource"} {
		if err := os.WriteFile(filepath.Join(existing, filepath.FromSlash(name)), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := testPackage("local.personal.existing", "1.0.0", testCode)
	var meta plugin.Manifest
	if err := json.Unmarshal(manifest.Plugin, &meta); err != nil {
		t.Fatal(err)
	}
	meta.Main = "src/main.mjs"
	meta.Permissions.StorageKb = 4
	meta.Settings = []plugin.SettingField{{Key: "apiKey", Label: "凭据", Secret: true}}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(existing, "plugin.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	baseline, err := storeWithSecrets.PackageHash(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	secret := hex.EncodeToString(random)
	for _, name := range []string{"state.json", "kv.json", "install.json", "settings.json", "secrets.json", "credentials.json", ".seeded-official", ".env", ".env.local", "private.key", "src/state.json"} {
		if err := os.WriteFile(filepath.Join(existing, filepath.FromSlash(name)), []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"settings", "credentials", ".git", ".ssh"} {
		if err := os.Mkdir(filepath.Join(existing, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(existing, name, "private"), []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// 安装记录使用合法的空记录，避免测试夹具破坏 Manager 的来源检查。
	if err := os.Remove(filepath.Join(existing, "install.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "state.json"), []byte(`{"enabled":true,"settings":{"label":"`+secret+`"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "kv.json"), []byte(`{"data":"`+secret+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	baseline, err = storeWithSecrets.PackageHash(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := testRequest(t)
	req.ExistingDir = existing
	req.TargetID = meta.ID
	req.ExpectedHash = baseline
	p.prompt = func(_ context.Context, _, text string) (acp.PromptResult, error) {
		if !strings.Contains(text, meta.ID) || !strings.Contains(text, "提高版本") {
			t.Fatal("修改约定缺失")
		}
		app := filepath.Join(p.cfg.Cwd, "app")
		for _, name := range []string{"plugin.json", "index.js", "src/main.mjs", "src/lib/helper.js", "asset.txt"} {
			if _, err := os.Stat(filepath.Join(app, filepath.FromSlash(name))); err != nil {
				t.Fatalf("代码树未保留 %s", name)
			}
		}
		err := filepath.WalkDir(app, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err == nil && strings.Contains(string(contents), secret) {
				t.Error("复制泄露设置 / 凭据")
			}
			return err
		})
		if err != nil {
			return acp.PromptResult{}, err
		}
		for _, name := range []string{"state.json", "kv.json", "install.json", "settings.json", "secrets.json", "credentials.json", ".env", ".env.local", "private.key", "src/state.json", "settings", "credentials", ".git", ".ssh"} {
			if _, err := os.Lstat(filepath.Join(app, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("敏感项未排除 %s", name)
			}
		}
		meta.Version = "1.1.0"
		next, _ := json.Marshal(meta)
		err = os.WriteFile(filepath.Join(app, "plugin.json"), next, 0600)
		return acp.PromptResult{StopReason: "end_turn"}, err
	}
	out, err := s.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Installation.Operation != "update" || out.Installation.PluginID != meta.ID || out.Installation.ExpectedHash != baseline || out.Installation.Status != "pending" {
		t.Fatalf("更新未固定身份: %+v", out.Installation)
	}
	if probe.reads != 0 {
		t.Fatal("生成读取了凭据")
	}
	before, err := store.Get(meta.ID)
	if err != nil || before.Manifest.Version != "1.0.0" {
		t.Fatal("生成直接修改了活跃版本")
	}
}

func TestCopyRejectsLinksUnsafePathsAndOverlap(t *testing.T) {
	for _, kind := range []string{"root symlink", "file symlink", "excluded symlink", "path escape", "overlap"} {
		t.Run(kind, func(t *testing.T) {
			s, _, m, _ := harness(t)
			req := testRequest(t)
			src := t.TempDir()
			if err := writeApp(src, "local.personal.copy", "1.0.0", testCode); err != nil {
				t.Fatal(err)
			}
			req.ExistingDir = src
			req.TargetID = "local.personal.copy"
			req.ExpectedHash = "sha256:" + strings.Repeat("0", 64)
			switch kind {
			case "root symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(src, alias); err != nil {
					t.Fatal(err)
				}
				req.ExistingDir = alias
			case "file symlink", "excluded symlink":
				name := "escape.js"
				if kind == "excluded symlink" {
					name = "state.json"
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(src, name)); err != nil {
					t.Fatal(err)
				}
			case "path escape":
				if err := os.WriteFile(filepath.Join(src, `bad\name.js`), []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
			case "overlap":
				req.WorkspaceRoot = src
			}
			if _, err := s.Run(context.Background(), req); err == nil {
				t.Fatal("接受了不安全复制")
			}
			requireNoProposal(t, m)
		})
	}
}

func TestCopyLimitsAndBoundary(t *testing.T) {
	for _, kind := range []string{"file limit", "total limit", "count limit", "exact count", "exact limits"} {
		t.Run(kind, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			files, size := 1, 2<<20
			switch kind {
			case "file limit":
				size++
			case "total limit":
				files = 5
			case "count limit":
				files = 201
				size = 1
			case "exact count":
				files = 200
				size = 1
			case "exact limits":
				files = 4
			}
			for i := 0; i < files; i++ {
				if err := os.WriteFile(filepath.Join(src, fmt.Sprintf("f%03d", i)), make([]byte, size), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := copyExisting(context.Background(), src, dst)
			if kind == "exact limits" || kind == "exact count" {
				if err != nil {
					t.Fatalf("拒绝合法边界: %v", err)
				}
			} else if !errors.Is(err, ErrSourceLimit) {
				t.Fatalf("超限未失败: %v", err)
			}
		})
	}
}

func TestCopyDirectoryEntryBudget(t *testing.T) {
	src := t.TempDir()
	for i := 0; i < maxCopyEntries; i++ {
		if err := os.Mkdir(filepath.Join(src, fmt.Sprintf("d%04d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyExisting(context.Background(), src, t.TempDir()); err != nil {
		t.Fatalf("拒绝合法目录边界: %v", err)
	}
	if err := os.Mkdir(filepath.Join(src, "overflow"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyExisting(context.Background(), src, t.TempDir()); !errors.Is(err, ErrSourceLimit) {
		t.Fatalf("无文件目录膨胀未封顶: %v", err)
	}
}

func TestCopyCancelledBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyExisting(ctx, t.TempDir(), t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消复制: %v", err)
	}
}
