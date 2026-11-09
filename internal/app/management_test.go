package app

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/plugin"
)

func personalSource(t *testing.T, id, version string) plugin.Source {
	t.Helper()
	manifest, err := json.Marshal(plugin.Manifest{
		ID: id, Name: "Personal", Version: version, Main: "index.js",
		Navigation: []plugin.NavItem{{Title: "Overview", Page: "overview"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: `export function page() { return { title: "Personal", sections: [] }; }`}}
}

func TestManagedInstallWaitsForNativeReview(t *testing.T) {
	a := newTestApp(t)
	called := 0
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { called++; done(true) }
	job, err := a.ApplicationManager().Propose(personalSource(t, "local.personal.review", "1.0.0"), "install", "", "", "local-api")
	if err != nil {
		t.Fatal(err)
	}
	a.ApplicationsChanged(job.PluginID)
	if called != 0 {
		t.Fatal("HTTP 提议触发了自动批准")
	}
	if _, err := a.plugins.Get(job.PluginID); !errors.Is(err, plugin.ErrNotInstalled) {
		t.Fatal("pending 应用已经安装")
	}
	a.reviewInstallation(job.RequestID)
	if called != 1 {
		t.Fatal("审阅没有经过原生确认")
	}
	installed, err := a.plugins.Get(job.PluginID)
	if err != nil || installed.Official || installed.Signed {
		t.Fatalf("个人应用身份错误：%v", err)
	}
	got, err := a.RefreshPage(job.PluginID, "overview")
	if err != nil || got.PackageHash != job.PackageHash || got.EvaluatedAt.IsZero() {
		t.Fatalf("安装后无法获取页面元数据：%v", err)
	}
}

func TestNativeReviewLeavesCreatorForInstalledSettings(t *testing.T) {
	a := newTestApp(t)
	a.openCreator("")
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(true) }
	job, err := a.ApplicationManager().Propose(personalSource(t, "local.personal.creator", "1.0.0"), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	a.reviewInstallation(job.RequestID)
	if a.coding.open || a.pluginDetail != job.PluginID || a.route != routePlugins {
		t.Fatal("安装成功后创建表单遮挡了应用设置")
	}
}

func TestManagedInstallRejectionDoesNotRunOrInstall(t *testing.T) {
	a := newTestApp(t)
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(false) }
	job, err := a.ApplicationManager().Propose(personalSource(t, "local.personal.reject", "1.0.0"), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	a.reviewInstallation(job.RequestID)
	got, err := a.ApplicationManager().GetInstallation(job.RequestID)
	if err != nil || got.Status != "rejected" {
		t.Fatalf("拒绝状态：%s %v", got.Status, err)
	}
	if _, err := a.Refresh(job.PluginID); !errors.Is(err, agent.ErrNotFound) {
		t.Fatal("拒绝后仍可执行")
	}
}

func TestStopAPICancelsOnlyItsOwnPendingJobs(t *testing.T) {
	a := newTestApp(t)
	api, err := a.ApplicationManager().Propose(personalSource(t, "local.personal.api", "1.0.0"), "install", "", "", "local-api")
	if err != nil {
		t.Fatal(err)
	}
	acp, err := a.ApplicationManager().Propose(personalSource(t, "local.personal.acp", "1.0.0"), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	a.stopAgent()
	got, _ := a.ApplicationManager().GetInstallation(api.RequestID)
	other, _ := a.ApplicationManager().GetInstallation(acp.RequestID)
	if got.Status != "cancelled" || other.Status != "pending" {
		t.Fatalf("撤销范围错误：%s %s", got.Status, other.Status)
	}
}

func TestManagedRefreshRejectsUndeclaredPageAndBusy(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "app")
	writeAgentPlugin(t, dir, "local.personal.busy", `export function page() { return { sections: [] }; }`)
	m, err := a.plugins.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	if _, err := a.RefreshPage(m.ID, "not-declared"); err == nil {
		t.Fatal("执行了未声明页面")
	}
	release, err := a.ApplicationManager().TryExecution(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = a.RefreshPage(m.ID, "overview")
	var busy *plugin.Error
	if !errors.As(err, &busy) || busy.Code != "APP_BUSY" || busy.Status != 409 {
		t.Fatalf("重复执行未被拒绝：%v", err)
	}
}
