package plugin

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLifecycleDedupAcrossEntrypointsAndHistoryBounds(t *testing.T) {
	_, m := newTestManager(t, nil)
	pkg := personalPackage(t, "local.personal.dedup", "1.0.0")
	first := proposeTest(t, m, pkg, "install", "")
	_, err := m.Propose(Source{Kind: "inline", Package: &pkg}, "install", "", "", "acp")
	requireCode(t, err, "CANDIDATE_CONFLICT")
	if got, getErr := m.GetInstallation(first.RequestID); getErr != nil || got.Via != "local-api" || got.Status != "pending" {
		t.Fatalf("ACP 附着到了 API 任务: %+v %v", got, getErr)
	}
	_, _ = m.Cancel(first.RequestID)
	for range MaxInstallationHistory + 2 {
		i := proposeTest(t, m, pkg, "install", "")
		_, _ = m.Cancel(i.RequestID)
	}
	m.mu.Lock()
	count := len(m.tasks)
	m.mu.Unlock()
	if count > MaxInstallationHistory {
		t.Fatalf("终态超过上限: %d", count)
	}
	m.mu.Lock()
	for _, task := range m.tasks {
		task.record.CompletedAt = time.Now().Add(-InstallationHistoryTTL - time.Second)
	}
	m.mu.Unlock()
	m.Installations()
	m.mu.Lock()
	count = len(m.tasks)
	m.mu.Unlock()
	if count != 0 {
		t.Fatal("历史超过24小时未清理")
	}
}

func TestVersionOrderingDoesNotOverflowOrCompareNumbersLexically(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0.0-beta.10", "1.0.0-beta.2", 1},
		{"1.0.0-beta.1", "1.0.0-beta", 1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"184467440737095516160.0.0", "184467440737095516159.0.0", 1},
	} {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("版本排序 %s / %s = %d", tc.a, tc.b, got)
		}
	}
}

func TestManifestAndValidationShareSecurityRules(t *testing.T) {
	_, m := newTestManager(t, nil)
	for _, manifest := range []string{
		strings.Replace(personalManifest, "个人应用", "名称"+string(rune(0x202e)), 1),
		strings.Replace(personalManifest, `"page":"overview"`, `"page":"overview"},{"title":"重复","page":"overview"`, 1),
	} {
		if _, err := ParseManifest([]byte(manifest)); err == nil {
			t.Fatal("ParseManifest 未执行共同安全检查")
		}
		p := Package{Plugin: []byte(manifest), Main: personalSource}
		v, err := m.Validate(Source{Kind: "inline", Package: &p})
		if err != nil || v.Valid {
			t.Fatal("源校验未拒绝危险元数据")
		}
	}
	var values struct {
		Values map[string]string `json:"values"`
	}
	escaped := fmt.Sprintf(`{"values":{"x":"a","%cu0078":"b"}}`, 92)
	if err := DecodeJSON([]byte(escaped), &values); err == nil {
		t.Fatal("接受转义后重复的键")
	}
	p := personalPackage(t, "local.personal.multi", "1.0.0")
	p.Plugin = []byte(strings.Replace(strings.Replace(string(p.Plugin), "eip155:8453", "invalid:chain", 1), "个人应用", "", 1))
	v, err := m.Validate(Source{Kind: "inline", Package: &p})
	if err != nil || v.Valid || len(v.Errors) < 2 {
		t.Fatalf("未汇总独立错误: %+v %v", v, err)
	}
}

func TestUpdateRejectsSignedAndSecretTypeMigration(t *testing.T) {
	sec := &memSecrets{m: map[string][]byte{}}
	s, m := newTestManager(t, sec)
	first := installTest(t, m, personalPackage(t, "local.personal.types", "1.0.0"))
	p := personalPackage(t, first.PluginID, "2.0.0")
	p.Plugin = []byte(strings.Replace(string(p.Plugin), `"key":"plain","label":"普通"`, `"key":"plain","label":"普通","secret":true`, 1))
	_, err := m.Propose(Source{Kind: "inline", Package: &p}, "update", first.PluginID, first.PackageHash, "local-api")
	requireCode(t, err, "SETTINGS_MIGRATION_REQUIRED")
	dir := t.TempDir()
	writeVersion(t, dir, "local.personal.signed", "1.0.0", personalSource)
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := signEntry(t, dir, "https://example.com/signed.zip", private)
	archive, err := ZipDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := s.Prepare(archive, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Install(); err != nil {
		t.Fatal(err)
	}
	hash, err := s.PackageHash(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	p = personalPackage(t, entry.ID, "2.0.0")
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "update", entry.ID, hash, "local-api")
	requireCode(t, err, "SOURCE_NOT_ALLOWED")
}

func TestFolderMJSAndLimitsRemainSupported(t *testing.T) {
	_, m := newTestManager(t, nil)
	dir := t.TempDir()
	manifest := strings.Replace(personalManifest, "index.js", "nested/main.mjs", 1)
	writePlugin(t, dir, manifest, personalSource)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "index.js"), filepath.Join(dir, "nested", "main.mjs")); err != nil {
		t.Fatal(err)
	}
	v, err := m.Validate(Source{Kind: "folder", Path: dir})
	if err != nil || !v.Valid {
		t.Fatalf("现有 mjs 路径不兼容: %+v %v", v, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), make([]byte, maxPluginFile+1), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = m.Validate(Source{Kind: "folder", Path: dir})
	requireCode(t, err, "PACKAGE_TOO_LARGE")
}

