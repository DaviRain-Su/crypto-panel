package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

type settingsObserver struct {
	*managedData
	ids     []string
	success []bool
	cleared []bool
}

func (d *settingsObserver) ApplicationSettingsChanged(id string, success, clearedSecret bool) {
	d.ids = append(d.ids, id)
	d.success = append(d.success, success)
	d.cleared = append(d.cleared, clearedSecret)
}

func TestSettingsNotifiesVerificationOnlyAfterValidatedWrite(t *testing.T) {
	s, manager, _ := managementServer(t)
	observer := &settingsObserver{managedData: s.data.(*managedData)}
	s.data = observer
	proposed := jsonRequest(t, s, http.MethodPost, "/v0/plugins", httpPackage(t, "1.0.0"))
	var job plugin.Installation
	if err := json.Unmarshal(proposed.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	path := "/v0/plugins/" + job.PluginID + "/settings"
	ok := jsonRequest(t, s, http.MethodPut, path, map[string]any{"values": map[string]string{}})
	if ok.Code != 200 || len(observer.success) != 1 || !observer.success[0] || observer.ids[0] != job.PluginID {
		t.Fatalf("成功写入没有通知验证：%d %+v", ok.Code, observer.success)
	}
	bad := jsonRequest(t, s, http.MethodPut, path, map[string]any{"values": map[string]string{"undeclared": "value"}})
	if bad.Code != 422 || len(observer.success) != 2 || observer.success[1] {
		t.Fatalf("失败写入被报告为可自动验证：%d %+v", bad.Code, observer.success)
	}
	req := withLocalHost(s, httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"values":false}`)))
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != 400 || len(observer.success) != 2 {
		t.Fatal("无效请求触发了验证通知")
	}
}

func TestProtectedSettingsDoNotNotifyVerification(t *testing.T) {
	_, base := newTestServer(t)
	base.plugins = nil
	store := plugin.NewStore(t.TempDir(), credentialMemory{})
	manager, err := plugin.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	observer := &settingsObserver{managedData: &managedData{base, manager}}
	s := New(observer, "test-token")
	for _, id := range []string{"com.example.gas", "com.example.vault"} {
		rec := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+id+"/settings", map[string]any{"values": map[string]string{"plain": "hacked"}})
		if rec.Code != http.StatusForbidden || codeOf(t, rec) != "API_MUTATION_FORBIDDEN" {
			t.Fatalf("%s 设置：%d %s", id, rec.Code, rec.Body)
		}
	}
	if len(observer.ids) != 0 {
		t.Fatalf("受保护应用触发了安装验证：%v", observer.ids)
	}
	if _, err := store.Get("com.example.gas"); err != nil {
		t.Fatal(err)
	}
}
