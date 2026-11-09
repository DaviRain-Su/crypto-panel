package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
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
		req := httptest.NewRequest("POST", "/v0/plugins", strings.NewReader(body))
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
		req := httptest.NewRequest("POST", "/v0/plugins/validate", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.contentType)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("status %d, want %d", rec.Code, tc.status)
		}
	}
}