func TestValidationIndependentSizeAndCountLimits(t *testing.T) {
	_, m := newTestManager(t, nil)
	makeSized := func(file string, size int64) {
		t.Helper()
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("single package", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "large.cryptoapp.json")
		makeSized(file, MaxPackageBytes+1)
		_, err := m.Validate(Source{Kind: "file", Path: file})
		requireCode(t, err, "PACKAGE_TOO_LARGE")
	})
	t.Run("manifest", func(t *testing.T) {
		p := personalPackage(t, "local.personal.limit", "1.0.0")
		p.Plugin = append(p.Plugin, []byte(strings.Repeat(" ", maxManifestBytes))...)
		_, err := m.Validate(Source{Kind: "inline", Package: &p})
		requireCode(t, err, "PACKAGE_TOO_LARGE")
	})
	t.Run("folder main", func(t *testing.T) {
		dir := t.TempDir()
		writePlugin(t, dir, personalManifest, personalSource)
		makeSized(filepath.Join(dir, "index.js"), maxSourceBytes+1)
		_, err := m.Validate(Source{Kind: "folder", Path: dir})
		requireCode(t, err, "PACKAGE_TOO_LARGE")
	})
	t.Run("count", func(t *testing.T) {
		dir := t.TempDir()
		writePlugin(t, dir, personalManifest, personalSource)
		for n := range maxPluginFiles - 2 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("data-%d.bin", n)), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		v, err := m.Validate(Source{Kind: "folder", Path: dir})
		if err != nil || !v.Valid {
			t.Fatalf("恰好200文件应合法: %+v %v", v, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "one-too-many.bin"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = m.Validate(Source{Kind: "folder", Path: dir})
		requireCode(t, err, "PACKAGE_TOO_LARGE")
	})
	t.Run("total", func(t *testing.T) {
		dir := t.TempDir()
		writePlugin(t, dir, personalManifest, personalSource)
		for n := range 4 {
			makeSized(filepath.Join(dir, fmt.Sprintf("large-%d.bin", n)), maxPluginFile)
		}
		_, err := m.Validate(Source{Kind: "folder", Path: dir})
		requireCode(t, err, "PACKAGE_TOO_LARGE")
	})
}

func TestRecoveryAfterFailedImmediateRollback(t *testing.T) {
	s, m := newTestManager(t, nil)
	first := installTest(t, m, personalPackage(t, "local.personal.recoverfailed", "1.0.0"))
	pending := proposeTest(t, m, personalPackage(t, first.PluginID, "2.0.0"), "update", first.PackageHash)
	m.rename = func(from, to string) error {
		if (filepath.Base(from) == "new" || filepath.Base(from) == "old") && to == filepath.Join(s.dir, first.PluginID) {
			return fmt.Errorf("injected filesystem failure")
		}
		return os.Rename(from, to)
	}
	done, err := m.Decide(pending.RequestID, true)
	if err == nil || done.Status != "failed" || done.Code != "RECOVERY_REQUIRED" {
		t.Fatalf("需恢复状态: %+v %v", done, err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	inst, err := s.Get(first.PluginID)
	if err != nil || inst.Manifest.Version != "1.0.0" {
		t.Fatalf("启动未恢复旧版: %+v %v", inst, err)
	}
}

func TestRecoveryDoesNotResurrectRemovedCommittedInstall(t *testing.T) {
	s, m := newTestManager(t, nil)
	first := installTest(t, m, personalPackage(t, "local.personal.removed", "1.0.0"))
	// 模拟提交成功后事务目录清理失败，保留一份旧版备份和新版记录。
	tx, err := os.MkdirTemp(m.transactionDir, "commit-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tx, "old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyCandidate(filepath.Join(s.dir, first.PluginID), filepath.Join(tx, "old")); err != nil {
		t.Fatal(err)
	}
	_ = saveState(filepath.Join(tx, "old"), state{Enabled: true})
	data, _ := json.Marshal(commitRecord{PluginID: first.PluginID, Operation: "update", PackageHash: first.PackageHash, ExpectedHash: first.PackageHash})
	if err := writeDurable(filepath.Join(tx, "record.json"), data); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(first.PluginID); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, err := s.Get(first.PluginID); err != ErrNotInstalled {
		t.Fatal("启动恢复使卸载后的应用复活")
	}
}
