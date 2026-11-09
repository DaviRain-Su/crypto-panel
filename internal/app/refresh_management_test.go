package app

import (
	"path/filepath"
	"testing"
)

func TestPageRefreshFailureKeepsSameVersionSuccess(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "app")
	writeAgentPlugin(t, dir, "local.personal.refresh", `let calls=0; export function page(){ if (++calls>1) throw new Error("failed"); return {title:"Good",sections:[{text:"cached content"}]}; }`)
	m, err := a.plugins.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.openPluginPage(m.ID, "overview")
	first := a.pluginView
	if !first.hasPage || first.err != "" || first.updated.IsZero() {
		t.Fatalf("首次刷新失败：%+v", first)
	}
	a.loadPluginPage()
	second := a.pluginView
	if second.err == "" || !second.hasPage || second.page.Title != "Good" || second.updated != first.updated {
		t.Fatal("刷新失败丢失上次成功页面")
	}
	a.stopAllRunners()
}

func TestUIRefreshCoalescesAndSharesExecutionLease(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "app")
	writeAgentPlugin(t, dir, "local.personal.coalesce", `export function page(){return {sections:[]};}`)
	m, err := a.plugins.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	var work []func()
	a.goAsync = func(fn func()) { work = append(work, fn) }
	a.openPluginPage(m.ID, "overview")
	a.loadPluginPage()
	if len(work) != 1 {
		t.Fatal("重复刷新建立了多个执行")
	}
	if _, err := a.RefreshPage(m.ID, "overview"); err == nil {
		t.Fatal("API绕过UI执行锁")
	}
	work[0]()
	if a.pluginView.loading || !a.pluginView.hasPage {
		t.Fatal("页面执行没有完成")
	}
	a.stopAllRunners()
	for _, fn := range work[1:] {
		fn()
	}
}

// 即使安装变更通知尚未送达，刷新也不能继续执行旧包的缓存 runtime。
func TestRefreshRebuildsRunnerAfterPackageUpdate(t *testing.T) {
	a := newTestApp(t)
	manager := a.ApplicationManager()
	id := "local.personal.rebuild"
	job, err := manager.Propose(personalSource(t, id, "1.0.0"), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.openPluginPage(id, "overview")
	first := a.pluginView
	if !first.hasPage || first.page.Title != "Personal" {
		t.Fatal("初版没有成功执行")
	}
	source := personalSource(t, id, "1.0.1")
	source.Package.Main = `export function page(){return {title:"Updated",sections:[]};}`
	update, err := manager.Propose(source, "update", id, first.hash, "local-api")
	if err != nil {
		t.Fatal(err)
	}
	// 仅提议或取消待确认更新时，当前页继续显示已安装版本。
	a.ApplicationsChanged("")
	if a.pluginView.updated != first.updated || !a.pluginView.hasPage {
		t.Fatal("待确认更新清空了当前成功页面")
	}
	if _, err := manager.Decide(update.RequestID, true); err != nil {
		t.Fatal(err)
	}
	a.loadPluginPage()
	if a.pluginView.err != "" || a.pluginView.page.Title != "Updated" || a.pluginView.hash == first.hash {
		t.Fatal("包更新后仍执行旧缓存")
	}
	a.stopAllRunners()
}
