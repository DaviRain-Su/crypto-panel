package agent

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/plugin"
)

type managedData struct {
	*fakeData
	manager *plugin.Manager
}

func (d *managedData) ApplicationManager() *plugin.Manager { return d.manager }
func (d *managedData) ApplicationsChanged(string)          {}
func (d *managedData) RefreshPage(id, page string) (PageResult, error) {
	p, err := d.Refresh(id)
	return PageResult{Page: p}, err
}

type credentialMemory map[string][]byte

func (s credentialMemory) Get(k string) ([]byte, error) { return bytes.Clone(s[k]), nil }
func (s credentialMemory) Set(k string, v []byte) error { s[k] = bytes.Clone(v); return nil }
func (s credentialMemory) Delete(k string) error        { delete(s, k); return nil }

func managementServer(t *testing.T) (*Server, *plugin.Manager, *plugin.Store) {
	t.Helper()
	_, base := newTestServer(t)
	base.plugins = nil
	store := plugin.NewStore(t.TempDir(), credentialMemory{})
	manager, err := plugin.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return New(&managedData{base, manager}, "test-token"), manager, store
}
func jsonRequest(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := withLocalHost(s, httptest.NewRequest(method, path, bytes.NewReader(data)))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}
func httpPackage(t *testing.T, version string) map[string]any {
	t.Helper()
	return map[string]any{"source": map[string]any{"kind": "inline", "package": map[string]any{
		"plugin": map[string]any{"id": "local.personal.http", "name": "HTTP app", "version": version, "main": "index.js", "permissions": map[string]any{"storageKb": 1}, "settings": []any{map[string]any{"key": "apiKey", "label": "API key", "secret": true}}, "navigation": []any{map[string]any{"page": "overview", "title": "Overview"}}},
		"main":   `export function page() { return { title: "HTTP", sections: [] }; }`,
	}}}
}
func TestHTTPLifecycleRequiresNativeDecision(t *testing.T) {
	s, manager, store := managementServer(t)
	pkg := httpPackage(t, "1.0.0")
	valid := jsonRequest(t, s, "POST", "/v0/plugins/validate", pkg)
	if valid.Code != 200 {
		t.Fatalf("validate: %d %s", valid.Code, valid.Body)
	}
	proposed := jsonRequest(t, s, "POST", "/v0/plugins", pkg)
	if proposed.Code != 202 {
		t.Fatalf("propose: %d %s", proposed.Code, proposed.Body)
	}
	var job plugin.Installation
	if err := json.Unmarshal(proposed.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Status != "pending" || proposed.Header().Get("Location") != "/v0/installations/"+job.RequestID {
		t.Fatal("提议缺少可查询任务")
	}
	if _, err := store.Get(job.PluginID); err == nil {
		t.Fatal("token 已被当成安装许可")
	}
	list := do(t, s, "GET", "/v0/plugins", "test-token")
	var listing struct {
		Plugins       []Plugin              `json:"plugins"`
		Installations []plugin.Installation `json:"installations"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Plugins) != 0 || len(listing.Installations) != 1 {
		t.Fatal("pending 混入已安装列表")
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	status := do(t, s, "GET", proposed.Header().Get("Location"), "test-token")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"status":"installed"`) {
		t.Fatal("无法查询安装结果")
	}
	secret := rand.Text()
	settings := jsonRequest(t, s, "PUT", "/v0/plugins/"+job.PluginID+"/settings", map[string]any{"values": map[string]string{"apiKey": secret}})
	if settings.Code != 200 || strings.Contains(settings.Body.String(), secret) || strings.Contains(settings.Body.String(), `"value"`) {
		t.Fatal("设置响应泄露秘密或写入失败")
	}
	hash, err := store.PackageHash(job.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	update := httpPackage(t, "1.1.0")
	update["expectedHash"] = hash
	updated := jsonRequest(t, s, "PUT", "/v0/plugins/"+job.PluginID, update)
	if updated.Code != 202 {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	var next plugin.Installation
	if err := json.Unmarshal(updated.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(next.RequestID, true); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Setting(job.PluginID, "apiKey"); err != nil || value != secret {
		t.Fatal("更新丢失凭据")
	}
	if removed := do(t, s, "DELETE", "/v0/plugins/"+job.PluginID, "test-token"); removed.Code != http.StatusNoContent {
		t.Fatalf("remove: %d", removed.Code)
	}
}
func TestManagementRejectsMalformedBodiesBeforeProposing(t *testing.T) {
	s, manager, _ := managementServer(t)
	for _, body := range []string{`null`, `{}`, `{"source":{},"approve":true}`, `{"source":{},"source":{}}`, `{"source":{}} {}`, `{"source":{"kind":"inline","package":{"plugin":{},"main":""},"path":"/tmp"}}`} {
		req := withLocalHost(s, httptest.NewRequest("POST", "/v0/plugins", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code < 400 || len(manager.Installations()) != 0 {
			t.Fatalf("坏正文产生任务：%d", rec.Code)
		}
	}
}
func TestManagementBodyAndContentTypeLimits(t *testing.T) {
	s, _, _ := managementServer(t)
	for _, tc := range []struct {
		contentType, body string
		status            int
	}{
		{"text/plain", `{}`, 415},
		{"application/json", strings.Repeat(" ", maxApplicationRequest+1), 413},
	} {
		req := withLocalHost(s, httptest.NewRequest("POST", "/v0/plugins/validate", strings.NewReader(tc.body)))
		req.Header.Set("Content-Type", tc.contentType)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("status %d, want %d", rec.Code, tc.status)
		}
	}
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body["code"]
}

func secretStored(secrets credentialMemory, secret string) bool {
	for _, value := range secrets {
		if string(value) == secret {
			return true
		}
	}
	return false
}

func installSignedApp(t *testing.T, store *plugin.Store, id string) {
	t.Helper()
	dir := t.TempDir()
	manifest := fmt.Sprintf(`{"id":%q,"name":"Signed","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"settings":[{"key":"plain","label":"Plain"},{"key":"token","label":"Token","secret":true}],"navigation":[{"title":"Overview","page":"overview"}]}`, id)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`export function page(){return {title:"Signed"};}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := plugin.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plugin.DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entry := plugin.RegistryEntry{
		ID: m.ID, Name: m.Name, Version: m.Version, URL: "https://example.com/signed.zip",
		Hash: digest, Signature: plugin.SignDigest(priv, digest),
		Publisher: plugin.PublicKeyString(priv.Public().(ed25519.PublicKey)),
	}
	archive, err := plugin.ZipDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := store.Prepare(archive, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ready.Install(); err != nil {
		t.Fatal(err)
	}
}

func TestAPICannotUninstallOrRetuneProtectedApps(t *testing.T) {
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
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	gasPath := filepath.Join(store.Dir(), "com.example.gas", "index.js")
	gasBefore, err := os.ReadFile(gasPath)
	if err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	vaultManifest := `{"id":"com.example.vault","name":"Vault","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"settings":[{"key":"plain","label":"Plain"},{"key":"token","label":"Token","secret":true}],"navigation":[{"title":"Overview","page":"overview"}]}`
	if err := os.WriteFile(filepath.Join(vaultDir, "plugin.json"), []byte(vaultManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "index.js"), []byte(`export function page(){return {title:"Vault"};}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install(vaultDir); err != nil {
		t.Fatal(err)
	}
	vaultSecret := "vault-credential"
	if _, err := manager.SetSettings("com.example.vault", map[string]string{"plain": "original", "token": vaultSecret}); err != nil {
		t.Fatal(err)
	}
	installSignedApp(t, store, "local.signed.app")
	signedSecret := "signed-credential"
	if _, err := manager.SetSettings("local.signed.app", map[string]string{"token": signedSecret}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"com.example.gas", "com.example.vault", "local.signed.app"} {
		removed := do(t, s, "DELETE", "/v0/plugins/"+id, "test-token")
		if removed.Code != http.StatusForbidden || codeOf(t, removed) != "API_MUTATION_FORBIDDEN" {
			t.Fatalf("%s 卸载: %d %s", id, removed.Code, removed.Body)
		}
		changed := jsonRequest(t, s, "PUT", "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "hacked", "token": "hacked"}})
		if changed.Code != http.StatusForbidden || codeOf(t, changed) != "API_MUTATION_FORBIDDEN" {
			t.Fatalf("%s 设置: %d %s", id, changed.Code, changed.Body)
		}
	}
	gasAfter, err := os.ReadFile(gasPath)
	if err != nil || string(gasAfter) != string(gasBefore) {
		t.Fatal("官方应用被 API 删除或改写")
	}
	if plain, err := store.Setting("com.example.vault", "plain"); err != nil || plain != "original" || !secretStored(secrets, vaultSecret) || secretStored(secrets, "hacked") {
		t.Fatalf("非个人应用的设置被 API 修改: %q %v", plain, err)
	}
	if !secretStored(secrets, signedSecret) {
		t.Fatal("已签名应用的凭据被 API 删除")
	}
	signed, err := store.Get("local.signed.app")
	if err != nil || !signed.Signed {
		t.Fatal("已签名应用不再是已签名安装")
	}
	if _, err := manager.SetSettings("com.example.vault", map[string]string{"plain": "from-ui"}); err != nil {
		t.Fatal(err)
	}
	if plain, _ := store.Setting("com.example.vault", "plain"); plain != "from-ui" {
		t.Fatal("原生设置路径被 API 限制挡住")
	}
	if err := manager.Remove("com.example.gas"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("com.example.gas"); err != plugin.ErrNotInstalled {
		t.Fatal("原生卸载不能删除官方应用")
	}

	proposed := jsonRequest(t, s, "POST", "/v0/plugins", httpPackage(t, "1.0.0"))
	if proposed.Code != http.StatusAccepted {
		t.Fatalf("local.* 提议: %d %s", proposed.Code, proposed.Body)
	}
	var job plugin.Installation
	if err := json.Unmarshal(proposed.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	localSecret := "local-credential"
	settings := jsonRequest(t, s, "PUT", "/v0/plugins/"+job.PluginID+"/settings", map[string]any{"values": map[string]string{"apiKey": localSecret}})
	if settings.Code != http.StatusOK || strings.Contains(settings.Body.String(), localSecret) {
		t.Fatalf("local.* 设置: %d %s", settings.Code, settings.Body)
	}
	if value, err := store.Setting(job.PluginID, "apiKey"); err != nil || value != localSecret {
		t.Fatal("local.* 设置没有写入")
	}
	if removed := do(t, s, "DELETE", "/v0/plugins/"+job.PluginID, "test-token"); removed.Code != http.StatusNoContent {
		t.Fatalf("local.* 卸载: %d %s", removed.Code, removed.Body)
	}
	if _, err := store.Get(job.PluginID); err != plugin.ErrNotInstalled || secretStored(secrets, localSecret) {
		t.Fatal("local.* 卸载没有删除应用或凭据")
	}
}

func TestAPICannotCancelOrReuseACPInstallation(t *testing.T) {
	s, manager, _ := managementServer(t)
	manifest := []byte(`{"id":"local.personal.acp-only","name":"ACP","version":"1.0.0","main":"index.js","navigation":[{"title":"Overview","page":"overview"}]}`)
	main := `export function page(){return {title:"ACP"};}`
	source := plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: main}}
	acp, err := manager.Propose(source, "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	removed := do(t, s, "DELETE", "/v0/installations/"+acp.RequestID, "test-token")
	if removed.Code != http.StatusForbidden || codeOf(t, removed) != "INSTALLATION_ORIGIN_FORBIDDEN" {
		t.Fatalf("取消 ACP: %d %s", removed.Code, removed.Body)
	}
	if got, err := manager.GetInstallation(acp.RequestID); err != nil || got.Status != "pending" || got.Via != "acp" {
		t.Fatalf("ACP 任务被 API 改变: %+v %v", got, err)
	}
	conflict := jsonRequest(t, s, "POST", "/v0/plugins", map[string]any{"source": map[string]any{"kind": "inline", "package": map[string]any{"plugin": json.RawMessage(manifest), "main": main}}})
	if conflict.Code != http.StatusConflict || codeOf(t, conflict) != "CANDIDATE_CONFLICT" || strings.Contains(conflict.Body.String(), acp.RequestID) {
		t.Fatalf("API 提议附着到了 ACP 任务: %d %s", conflict.Code, conflict.Body)
	}
	apiBody := httpPackage(t, "1.0.0")
	accepted := jsonRequest(t, s, "POST", "/v0/plugins", apiBody)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("API 自己的提议: %d %s", accepted.Code, accepted.Body)
	}
	var api plugin.Installation
	if err := json.Unmarshal(accepted.Body.Bytes(), &api); err != nil {
		t.Fatal(err)
	}
	cancelled := do(t, s, "DELETE", "/v0/installations/"+api.RequestID, "test-token")
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("取消 API 任务: %d %s", cancelled.Code, cancelled.Body)
	}
	if got, _ := manager.GetInstallation(acp.RequestID); got.Status != "pending" {
		t.Fatal("取消 API 任务时取消了 ACP")
	}
}

type headerManaged struct{ *fakeData }

func (headerManaged) ApplicationManager() *plugin.Manager { return nil }
func (headerManaged) ApplicationsChanged(string)          {}
func (headerManaged) RefreshPage(string, string) (PageResult, error) {
	return PageResult{Page: map[string]any{"title": "Gas"}, PackageHash: "sha256:" + strings.Repeat("ab", 32), EvaluatedAt: time.Date(2026, 11, 9, 1, 2, 3, 4, time.UTC)}, nil
}

func TestRefreshSendsLegacyAndLintelHeaders(t *testing.T) {
	_, base := newTestServer(t)
	s := New(headerManaged{base}, "test-token")
	rec := do(t, s, "POST", "/v0/plugins/com.example.gas/refresh", "test-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	hash := "sha256:" + strings.Repeat("ab", 32)
	when := time.Date(2026, 11, 9, 1, 2, 3, 4, time.UTC).Format(time.RFC3339Nano)
	for _, pair := range [][2]string{
		{"X-Crypto-Panel-Package-Hash", hash},
		{"X-Lintel-Package-Hash", hash},
		{"X-Crypto-Panel-Evaluated-At", when},
		{"X-Lintel-Evaluated-At", when},
	} {
		if rec.Header().Get(pair[0]) != pair[1] {
			t.Fatalf("%s = %q", pair[0], rec.Header().Get(pair[0]))
		}
	}
}
