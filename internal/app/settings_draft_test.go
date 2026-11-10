package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

var errSetRefused = errors.New("set refused for the test")

// 保存失败（busy、校验）不得清空用户输入的 secret：草稿是唯一副本。
func TestSavePluginSettingsKeepsDraftsWhenNothingWasWritten(t *testing.T) {
	a := newTestApp(t)
	job := proposeVerification(t, a, verificationSource(t, "local.personal.drafts", `export function page(){return {sections:[]};}`,
		plugin.SettingField{Key: "apiKey", Label: "API key", Secret: true},
		plugin.SettingField{Key: "endpoint", Label: "Endpoint"}), "acp")
	a.reviewInstallation(job.RequestID)

	// 占住租约，让 SetSettings 以 APP_BUSY 零写入失败。
	release, err := a.pluginManager.TryExecution(job.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	*a.pluginDrafts[job.PluginID+"/apiKey"] = "typed-secret"
	*a.pluginDrafts[job.PluginID+"/endpoint"] = "https://example.com"
	a.savePluginSettings(job.Manifest)
	release()
	if got := *a.pluginDrafts[job.PluginID+"/apiKey"]; got != "typed-secret" {
		t.Fatalf("零写入失败清空了 secret 草稿：%q", got)
	}
	if got := *a.pluginDrafts[job.PluginID+"/endpoint"]; got != "https://example.com" {
		t.Fatalf("零写入失败清空了普通草稿：%q", got)
	}

	// 无租约时保存成功，secret 草稿清空（已应用），普通草稿保留。
	a.savePluginSettings(job.Manifest)
	if got := *a.pluginDrafts[job.PluginID+"/apiKey"]; got != "" {
		t.Fatalf("保存成功后 secret 草稿未清空：%q", got)
	}
}

// 部分失败按事务报告区分：applied 的清空，failed 的保留。
func TestSavePluginSettingsKeepsFailedKeyDrafts(t *testing.T) {
	a := newTestApp(t)
	job := proposeVerification(t, a, verificationSource(t, "local.personal.partial", `export function page(){return {sections:[]};}`,
		plugin.SettingField{Key: "alpha", Label: "Alpha", Secret: true},
		plugin.SettingField{Key: "beta", Label: "Beta", Secret: true}), "acp")
	a.reviewInstallation(job.RequestID)

	// 只让 beta 的钥匙串写入失败，alpha 正常写入。
	failing := &failSetStore{inner: memStore{}, failKey: "/beta"}
	a.plugins = plugin.NewStore(a.plugins.Dir(), failing)
	var err error
	a.pluginManager, err = plugin.NewManager(a.plugins)
	if err != nil {
		t.Fatal(err)
	}
	*a.pluginDrafts[job.PluginID+"/alpha"] = "good"
	*a.pluginDrafts[job.PluginID+"/beta"] = "keep-me"
	a.savePluginSettings(job.Manifest)
	if got := *a.pluginDrafts[job.PluginID+"/alpha"]; got != "" {
		t.Fatalf("applied 的 secret 草稿未清空：%q", got)
	}
	if got := *a.pluginDrafts[job.PluginID+"/beta"]; got != "keep-me" {
		t.Fatalf("failed 的 secret 草稿被清空，无法重试：%q", got)
	}
}

// failSetStore 拒绝包含 failKey 的 Set，其余走内存表。
type failSetStore struct {
	inner   memStore
	failKey string
}

func (f *failSetStore) Set(key string, value []byte) error {
	if f.failKey != "" && strings.Contains(key, f.failKey) {
		return errSetRefused
	}
	return f.inner.Set(key, value)
}
func (f *failSetStore) Get(key string) ([]byte, error) { return f.inner.Get(key) }
func (f *failSetStore) Delete(key string) error        { return f.inner.Delete(key) }
