//go:build unix

package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestFIFOInstallRecordDoesNotBlockAPI(t *testing.T) {
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
	id := "local.personal.fifo"
	job, err := manager.Propose(localRecordSource(id), "install", "", "", "local-api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	secret := "fifo-secret"
	if _, err := manager.SetSettings(id, map[string]string{"plain": "original", "token": secret}); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(store.Dir(), id, "install.json")
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(record, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, "/v0/plugins/" + id + "/settings", map[string]any{"values": map[string]string{"plain": "hacked"}}},
		{http.MethodDelete, "/v0/plugins/" + id, nil},
	} {
		done := make(chan *httptest.ResponseRecorder, 1)
		go func(method, path string, body any) {
			var reader *bytes.Reader
			if body == nil {
				reader = bytes.NewReader(nil)
			} else {
				data, _ := json.Marshal(body)
				reader = bytes.NewReader(data)
			}
			req := withLocalHost(s, httptest.NewRequest(method, path, reader))
			req.Header.Set("Authorization", "Bearer test-token")
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			s.handler().ServeHTTP(rec, req)
			done <- rec
		}(call.method, call.path, call.body)
		select {
		case rec := <-done:
			if rec.Code != http.StatusForbidden || codeOf(t, rec) != "API_MUTATION_FORBIDDEN" {
				t.Fatalf("%s %s = %d %s", call.method, call.path, rec.Code, rec.Body)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s %s blocked on a FIFO install.json", call.method, call.path)
		}
	}
	plain, err := store.Setting(id, "plain")
	if err != nil || plain != "original" || !secretStored(secrets, secret) {
		t.Fatalf("FIFO 请求改动了应用 plain=%q secret=%v err=%v", plain, secretStored(secrets, secret), err)
	}
}
