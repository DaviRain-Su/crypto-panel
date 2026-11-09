package app

import (
	"crypto/rand"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestInstallationVerificationSecretClearDoesNotAutomaticallyExecute(t *testing.T) {
	a := newTestApp(t)
	_ = a.pluginManager.Close()
	a.plugins = plugin.NewStore(a.plugins.Dir(), memStore{})
	var err error
	a.pluginManager, err = plugin.NewManager(a.plugins)
	if err != nil {
		t.Fatal(err)
	}
	job := queueApprovedVerification(t, a, verificationSource(t, "local.personal.clear", `export function page(){host.kv.put("runs",(host.kv.get("runs")||0)+1);return {sections:[]};}`, plugin.SettingField{Key: "apiKey", Label: "API key", Secret: true}))
	if _, err := a.pluginManager.SetSettings(job.PluginID, map[string]string{"apiKey": rand.Text()}); err != nil {
		t.Fatal(err)
	}
	a.ApplicationSettingsChanged(job.PluginID, true, false)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("完整配置没有通过：%+v", got)
	}
	if _, err := a.pluginManager.SetSettings(job.PluginID, map[string]string{"apiKey": ""}); err != nil {
		t.Fatal(err)
	}
	a.ApplicationSettingsChanged(job.PluginID, true, true)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "waiting_settings" || got.CheckedAt != nil {
		t.Fatalf("清除秘密仍自动运行并报告通过：%+v", got)
	}
	kv, _ := a.plugins.KV(job.PluginID)
	if runs, _ := kv.Get("runs"); string(runs) != "1" {
		t.Fatal("清除秘密自动执行了首页")
	}
	a.verifyInstalledApp(job.PluginID)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("清除秘密后无法显式验证可选设置：%+v", got)
	}
	if runs, _ := kv.Get("runs"); string(runs) != "2" {
		t.Fatal("显式验证没有执行首页")
	}
}
