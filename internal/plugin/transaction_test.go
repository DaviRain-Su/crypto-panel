package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommitSwitchFailureRestoresOldState(t *testing.T) {
	sec := &memSecrets{m: map[string][]byte{}}
	s, m := newTestManager(t, sec)
	first := installTest(t, m, personalPackage(t, "local.personal.rollback", "1.0.0"))
	credential := testCredential(t)
	if _, err := m.SetSettings(first.PluginID, map[string]string{"plain": "old state", "token": credential}); err != nil {
		t.Fatal(err)
	}
	kv, _ := s.KV(first.PluginID)
	_ = kv.Put("saved", json.RawMessage(`42`))
	pending := proposeTest(t, m, personalPackage(t, first.PluginID, "2.0.0"), "update", first.PackageHash)
	m.rename = func(from, to string) error {
		if filepath.Base(from) == "new" && to == filepath.Join(s.dir, first.PluginID) {
			return errors.New("platform details must not escape")
		}
		return os.Rename(from, to)
	}
	done, err := m.Decide(pending.RequestID, true)
	if err == nil || done.Status != "failed" || strings.Contains(err.Error(), "platform details") {
		t.Fatalf("切换失败结果: %+v %v", done, err)
	}
	inst, err := s.Get(first.PluginID)
	if err != nil || inst.Manifest.Version != "1.0.0" {
		t.Fatalf("旧版丢失: %+v %v", inst, err)
	}
	if v, _ := s.Setting(first.PluginID, "plain"); v != "old state" {
		t.Fatal("旧设置丢失")
	}
	if v, _ := s.Setting(first.PluginID, "token"); v != credential {
		t.Fatal("旧凭据丢失")
	}
	if v, ok := kv.Get("saved"); !ok || string(v) != "42" {
		t.Fatal("旧 KV 丢失")
	}
}

