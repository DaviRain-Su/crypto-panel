package agent

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestSettingsSecretClearWaitsForExplicitVerification(t *testing.T) {
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
	before := jsonRequest(t, s, http.MethodPut, path, map[string]any{"values": map[string]string{"apiKey": rand.Text()}})
	if before.Code != 200 || len(observer.success) != 1 || !observer.success[0] || observer.cleared[0] {
		t.Fatal("普通秘密设置保存没有请求自动验证")
	}
	after := jsonRequest(t, s, http.MethodPut, path, map[string]any{"values": map[string]string{"apiKey": ""}})
	if after.Code != 200 || !strings.Contains(after.Body.String(), `"configured":false`) {
		t.Fatal("秘密设置未按请求清除")
	}
	if len(observer.success) != 2 || !observer.success[1] || !observer.cleared[1] {
		t.Fatal("清除秘密没有通知宿主等待显式验证")
	}
}
