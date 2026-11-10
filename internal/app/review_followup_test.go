package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestNativeUninstallDropsSecretDraft(t *testing.T) {
	a := newTestApp(t)
	id := "local.personal.draftleak"
	dir := filepath.Join(t.TempDir(), "src")
	writePluginWithSecret(t, dir, id)
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	draft := a.pluginDrafts[id+"/apiKey"]
	if draft == nil {
		t.Fatal("refresh did not create a secret draft")
	}
	*draft = "stale-secret"
	previous := confirmPluginRemoval
	confirmPluginRemoval = func(string, string, string, string) bool { return true }
	t.Cleanup(func() { confirmPluginRemoval = previous })
	a.removePlugin(id)
	if _, ok := a.pluginDrafts[id+"/apiKey"]; ok {
		t.Fatal("native uninstall kept the secret draft")
	}
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.savePluginSettings(plugin.Manifest{
		ID: id, Settings: []plugin.SettingField{{Key: "apiKey", Label: "API key", Secret: true}},
	})
	if v, err := a.plugins.Setting(id, "apiKey"); err != nil || v != "" {
		t.Fatalf("reinstall saved the old secret %q (%v)", v, err)
	}
	*a.pluginDrafts[id+"/apiKey"] = "broken-secret"
	inst, err := a.plugins.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	a.removeBrokenPlugin(inst.Dir)
	if _, ok := a.pluginDrafts[id+"/apiKey"]; ok {
		t.Fatal("broken-folder uninstall kept the secret draft")
	}
}

func writePluginWithSecret(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
	  "id": "` + id + `",
	  "name": "Draft",
	  "version": "1.0.0",
	  "main": "index.js",
	  "permissions": {"storageKb": 1},
	  "settings": [{"key": "apiKey", "label": "API key", "secret": true}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`export function page(){return {sections:[]};}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsStatusCache(t *testing.T) {
	a := newTestApp(t)
	id := "local.personal.cache"
	dir := filepath.Join(t.TempDir(), "src")
	writePluginWithSecret(t, dir, id)
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	if _, ok := a.pluginSettingStatus[id]; ok {
		t.Fatal("refresh filled a settings cache that was never opened")
	}
	configured := false
	a.pluginSettingStatus["local.personal.gone"] = []plugin.SettingStatus{{
		SettingField: plugin.SettingField{Key: "apiKey", Secret: true},
		Configured:   &configured,
	}}
	a.refreshPlugins()
	if _, ok := a.pluginSettingStatus["local.personal.gone"]; ok {
		t.Fatal("refresh kept settings status for an app that is gone")
	}
	a.openPluginDetail(id)
	status, ok := a.pluginSettingStatus[id]
	if !ok || len(status) != 1 || status[0].Configured == nil || *status[0].Configured {
		t.Fatalf("opening settings did not cache an unconfigured secret: %+v", status)
	}
	if _, err := a.plugins.SetSettings(id, map[string]string{"apiKey": "saved"}); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	status = a.pluginSettingStatus[id]
	if len(status) != 1 || status[0].Configured == nil || !*status[0].Configured {
		t.Fatalf("refresh did not reload settings status: %+v", status)
	}
	release, err := a.pluginManager.TryExecution(id)
	if err != nil {
		t.Fatal(err)
	}
	a.loadPluginSettingStatus(id)
	release()
	kept := a.pluginSettingStatus[id]
	if len(kept) != 1 || kept[0].Configured == nil || !*kept[0].Configured {
		t.Fatalf("a busy read replaced the cached settings status: %+v", kept)
	}
}