func TestInstallingIsObservableAndCannotCancel(t *testing.T) {
	s, m := newTestManager(t, nil)
	pending := proposeTest(t, m, personalPackage(t, "local.personal.committing", "1.0.0"), "install", "")
	entered, proceed := make(chan struct{}), make(chan struct{})
	var proceedOnce sync.Once
	continueCommit := func() { proceedOnce.Do(func() { close(proceed) }) }
	t.Cleanup(continueCommit)
	m.rename = func(from, to string) error {
		if filepath.Base(from) == "new" {
			close(entered)
			<-proceed
		}
		return os.Rename(from, to)
	}
	type result struct {
		record Installation
		err    error
	}
	finished := make(chan result, 1)
	go func() { r, e := m.Decide(pending.RequestID, true); finished <- result{r, e} }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("提交未进入切换")
	}
	query, err := m.GetInstallation(pending.RequestID)
	if err != nil || query.Status != "installing" {
		t.Fatalf("installing 不可见: %+v %v", query, err)
	}
	_, err = m.Cancel(pending.RequestID)
	requireCode(t, err, "INSTALLATION_NOT_PENDING")
	if err := m.Remove(pending.PluginID); err == nil {
		t.Fatal("提交中仍可卸载")
	}
	closed := make(chan struct{})
	go func() { _ = m.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close 未等待提交")
	default:
	}
	continueCommit()
	select {
	case done := <-finished:
		if done.err != nil || done.record.Status != "installed" {
			t.Fatalf("提交结果: %+v", done)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("提交卡住")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close 卡住")
	}
	if _, err := s.Get(pending.PluginID); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryAtEachDirectorySwitchStage(t *testing.T) {
	for _, stage := range []string{"prepared", "old-moved", "new-moved", "new-corrupt", "new-state-corrupt", "new-state-missing", "new-kv-corrupt"} {
		t.Run(stage, func(t *testing.T) {
			sec := &memSecrets{m: map[string][]byte{}}
			s, m := newTestManager(t, sec)
			first := installTest(t, m, personalPackage(t, "local.personal.recovery", "1.0.0"))
			if _, err := s.SetSettings(first.PluginID, map[string]string{"plain": "survives"}); err != nil {
				t.Fatal(err)
			}
			kv, _ := s.KV(first.PluginID)
			_ = kv.Put("saved", json.RawMessage(`"survives"`))
			pkg := personalPackage(t, first.PluginID, "2.0.0")
			candidate, result, err := m.prepareCandidate(Source{Kind: "inline", Package: &pkg})
			if err != nil || !result.Valid {
				t.Fatalf("候选: %v", err)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			tx, err := os.MkdirTemp(m.transactionDir, "commit-")
			if err != nil {
				t.Fatal(err)
			}
			next := filepath.Join(tx, "new")
			old := filepath.Join(tx, "old")
			dest := filepath.Join(s.dir, first.PluginID)
			if err := os.Rename(candidate.dir, next); err != nil {
				t.Fatal(err)
			}
			stateData, _ := os.ReadFile(filepath.Join(dest, "state.json"))
			_ = os.WriteFile(filepath.Join(next, "state.json"), stateData, 0600)
			kvData, _ := os.ReadFile(filepath.Join(dest, "kv.json"))
			_ = os.WriteFile(filepath.Join(next, "kv.json"), kvData, 0600)
			_ = saveInstallInfo(next, installInfo{InstalledVia: "local-api", PackageHash: result.PackageHash})
			data, _ := json.Marshal(commitRecord{PluginID: first.PluginID, Operation: "update", PackageHash: result.PackageHash, ExpectedHash: first.PackageHash})
			if err := writeDurable(filepath.Join(tx, "record.json"), data); err != nil {
				t.Fatal(err)
			}
			if stage != "prepared" {
				if err := os.Rename(dest, old); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(stage, "new-") {
				if err := os.Rename(next, dest); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "new-corrupt" {
				_ = os.WriteFile(filepath.Join(dest, "index.js"), []byte("broken"), 0600)
			}
			if stage == "new-state-corrupt" {
				_ = os.WriteFile(filepath.Join(dest, "state.json"), []byte("broken"), 0600)
			}
			if stage == "new-state-missing" {
				_ = os.Remove(filepath.Join(dest, "state.json"))
			}
			if stage == "new-kv-corrupt" {
				_ = os.WriteFile(filepath.Join(dest, "kv.json"), []byte("broken"), 0600)
			}
			restarted, err := NewManager(s)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			inst, err := s.Get(first.PluginID)
			if err != nil {
				t.Fatal(err)
			}
			want := "1.0.0"
			if stage == "new-moved" {
				want = "2.0.0"
			}
			if inst.Manifest.Version != want {
				t.Fatalf("恢复版本 %s，预期 %s", inst.Manifest.Version, want)
			}
			if v, _ := s.Setting(first.PluginID, "plain"); v != "survives" {
				t.Fatal("恢复丢失设置")
			}
			if v, ok := kv.Get("saved"); !ok || string(v) != `"survives"` {
				t.Fatal("恢复丢失 KV")
			}
			if entries, _ := os.ReadDir(m.transactionDir); len(entries) != 0 {
				t.Fatal("未清理已恢复事务")
			}
		})
	}
}

func TestConcurrentSettingsAndLeaseBounds(t *testing.T) {
	s, m := newTestManager(t, nil)
	p := personalPackage(t, "local.personal.concurrent", "1.0.0")
	p.Plugin = []byte(strings.Replace(string(p.Plugin), `"key":"token","label":"凭据","secret":true`, `"key":"second","label":"第二项"`, 1))
	i := installTest(t, m, p)
	var wg sync.WaitGroup
	for _, key := range []string{"plain", "second"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			for retry := 0; retry < 100; retry++ {
				_, err := s.SetSettings(i.PluginID, map[string]string{key: key})
				if err == nil {
					return
				}
				var busy *Error
				if !errors.As(err, &busy) || busy.Code != "APP_BUSY" {
					t.Error(err)
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Error("设置持续繁忙")
		}(key)
	}
	wg.Wait()
	for _, key := range []string{"plain", "second"} {
		if v, _ := s.Setting(i.PluginID, key); v != key {
			t.Fatalf("丢失字段 %s", key)
		}
	}
	leases := make([]func(), 0, maxActivePluginLeases)
	for n := range maxActivePluginLeases {
		release, err := s.tryLease("local.personal.lease" + string(rune('a'+n/26)) + string(rune('a'+n%26)))
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, release)
	}
	if _, err := s.tryLease("local.personal.extra"); err == nil {
		t.Fatal("lease 表无界增长")
	}
	for _, release := range leases {
		release()
	}
	pluginLeases.Lock()
	remaining := len(pluginLeases.active)
	pluginLeases.Unlock()
	if remaining != 0 {
		t.Fatal("释放后 lease 表未清理")
	}
}
