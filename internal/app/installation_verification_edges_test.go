package app

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/plugin"
)

// 队列夹具保留真实 Manager / runtime，仅延后后台工作或结果的主线程投递。
func queueApprovedVerification(t *testing.T, a *App, source plugin.Source) plugin.Installation {
	t.Helper()
	job := proposeVerification(t, a, source, "acp")
	installed, err := a.pluginManager.Decide(job.RequestID, true)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.beginInstallationVerification(installed)
	return installed
}

func TestInstallationVerificationOptionalSettingsAndRetry(t *testing.T) {
	a := newTestApp(t)
	job := proposeVerification(t, a, verificationSource(t, "local.personal.optional", `
export function page(){
  if (!host.kv.get("retry")) {host.kv.put("retry",true);throw new Error("try again");}
  return {sections:[]};
}`, plugin.SettingField{Key: "label", Label: "Optional label"}), "acp")
	a.reviewInstallation(job.RequestID)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Verify first page"); err != nil {
		t.Fatal(err)
	}
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "failed" {
		t.Fatalf("首次失败没有显示：%+v", got)
	}
	a.verifyInstalledApp(job.PluginID)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("重试没有使用新 runtime 或保留 KV：%+v", got)
	}
}

func TestInstallationVerificationCoalescesAndUsesExecutionLease(t *testing.T) {
	a := newTestApp(t)
	var work []func()
	a.goAsync = func(fn func()) { work = append(work, fn) }
	job := queueApprovedVerification(t, a, verificationSource(t, "local.personal.lease", `
export function page(){host.kv.put("checked",true);return {sections:[]};}`))
	a.verifyInstalledApp(job.PluginID)
	a.verifyInstalledApp(job.PluginID)
	if len(work) != 1 {
		t.Fatalf("重复请求未合并：%d", len(work))
	}
	release, err := a.pluginManager.TryExecution(job.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	work[0]()
	release()
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "failed" || got.Code != "APP_BUSY" {
		t.Fatalf("绕过应用执行 lease：%+v", got)
	}
	kv, _ := a.plugins.KV(job.PluginID)
	if checked, _ := kv.Get("checked"); checked != nil {
		t.Fatal("正忙的应用被执行")
	}
	a.verifyInstalledApp(job.PluginID)
	work[1]()
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("释放 lease 后不能重试：%+v", got)
	}
}

func TestInstallationVerificationHasBoundedCapacity(t *testing.T) {
	a := newTestApp(t)
	var work []func()
	a.goAsync = func(fn func()) { work = append(work, fn) }
	var last plugin.Installation
	for i := 0; i < 6; i++ {
		last = queueApprovedVerification(t, a, verificationSource(t, fmt.Sprintf("local.personal.capacity-%d", i), `export function page(){return {sections:[]};}`))
	}
	if len(work) != 5 || a.runningInstallationVerifications != 5 {
		t.Fatalf("并发验证无界：%d / %d", len(work), a.runningInstallationVerifications)
	}
	if got := installedVerification(t, a, last.PluginID); got == nil || got.Code != "APP_BUSY" || got.Status != "failed" {
		t.Fatalf("容量满未提供安全重试状态：%+v", got)
	}
	for _, fn := range work {
		fn()
	}
	if a.runningInstallationVerifications != 0 {
		t.Fatal("验证结束没有归还容量")
	}
	a.verifyInstalledApp(last.PluginID)
	work[5]()
	if got := installedVerification(t, a, last.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("归还容量后仍不能验证：%+v", got)
	}
}

func TestInstallationVerificationQueuedWorkCannotRunAfterRemovalOrUpdate(t *testing.T) {
	for _, operation := range []string{"remove", "update", "host", "quit"} {
		t.Run(operation, func(t *testing.T) {
			a := newTestApp(t)
			var work []func()
			a.goAsync = func(fn func()) { work = append(work, fn) }
			old := queueApprovedVerification(t, a, verificationSource(t, "local.personal.queued", `export function page(){host.kv.put("old",true);return {sections:[]};}`))
			var update plugin.Installation
			switch operation {
			case "remove":
				if err := a.pluginManager.Remove(old.PluginID); err != nil {
					t.Fatal(err)
				}
				a.ApplicationsChanged(old.PluginID)
			case "update":
				source := personalSource(t, old.PluginID, "1.0.1")
				var m plugin.Manifest
				_ = json.Unmarshal(source.Package.Plugin, &m)
				m.Permissions.StorageKb = 4
				source.Package.Plugin, _ = json.Marshal(m)
				source.Package.Main = `export function page(){host.kv.put("new",true);return {sections:[]};}`
				proposed, err := a.pluginManager.Propose(source, "update", old.PluginID, old.PackageHash, "acp")
				if err != nil {
					t.Fatal(err)
				}
				update, err = a.pluginManager.Decide(proposed.RequestID, true)
				if err != nil {
					t.Fatal(err)
				}
				a.refreshPlugins()
				a.beginInstallationVerification(update)
			case "host":
				a.stopAllRunners()
			case "quit":
				a.quitting = true
				a.stopAllRunners()
			}
			work[0]()
			if operation == "remove" {
				if installedVerification(t, a, old.PluginID) != nil {
					t.Fatal("卸载后仍保留验证")
				}
				if _, err := a.plugins.Get(old.PluginID); !errors.Is(err, plugin.ErrNotInstalled) {
					t.Fatal("验证使卸载的应用复活")
				}
				return
			}
			kv, _ := a.plugins.KV(old.PluginID)
			if ran, _ := kv.Get("old"); ran != nil {
				t.Fatal("已失效的后台工作仍执行旧代码")
			}
			if operation == "update" {
				if ran, _ := kv.Get("new"); ran != nil {
					t.Fatal("旧任务提前执行了新包")
				}
				work[1]()
				if got := installedVerification(t, a, old.PluginID); got == nil || got.Status != "passed" || got.RequestID != update.RequestID || got.PackageHash != update.PackageHash {
					t.Fatalf("新版本验证被旧工作覆盖：%+v", got)
				}
			}
			if a.runningInstallationVerifications != 0 {
				t.Fatal("失效工作泄露并发槽位")
			}
		})
	}
}

