package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func proposeTest(t *testing.T, m *Manager, p Package, op, baseline string) Installation {
	t.Helper()
	target := ""
	if op == "update" {
		manifest, err := ParseManifest(p.Plugin)
		if err != nil {
			t.Fatal(err)
		}
		target = manifest.ID
	}
	inst, err := m.Propose(Source{Kind: "inline", Package: &p}, op, target, baseline, "local-api")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}
func installTest(t *testing.T, m *Manager, p Package) Installation {
	t.Helper()
	inst := proposeTest(t, m, p, "install", "")
	done, err := m.Decide(inst.RequestID, true)
	if err != nil || done.Status != "installed" {
		t.Fatalf("安装: %+v %v", done, err)
	}
	return done
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("错误=%v，预期 %s", err, code)
	}
}
func TestManagerPendingFreezeAndDedup(t *testing.T) {
	s, m := newTestManager(t, nil)
	p := personalPackage(t, "local.personal.gas", "1.0.0")
	dir := t.TempDir()
	writePlugin(t, dir, string(p.Plugin), p.Main)
	i, err := m.Propose(Source{Kind: "folder", Path: dir}, "install", "", "", "local-api")
	if err != nil {
		t.Fatal(err)
	}
	if i.Status != "pending" || i.ExpiresAt.Sub(i.CreatedAt) != 30*time.Minute {
		t.Fatalf("任务: %+v", i)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatal("pending 已进入活跃列表")
	}
	duplicate, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api")
	if err != nil || duplicate.RequestID != i.RequestID {
		t.Fatalf("未去重: %+v %v", duplicate, err)
	}
	p.Main += ` // changed`
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api")
	requireCode(t, err, "CANDIDATE_CONFLICT")
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`throw new Error("external change")`), 0600); err != nil {
		t.Fatal(err)
	}
	i.Manifest.Permissions.Chains[0] = "near:mainnet"
	done, err := m.Decide(i.RequestID, true)
	if err != nil || done.Status != "installed" {
		t.Fatalf("确认: %+v %v", done, err)
	}
	data, _ := os.ReadFile(filepath.Join(s.Dir(), i.PluginID, "index.js"))
	if string(data) != personalSource {
		t.Fatal("安装重新读取了外部文件")
	}
	inst, _ := s.Get(i.PluginID)
	if inst.Official || inst.Signed || inst.Manifest.Permissions.Chains[0] != "eip155:8453" {
		t.Fatalf("伪造来源或内部清单被修改: %+v", inst)
	}
	if hash, err := s.PackageHash(i.PluginID); err != nil || hash != i.PackageHash {
		t.Fatalf("安装摘要: %q %v", hash, err)
	}
}
func TestManagerQueueCancelExpireClose(t *testing.T) {
	s, m := newTestManager(t, nil)
	ids := make([]string, 0, 5)
	for n := range 5 {
		p := personalPackage(t, fmt.Sprintf("local.personal.app%d", n), "1.0.0")
		i := proposeTest(t, m, p, "install", "")
		ids = append(ids, i.RequestID)
	}
	p := personalPackage(t, "local.personal.full", "1.0.0")
	_, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api")
	requireCode(t, err, "QUEUE_FULL")
	i, err := m.Cancel(ids[0])
	if err != nil || i.Status != "cancelled" {
		t.Fatalf("取消: %+v %v", i, err)
	}
	if _, err := m.Decide(ids[0], true); err == nil {
		t.Fatal("取消后被确认")
	}
	m.mu.Lock()
	m.tasks[ids[1]].record.ExpiresAt = time.Now().Add(-time.Second)
	m.mu.Unlock()
	i, err = m.GetInstallation(ids[1])
	if err != nil || i.Status != "expired" {
		t.Fatalf("过期: %+v %v", i, err)
	}
	i, err = m.Decide(ids[2], false)
	if err != nil || i.Status != "rejected" {
		t.Fatalf("拒绝: %+v %v", i, err)
	}
	m.CancelVia("local-api")
	if len(m.Installations()) != 0 {
		t.Fatal("未按来源取消")
	}
	if _, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api"); err == nil {
		t.Fatal("关闭的入口仍登记了提议")
	}
	m.ResumeVia("local-api")
	i = proposeTest(t, m, p, "install", "")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.GetInstallation(i.RequestID); err == nil {
		t.Fatal("重启恢复了待确认许可")
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatal("取消任务残留活跃应用")
	}
}
func TestManagerUpdatePreservesAndChecksBaseline(t *testing.T) {
	secrets := &memSecrets{m: map[string][]byte{}}
	s, m := newTestManager(t, secrets)
	first := installTest(t, m, personalPackage(t, "local.personal.gas", "1.0.0"))
	credential := testCredential(t)
	if _, err := m.SetSettings(first.PluginID, map[string]string{"plain": "保留", "token": credential}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(first.PluginID, false); err != nil {
		t.Fatal(err)
	}
	kv, _ := s.KV(first.PluginID)
	if err := kv.Put("data", json.RawMessage(`"keep"`)); err != nil {
		t.Fatal(err)
	}
	p := personalPackage(t, first.PluginID, "1.1.0")
	i := proposeTest(t, m, p, "update", first.PackageHash)
	if i.OldManifest == nil || i.OldManifest.Version != "1.0.0" {
		t.Fatal("缺少旧版权限提示")
	}
	_, err := m.Decide(i.RequestID, true)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := s.Get(first.PluginID)
	if inst.Manifest.Version != "1.1.0" || inst.Enabled {
		t.Fatalf("更新状态: %+v", inst)
	}
	for k, want := range map[string]string{"plain": "保留", "token": credential} {
		v, _ := s.Setting(first.PluginID, k)
		if v != want {
			t.Fatalf("字段丢失: %s", k)
		}
	}
	if v, ok := kv.Get("data"); !ok || string(v) != `"keep"` {
		t.Fatal("KV 未保留")
	}
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "update", first.PluginID, first.PackageHash, "local-api")
	requireCode(t, err, "BASE_CHANGED")
	hash, _ := s.PackageHash(first.PluginID)
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "update", first.PluginID, hash, "local-api")
	requireCode(t, err, "VERSION_CONFLICT")
	p = personalPackage(t, first.PluginID, "2.0.0")
	p.Plugin = []byte(strings.Replace(string(p.Plugin), `"key":"token"`, `"key":"renamed"`, 1))
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "update", first.PluginID, hash, "local-api")
	requireCode(t, err, "SETTINGS_MIGRATION_REQUIRED")
	p = personalPackage(t, first.PluginID, "2.0.0")
	p.Plugin = []byte(strings.Replace(string(p.Plugin), `"storageKb":4`, `"storageKb":0`, 1))
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "update", first.PluginID, hash, "local-api")
	requireCode(t, err, "STORAGE_QUOTA_CONFLICT")
}
func TestManagerCommitRechecksCandidateAndBase(t *testing.T) {
	s, m := newTestManager(t, nil)
	p := personalPackage(t, "local.personal.gas", "1.0.0")
	i := proposeTest(t, m, p, "install", "")
	m.mu.Lock()
	snapshot := m.tasks[i.RequestID].dir
	m.mu.Unlock()
	if err := os.WriteFile(filepath.Join(snapshot, "index.js"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	done, err := m.Decide(i.RequestID, true)
	if err == nil || done.Status != "failed" {
		t.Fatalf("破坏快照仍提交: %+v %v", done, err)
	}
	first := installTest(t, m, p)
	p = personalPackage(t, first.PluginID, "2.0.0")
	i = proposeTest(t, m, p, "update", first.PackageHash)
	if err := os.WriteFile(filepath.Join(s.Dir(), first.PluginID, "index.js"), []byte(personalSource+" // changed"), 0600); err != nil {
		t.Fatal(err)
	}
	done, err = m.Decide(i.RequestID, true)
	if err == nil || done.Status != "failed" || done.Code != "BASE_CHANGED" {
		t.Fatalf("变更基线仍提交: %+v %v", done, err)
	}
	inst, _ := s.Get(first.PluginID)
	if inst.Manifest.Version != "1.0.0" {
		t.Fatal("失败破坏旧版")
	}
}
func TestManagerLeaseAndRemoveInvalidatePending(t *testing.T) {
	s, m := newTestManager(t, nil)
	p := personalPackage(t, "local.personal.gas", "1.0.0")
	i := installTest(t, m, p)
	release, err := m.TryExecution(i.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.TryExecution(i.PluginID)
	requireCode(t, err, "APP_BUSY")
	_, err = m.SetSettings(i.PluginID, map[string]string{"plain": "busy"})
	requireCode(t, err, "APP_BUSY")
	err = m.Remove(i.PluginID)
	requireCode(t, err, "APP_BUSY")
	release()
	release()
	p = personalPackage(t, i.PluginID, "2.0.0")
	pending := proposeTest(t, m, p, "update", i.PackageHash)
	if err := m.Remove(i.PluginID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decide(pending.RequestID, true); err == nil {
		t.Fatal("卸载后旧任务复活")
	}
	if err := m.Remove(i.PluginID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(i.PluginID); err != ErrNotInstalled {
		t.Fatalf("仍安装: %v", err)
	}
}
func TestProposalInValidationRevokedWhenAPICloses(t *testing.T) {
	_, m := newTestManager(t, nil)
	p := personalPackage(t, "local.personal.inflight", "1.0.0")
	beforeProposalCommit = func() { m.CancelVia("local-api") }
	t.Cleanup(func() { beforeProposalCommit = nil })
	_, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api")
	requireCode(t, err, "PROPOSAL_CANCELLED")
	if len(m.Installations()) != 0 {
		t.Fatal("校验中的 API 提议在入口关闭后仍可批准")
	}
	acp, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "acp")
	if err != nil || acp.Status != "pending" || acp.Via != "acp" {
		t.Fatalf("关闭 API 撤销了仍在校验的 ACP 提议: %+v %v", acp, err)
	}
}

func TestRemoveCancelsOnlyThePendingTaskForThatApp(t *testing.T) {
	_, m := newTestManager(t, nil)
	first := installTest(t, m, personalPackage(t, "local.personal.gone", "1.0.0"))
	other := proposeTest(t, m, personalPackage(t, "local.personal.stay", "1.0.0"), "install", "")
	pending := proposeTest(t, m, personalPackage(t, first.PluginID, "2.0.0"), "update", first.PackageHash)
	if err := m.Remove(first.PluginID); err != nil {
		t.Fatal(err)
	}
	gone, err := m.GetInstallation(pending.RequestID)
	if err != nil || gone.Status != "cancelled" {
		t.Fatalf("同 id 的待确认任务未取消: %+v %v", gone, err)
	}
	stayed, err := m.GetInstallation(other.RequestID)
	if err != nil || stayed.Status != "pending" {
		t.Fatalf("卸载取消了其他应用的任务: %+v %v", stayed, err)
	}
}

func TestAPIProposalDoesNotAttachToACPTask(t *testing.T) {
	_, m := newTestManager(t, nil)
	p := personalPackage(t, "local.personal.origin", "1.0.0")
	acp, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api")
	requireCode(t, err, "CANDIDATE_CONFLICT")
	again, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "acp")
	if err != nil || again.RequestID != acp.RequestID {
		t.Fatalf("同一入口未复用任务: %+v %v", again, err)
	}
	_, err = m.CancelFrom(acp.RequestID, "local-api")
	requireCode(t, err, "INSTALLATION_ORIGIN_FORBIDDEN")
	got, err := m.GetInstallation(acp.RequestID)
	if err != nil || got.Status != "pending" || got.Via != "acp" {
		t.Fatalf("ACP 任务被 API 改变: %+v %v", got, err)
	}
	other := personalPackage(t, "local.personal.api-only", "1.0.0")
	api, err := m.Propose(Source{Kind: "inline", Package: &other}, "install", "", "", "local-api")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := m.CancelFrom(api.RequestID, "local-api")
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("API 不能取消自己的任务: %+v %v", cancelled, err)
	}
	if stayed, _ := m.GetInstallation(acp.RequestID); stayed.Status != "pending" {
		t.Fatal("取消 API 任务时连带取消了 ACP")
	}
}

func TestManagerPersonalNamespaceAndSafeJSON(t *testing.T) {
	_, m := newTestManager(t, nil)
	for _, id := range []string{"com.example.gas", "local.one", "org.personal.test", "local.personal.extra.slug"} {
		p := personalPackage(t, id, "1.0.0")
		if _, err := m.Propose(Source{Kind: "inline", Package: &p}, "install", "", "", "local-api"); err == nil {
			t.Fatalf("接受非个人 id %s", id)
		}
	}
	p := personalPackage(t, "local.personal.safe", "1.0.0")
	i := proposeTest(t, m, p, "install", "")
	data, _ := json.Marshal(i)
	for _, unexpected := range []string{personalSource, `"path"`, `"package"`, `"value"`} {
		if strings.Contains(string(data), unexpected) {
			t.Fatal("任务包含源码、路径或设置值")
		}
	}
}
