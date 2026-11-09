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
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"values":false}`))
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != 400 || len(observer.success) != 2 {
		t.Fatal("无效请求触发了验证通知")
	}
}