func TestInstallationVerificationLateResultsAreDiscarded(t *testing.T) {
	for _, change := range []string{"settings", "remove", "update", "host", "new-generation"} {
		t.Run(change, func(t *testing.T) {
			a := newTestApp(t)
			var work []func()
			a.goAsync = func(fn func()) { work = append(work, fn) }
			job := queueApprovedVerification(t, a, verificationSource(t, "local.personal.late", `export function page(){return {sections:[]};}`))
			a.coding.installationID = job.RequestID
			var completion func()
			calls := 0
			a.runOnMain = func(fn func()) {
				calls++
				if calls == 2 {
					completion = fn
					return
				}
				fn()
			}
			work[0]()
			a.runOnMain = func(fn func()) { fn() }
			if completion == nil {
				t.Fatal("未截获运行完成的主线程通知")
			}
			switch change {
			case "settings":
				a.ApplicationSettingsChanged(job.PluginID, false, false)
			case "remove":
				if err := a.pluginManager.Remove(job.PluginID); err != nil {
					t.Fatal(err)
				}
				// 故意不发变更通知，完成回调自身也必须发现卸载。
			case "update":
				proposed, err := a.pluginManager.Propose(personalSource(t, job.PluginID, "1.0.1"), "update", job.PluginID, job.PackageHash, "acp")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := a.pluginManager.Decide(proposed.RequestID, true); err != nil {
					t.Fatal(err)
				}
				// 同样不发通知，旧包的成功结果不能属于新包。
			case "host":
				a.stopAllRunners()
			case "new-generation":
				a.coding.installationID = "another-request"
				a.coding.notice = "new generation"
			}
			completion()
			got := installedVerification(t, a, job.PluginID)
			if change == "new-generation" {
				if a.coding.notice != "new generation" {
					t.Fatal("旧检查覆盖了新生成任务的通知")
				}
			} else if got != nil && got.Status == "passed" {
				t.Fatalf("迟到结果覆盖变更后的状态：%+v", got)
			}
			if a.runningInstallationVerifications != 0 {
				t.Fatal("迟到结果没有释放槽位")
			}
		})
	}
}

type verificationFailingSecrets struct {
	memStore
	fail bool
}

func (s *verificationFailingSecrets) Set(key string, value []byte) error {
	if s.fail {
		return errors.New("credential write failed")
	}
	return s.memStore.Set(key, value)
}

func TestInstallationVerificationPartialSettingsFailureCannotPass(t *testing.T) {
	a := newTestApp(t)
	_ = a.pluginManager.Close()
	secrets := &verificationFailingSecrets{memStore: memStore{}}
	a.plugins = plugin.NewStore(a.plugins.Dir(), secrets)
	var err error
	a.pluginManager, err = plugin.NewManager(a.plugins)
	if err != nil {
		t.Fatal(err)
	}
	job := proposeVerification(t, a, verificationSource(t, "local.personal.partial", `export function page(){host.kv.put("runs",(host.kv.get("runs")||0)+1);return {sections:[]};}`, plugin.SettingField{Key: "apiKey", Label: "API key", Secret: true}, plugin.SettingField{Key: "label", Label: "Label"}), "acp")
	a.reviewInstallation(job.RequestID)
	*a.pluginDrafts[job.PluginID+"/apiKey"] = rand.Text()
	a.savePluginSettings(job.Manifest)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("初始配置没有通过：%+v", got)
	}
	secrets.fail = true
	*a.pluginDrafts[job.PluginID+"/apiKey"] = rand.Text()
	*a.pluginDrafts[job.PluginID+"/label"] = "changed"
	a.savePluginSettings(job.Manifest)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "failed" || got.Code != "SETTINGS_WRITE_FAILED" || got.CheckedAt != nil {
		t.Fatalf("部分保存失败仍误报验证通过：%+v", got)
	}
	kv, _ := a.plugins.KV(job.PluginID)
	if runs, _ := kv.Get("runs"); string(runs) != "1" {
		t.Fatal("部分保存失败仍启动了验证")
	}
}

func TestInstallationVerificationDoesNotEnableDisabledUpdate(t *testing.T) {
	a := newTestApp(t)
	job := queueApprovedVerification(t, a, verificationSource(t, "local.personal.disabled", `export function page(){host.kv.put("runs",true);return {sections:[]};}`))
	if err := a.pluginManager.SetEnabled(job.PluginID, false); err != nil {
		t.Fatal(err)
	}
	source := personalSource(t, job.PluginID, "1.0.1")
	var manifest plugin.Manifest
	_ = json.Unmarshal(source.Package.Plugin, &manifest)
	manifest.Permissions.StorageKb = 4
	source.Package.Plugin, _ = json.Marshal(manifest)
	update, err := a.pluginManager.Propose(source, "update", job.PluginID, job.PackageHash, "acp")
	if err != nil {
		t.Fatal(err)
	}
	a.reviewInstallation(update.RequestID)
	got := installedVerification(t, a, job.PluginID)
	if got == nil || got.Status != "failed" || got.Code != "APP_DISABLED" {
		t.Fatalf("禁用更新仍被验证或误报：%+v", got)
	}
	inst, _ := a.plugins.Get(job.PluginID)
	if inst.Enabled {
		t.Fatal("验证擅自启用了应用")
	}
}
