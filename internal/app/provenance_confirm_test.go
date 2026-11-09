package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestProvenanceLockNoticeAndConfirm(t *testing.T) {
	a := newTestApp(t)
	sec := a.pluginSecrets.(memStore)
	delete(sec, "provenance:migration")
	id := "local.personal.locked"
	appDir := filepath.Join(a.plugins.Dir(), id)
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"local.personal.locked","name":"Locked","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"navigation":[{"title":"Overview","page":"overview"}]}`)
	if err := os.WriteFile(filepath.Join(appDir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "index.js"), []byte(`export function page(){return {title:"Locked"};}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "install.json"), []byte("{\"installedVia\":\"folder\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.refreshProvenanceLocks()
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("locked for the local API") || !tt.HasText("origin can't be confirmed") {
		t.Fatalf("没有来源无法确认的锁定提示: %q", tt.Texts())
	}
	if tt.HasText("Signing: unsigned") || tt.HasText("未签名") {
		t.Fatalf("列表把 install.json 说成了未签名: %q", tt.Texts())
	}
	sawBusy := false
	a.goAsync = func(fn func()) {
		if strings.Contains(a.pluginNotice, "Checking this app") {
			sawBusy = true
		}
		fn()
	}
	a.askProvenanceConfirm(id, "Locked")
	if !sawBusy {
		t.Fatal("确认没有先显示忙碌状态")
	}
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("取消确认写下了来源记录")
	}
	if err := a.pluginManager.APIMayMutate(id); err == nil {
		t.Fatal("取消确认后本机 API 被放行")
	}
	a.confirmProvenance = nil
	a.askProvenanceConfirm(id, "Locked")
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("没有窗口时的取消回调确认了应用")
	}
	a.confirmProvenance = func(view plugin.ProvenanceView, done func(bool)) {
		if view.ID != id || view.Path == "" || view.PackageHash == "" || view.Signed {
			t.Fatalf("确认窗信息不完整: %+v", view)
		}
		done(false)
	}
	a.askProvenanceConfirm(id, "Locked")
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("取消回调确认了应用")
	}
	a.confirmProvenance = func(view plugin.ProvenanceView, done func(bool)) {
		if view.ID != id || view.Path == "" || view.PackageHash == "" || view.Signed {
			t.Fatalf("确认窗信息不完整: %+v", view)
		}
		done(true)
	}
	a.askProvenanceConfirm(id, "Locked")
	if err := a.pluginManager.APIMayMutate(id); err != nil {
		t.Fatal(err)
	}
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Confirmed Locked") {
		t.Fatalf("确认后没有提示: %q", tt.Texts())
	}
	if tt.HasText("Confirm Locked") {
		t.Fatal("确认成功后锁定按钮还在")
	}
}

func TestAppsPageDoesNotReadProvenanceOnRender(t *testing.T) {
	sec := &countingSecrets{inner: memStore{}}
	a := &App{
		settings:      DefaultSettings(),
		settingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		pluginSecrets: sec,
	}
	a.newReader()
	a.initPlugins(t.TempDir())
	a.runOnMain = func(fn func()) { fn() }
	a.goAsync = func(fn func()) { fn() }
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) { done(false) }
	before := sec.gets
	a.route = routePlugins
	ui.NewTester(a.view, windowWidth, windowHeight)
	if sec.gets != before {
		t.Fatalf("绘制 Apps 页读取了凭据服务: %d -> %d", before, sec.gets)
	}
}

type countingSecrets struct {
	inner memStore
	gets  int
}

func (c *countingSecrets) Get(key string) ([]byte, error) {
	c.gets++
	return c.inner.Get(key)
}

func (c *countingSecrets) Set(key string, value []byte) error { return c.inner.Set(key, value) }
func (c *countingSecrets) Delete(key string) error            { return c.inner.Delete(key) }
