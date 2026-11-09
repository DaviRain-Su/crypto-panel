package agent

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestForgedInstallRecordCannotAuthorizeAPIMutation(t *testing.T) {
	for _, kind := range []string{"empty", "no-hash", "hardlink", "missing-host", "hash-mismatch"} {
		t.Run(kind, func(t *testing.T) {
			secrets := credentialMemory{}
			_, base := newTestServer(t)
			base.plugins = nil
			store := plugin.NewStore(t.TempDir(), secrets)
			manager, err := plugin.NewManager(store)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			s := New(&managedData{base, manager}, "test-token")
			id := "local.personal." + kind
			if kind == "missing-host" {
				id = "local.personal.missinghost"
			}
			if kind == "hash-mismatch" {
				id = "local.personal.hashmismatch"
			}
			if kind == "no-hash" {
				id = "local.personal.nohash"
			}
			job, err := manager.Propose(localRecordSource(id), "install", "", "", "local-api")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Decide(job.RequestID, true); err != nil {
				t.Fatal(err)
			}
			host := store.ProvenancePath()
			if host == "" || filepath.Dir(host) == store.Dir() {
				t.Fatalf("宿主记录不在应用目录之外: %s", host)
			}
			secret := "secret-" + kind
			if _, err := manager.SetSettings(id, map[string]string{"plain": "original", "token": secret}); err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(store.Dir(), id, "install.json")
			switch kind {
			case "empty":
				if err := os.WriteFile(record, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "no-hash":
				if err := os.WriteFile(record, []byte("{\"installedVia\":\"local-api\"}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				forged := filepath.Join(store.Dir(), "forged.json")
				if err := os.WriteFile(forged, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(forged, record); err != nil {
					t.Fatal(err)
				}
			case "missing-host":
				if err := os.Remove(host); err != nil {
					t.Fatal(err)
				}
			case "hash-mismatch":
				if err := os.WriteFile(filepath.Join(store.Dir(), id, "index.js"), []byte(`export function page(){return {title:"Changed"};}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			changed := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "hacked"}})
			plain, err := store.Setting(id, "plain")
			if err != nil {
				t.Fatal(err)
			}
			if changed.Code != http.StatusForbidden || codeOf(t, changed) != "API_MUTATION_FORBIDDEN" || plain != "original" || !secretStored(secrets, secret) {
				t.Fatalf("%s 设置被接受: %d plain=%q secret=%v body=%s", kind, changed.Code, plain, secretStored(secrets, secret), changed.Body)
			}
			removed := do(t, s, http.MethodDelete, "/v0/plugins/"+id, "test-token")
			if removed.Code != http.StatusForbidden || codeOf(t, removed) != "API_MUTATION_FORBIDDEN" || !secretStored(secrets, secret) {
				t.Fatalf("%s 卸载被接受: %d %s", kind, removed.Code, removed.Body)
			}
			if _, err := os.Stat(filepath.Join(store.Dir(), id, "plugin.json")); err != nil {
				t.Fatal(err)
			}
			if err := manager.Remove(id); err != nil || secretStored(secrets, secret) {
				t.Fatalf("原生卸载失败: %v secret=%v", err, secretStored(secrets, secret))
			}
		})
	}
}

func TestPreUpgradeInstallGainsHostProvenance(t *testing.T) {
	t.Run("regular", func(t *testing.T) {
		secrets := credentialMemory{}
		_, base := newTestServer(t)
		base.plugins = nil
		root := t.TempDir()
		store := plugin.NewStore(filepath.Join(root, "plugins"), secrets)
		id := "local.personal.upgraded"
		writeLooseApp(t, store.Dir(), id, false)
		manager, err := plugin.NewManager(store)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { manager.Close() })
		s := New(&managedData{base, manager}, "test-token")
		if _, err := manager.SetSettings(id, map[string]string{"plain": "original", "token": "kept"}); err != nil {
			t.Fatal(err)
		}
		changed := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "saved"}})
		if changed.Code != http.StatusOK {
			t.Fatalf("升级前的普通安装记录应记为未签名: %d %s", changed.Code, changed.Body)
		}
		if err := os.WriteFile(filepath.Join(store.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		blocked := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "hacked"}})
		plain, err := store.Setting(id, "plain")
		if err != nil || blocked.Code != http.StatusForbidden || codeOf(t, blocked) != "API_MUTATION_FORBIDDEN" || plain != "saved" || !secretStored(secrets, "kept") {
			t.Fatalf("迁移后的伪造记录被接受: %d plain=%q %s", blocked.Code, plain, blocked.Body)
		}
		if err := manager.Remove(id); err != nil || secretStored(secrets, "kept") {
			t.Fatal("原生卸载失败")
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		secrets := credentialMemory{}
		_, base := newTestServer(t)
		base.plugins = nil
		root := t.TempDir()
		store := plugin.NewStore(filepath.Join(root, "plugins"), secrets)
		id := "local.personal.unreadable"
		writeLooseApp(t, store.Dir(), id, true)
		manager, err := plugin.NewManager(store)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { manager.Close() })
		s := New(&managedData{base, manager}, "test-token")
		if _, err := manager.SetSettings(id, map[string]string{"plain": "original", "token": "kept"}); err != nil {
			t.Fatal(err)
		}
		changed := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "saved"}})
		if changed.Code != http.StatusOK {
			t.Fatalf("无法读取的旧 install.json 应视为未签名: %d %s", changed.Code, changed.Body)
		}
		if err := os.WriteFile(filepath.Join(store.Dir(), id, "index.js"), []byte(`export function page(){return {title:"Changed"};}`), 0o644); err != nil {
			t.Fatal(err)
		}
		blocked := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "hacked"}})
		plain, err := store.Setting(id, "plain")
		if err != nil || blocked.Code != http.StatusForbidden || plain != "saved" || !secretStored(secrets, "kept") {
			t.Fatalf("包摘要变化后仍可修改: %d plain=%q %s", blocked.Code, plain, blocked.Body)
		}
		if err := manager.Remove(id); err != nil || secretStored(secrets, "kept") {
			t.Fatal("原生卸载失败")
		}
	})
}

func writeLooseApp(t *testing.T, root, id string, unreadableInstall bool) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"` + id + `","name":"Old","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"settings":[{"key":"plain","label":"Plain"},{"key":"token","label":"Token","secret":true}],"navigation":[{"title":"Overview","page":"overview"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`export function page(){return {title:"Old"};}`), 0o644); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "install.json")
	if unreadableInstall {
		forged := filepath.Join(root, "forged-install.json")
		if err := os.WriteFile(forged, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(forged, record); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(record, []byte("{\"installedVia\":\"folder\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSignedUpdateUsesHostProvenance(t *testing.T) {
	secrets := credentialMemory{}
	_, base := newTestServer(t)
	base.plugins = nil
	store := plugin.NewStore(t.TempDir(), secrets)
	manager, err := plugin.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	s := New(&managedData{base, manager}, "test-token")
	installSignedApp(t, store, "local.personal.signed")
	hash, err := store.PackageHash("local.personal.signed")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"deleted", "empty", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			record := filepath.Join(store.Dir(), "local.personal.signed", "install.json")
			switch kind {
			case "deleted":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
			case "empty":
				if err := os.WriteFile(record, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(record); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				forged := filepath.Join(t.TempDir(), "forged.json")
				if err := os.WriteFile(forged, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(forged, record); err != nil {
					t.Fatal(err)
				}
			}
			body := map[string]any{"source": map[string]any{"kind": "inline", "package": map[string]any{
				"plugin": map[string]any{"id": "local.personal.signed", "name": "Signed", "version": "2.0.0", "main": "index.js", "permissions": map[string]any{"storageKb": 1}, "settings": []any{map[string]any{"key": "plain", "label": "Plain"}, map[string]any{"key": "token", "label": "Token", "secret": true}}, "navigation": []any{map[string]any{"page": "overview", "title": "Overview"}}},
				"main":   `export function page(){return {title:"Next"};}`,
			}}, "expectedHash": hash}
			rec := jsonRequest(t, s, http.MethodPut, "/v0/plugins/local.personal.signed", body)
			if rec.Code != http.StatusUnprocessableEntity || codeOf(t, rec) != "SOURCE_NOT_ALLOWED" {
				t.Fatalf("%s 仍可提议更新: %d %s", kind, rec.Code, rec.Body)
			}
		})
	}
	var listed struct {
		Installations []plugin.Installation `json:"installations"`
	}
	if err := json.Unmarshal(do(t, s, http.MethodGet, "/v0/plugins", "test-token").Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Installations) != 0 {
		t.Fatalf("被拒绝的更新仍进入队列: %+v", listed.Installations)
	}
}
