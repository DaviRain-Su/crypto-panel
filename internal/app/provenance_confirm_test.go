package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/i18n"
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
	if strings.Contains(a.pluginNotice, "Checking this app") {
		t.Fatal("取消后仍显示正在检查")
	}
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("取消确认写下了来源记录")
	}
	if err := a.pluginManager.APIMayMutate(id); err == nil {
		t.Fatal("取消确认后本机 API 被放行")
	}
	a.confirmProvenance = nil
	a.installOpen = true
	a.askProvenanceConfirm(id, "Locked")
	a.installOpen = false
	if strings.Contains(a.pluginNotice, "Checking this app") {
		t.Fatal("第二个确认窗被拒绝后仍显示正在检查")
	}
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("被拒绝的第二个确认窗写下了记录")
	}
	keep := a.plugins
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) {
		a.plugins = plugin.NewStore(t.TempDir(), memStore{})
		done(true)
	}
	a.askProvenanceConfirm(id, "Locked")
	a.plugins = keep
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("应用目录已经换掉后仍确认了原来的应用")
	}
	if strings.Contains(a.pluginNotice, "Checking this app") {
		t.Fatal("换掉应用目录后仍显示正在检查")
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

func TestProvenanceWindowCloseDoesNotConfirm(t *testing.T) {
	confirmed := false
	provenanceConfirmClosed(func(ok bool) {
		if ok {
			confirmed = true
		}
	})(nil)
	if confirmed {
		t.Fatal("关闭窗口被当成了确认")
	}
}

func TestConfirmRejectsSnapshotForADifferentID(t *testing.T) {
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
	prev := takeProvenanceSnapshot
	takeProvenanceSnapshot = func(plugins *plugin.Store, want string) (plugin.ProvenanceView, error) {
		view, err := plugins.SnapshotProvenance(want)
		if err != nil {
			return view, err
		}
		view.ID = "local.personal.other"
		return view, nil
	}
	t.Cleanup(func() { takeProvenanceSnapshot = prev })
	asked := false
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) {
		asked = true
		done(true)
	}
	a.askProvenanceConfirm(id, "Locked")
	if asked {
		t.Fatal("id 对不上仍打开了确认")
	}
	if _, ok := sec[plugin.ProvenanceAccount(id)]; ok {
		t.Fatal("id 对不上仍写下了记录")
	}
	if strings.Contains(a.pluginNotice, "Confirmed") || strings.Contains(a.pluginNotice, "Checking") {
		t.Fatalf("id 对不上的结果: %q", a.pluginNotice)
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

func TestConfirmKeychainStaysOffTheUIThread(t *testing.T) {
	onUI := false
	sec := &countingSecrets{inner: memStore{}, onUI: &onUI}
	a := &App{
		settings:      DefaultSettings(),
		settingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		systemLocale:  i18n.English,
		pluginSecrets: sec,
	}
	a.newReader()
	a.runOnMain = func(fn func()) {
		onUI = true
		fn()
		onUI = false
	}
	a.goAsync = func(fn func()) {
		prev := onUI
		onUI = false
		fn()
		onUI = prev
	}
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) { done(false) }
	a.initPlugins(t.TempDir())
	delete(sec.inner, "provenance:migration")
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
	sec.uiGets = 0
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) { done(true) }
	a.askProvenanceConfirm(id, "Locked")
	if sec.uiGets != 0 {
		t.Fatalf("确认在界面线程读取了凭据服务 %d 次", sec.uiGets)
	}
	if err := a.pluginManager.APIMayMutate(id); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmFailureWhenAPIStaysClosed(t *testing.T) {
	a := newTestApp(t)
	sec := a.pluginSecrets.(memStore)
	delete(sec, "provenance:migration")
	id := "local.personal.locked"
	appDir := filepath.Join(a.plugins.Dir(), id)
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"local.personal.locked","name":"Locked","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"navigation":[{"title":"Overview","page":"overview"}]}`)
	for name, body := range map[string][]byte{
		"plugin.json": manifest,
		"index.js":    []byte(`export function page(){return {title:"Locked"};}`),
	} {
		if err := os.WriteFile(filepath.Join(appDir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(appDir, "install.json"), []byte("{\"installedVia\":\"folder\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(t.TempDir(), "plugins", id)
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "index.js"), []byte(`export function page(){return {title:"Locked"};}`), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := plugin.DigestDir(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	signed := fmt.Sprintf("{\n  \"installedVia\": \"store\",\n  \"hash\": %q,\n  \"publisher\": %q,\n  \"signature\": %q\n}\n", digest, plugin.PublicKeyString(pub), plugin.SignDigest(priv, digest))
	if err := os.WriteFile(filepath.Join(otherDir, "install.json"), []byte(signed), 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := plugin.NewManager(plugin.NewStore(filepath.Dir(otherDir), memStore{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	a.pluginManager = other
	a.refreshPlugins()
	a.refreshProvenanceLocks()
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) { done(true) }
	a.askProvenanceConfirm(id, "Locked")
	if strings.Contains(a.pluginNotice, "Confirmed") {
		t.Fatal("本机 API 仍然拒绝时显示了已确认")
	}
	if !strings.Contains(a.pluginNotice, "Could not confirm") {
		t.Fatalf("没有失败提示: %q", a.pluginNotice)
	}
}

type countingSecrets struct {
	inner  memStore
	gets   int
	uiGets int
	onUI   *bool
}

func (c *countingSecrets) Get(key string) ([]byte, error) {
	c.gets++
	if c.onUI != nil && *c.onUI {
		c.uiGets++
	}
	return c.inner.Get(key)
}

func (c *countingSecrets) Set(key string, value []byte) error { return c.inner.Set(key, value) }
func (c *countingSecrets) Delete(key string) error            { return c.inner.Delete(key) }
