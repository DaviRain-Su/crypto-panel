package app

import (
	"os"
	"path/filepath"
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
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("locked for the local API") {
		t.Fatalf("没有锁定提示: %q", tt.Texts())
	}
	a.askProvenanceConfirm(id, "Locked")
	if err := a.pluginManager.APIMayMutate(id); err == nil {
		t.Fatal("取消确认后本机 API 被放行")
	}
	a.confirmProvenance = func(m plugin.Manifest, done func(bool)) {
		if m.ID != id {
			t.Fatalf("确认了别的应用: %s", m.ID)
		}
		done(true)
	}
	a.askProvenanceConfirm(id, "Locked")
	if err := a.pluginManager.APIMayMutate(id); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Confirmed Locked") {
		tt = ui.NewTester(a.view, windowWidth, windowHeight)
		if !tt.HasText("Confirmed Locked") {
			t.Fatalf("确认后没有提示: %q", tt.Texts())
		}
	}
}
