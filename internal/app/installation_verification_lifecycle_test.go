package app

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestInstallationVerificationNoPagesDoesNotRun(t *testing.T) {
	a := newTestApp(t)
	source := verificationSource(t, "local.personal.no-pages", `host.kv.put("ran",true);export function page(){return {sections:[]};}`)
	var manifest plugin.Manifest
	if err := json.Unmarshal(source.Package.Plugin, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Navigation = nil
	source.Package.Plugin, _ = json.Marshal(manifest)
	job := queueApprovedVerification(t, a, source)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "failed" || got.Code != "NO_PAGES" {
		t.Fatalf("无首页应用被错误执行：%+v", got)
	}
	kv, _ := a.plugins.KV(job.PluginID)
	if ran, _ := kv.Get("ran"); ran != nil {
		t.Fatal("没有声明页面仍执行了模块顶层")
	}
}

func TestInstallationVerificationSettingsNotificationRunsSavedConfiguration(t *testing.T) {
	a := newTestApp(t)
	job := queueApprovedVerification(t, a, verificationSource(t, "local.personal.http-settings", `export function page(){if(host.setting("label")!=="saved")throw new Error("not saved");return {sections:[]};}`, plugin.SettingField{Key: "label", Label: "Label"}))
	if _, err := a.pluginManager.SetSettings(job.PluginID, map[string]string{"label": "saved"}); err != nil {
		t.Fatal(err)
	}
	a.ApplicationSettingsChanged(job.PluginID, true, false)
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatalf("HTTP 保存通知没有验证最新配置：%+v", got)
	}
}

func TestInstallationVerificationDoesNotPersistOrRunAtRestart(t *testing.T) {
	a := newTestApp(t)
	job := queueApprovedVerification(t, a, verificationSource(t, "local.personal.restart", `export function page(){host.kv.put("runs",(host.kv.get("runs")||0)+1);return {sections:[]};}`))
	if got := installedVerification(t, a, job.PluginID); got == nil || got.Status != "passed" {
		t.Fatal("初次验证失败")
	}
	if err := a.pluginManager.Close(); err != nil {
		t.Fatal(err)
	}
	b := &App{settings: DefaultSettings(), pluginSecrets: memStore{}, runOnMain: func(fn func()) { fn() }, goAsync: func(fn func()) { fn() }}
	b.initPlugins(filepath.Dir(a.plugins.Dir()))
	t.Cleanup(func() { _ = b.pluginManager.Close() })
	if got := installedVerification(t, b, job.PluginID); got != nil {
		t.Fatal("重启恢复了旧验证状态")
	}
	kv, err := b.plugins.KV(job.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if runs, _ := kv.Get("runs"); string(runs) != "1" {
		t.Fatal("重启自动执行了已安装应用")
	}
}
