package agent

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementRoutesKeepTheSameAccessWall(t *testing.T) {
	s, _ := newTestServer(t)
	routes := []struct{ method, path string }{
		{"GET", "/v0/catalog"},
		{"GET", "/v0/examples/com.example.gas"},
		{"POST", "/v0/plugins/validate"},
		{"POST", "/v0/plugins"},
		{"PUT", "/v0/plugins/local.personal.demo"},
		{"GET", "/v0/installations/request"},
		{"DELETE", "/v0/installations/request"},
		{"PUT", "/v0/plugins/local.personal.demo/settings"},
		{"DELETE", "/v0/plugins/local.personal.demo"},
	}
	for _, route := range routes {
		t.Run(route.method+route.path, func(t *testing.T) {
			for _, origin := range []string{"", "https://untrusted.example"} {
				req := withLocalHost(s, httptest.NewRequest(route.method, route.path, nil))
				want := http.StatusUnauthorized
				if origin != "" {
					req.Header.Set("Authorization", "Bearer test-token")
					req.Header.Set("Origin", origin)
					want = http.StatusForbidden
				}
				rec := httptest.NewRecorder()
				s.handler().ServeHTTP(rec, req)
				if rec.Code != want {
					t.Fatalf("status %d, want %d", rec.Code, want)
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("管理接口可被缓存")
				}
			}
		})
	}
}

func TestHTTPHasNoApprovalOrSigningEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{
		"/v0/installations/request/approve", "/v0/approve", "/v0/sign",
		"/v0/plugins/local.personal.demo/sign",
	} {
		rec := do(t, s, http.MethodPost, path, "test-token")
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s 存在批准入口：%d", path, rec.Code)
		}
	}
}

func TestReadOnlyBackendDoesNotPretendToInstall(t *testing.T) {
	s, _ := newTestServer(t)
	req := withLocalHost(s, httptest.NewRequest("POST", "/v0/plugins", strings.NewReader(`{"source":{"kind":"folder","path":"/missing"}}`)))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestRefreshDoesNotEchoUntrustedRuntimeErrors(t *testing.T) {
	s, data := newTestServer(t)
	secret := rand.Text()
	data.errs["local.personal.bad"] = errors.New("上游响应包含 " + secret)
	rec := do(t, s, "POST", "/v0/plugins/local.personal.bad/refresh", "test-token")
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("运行错误被回传或状态错误：%d", rec.Code)
	}
}

func TestCatalogAndTemplatesAreAvailableWithoutPrivateSource(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, "GET", "/v0/catalog", "test-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: %d", rec.Code)
	}
	var catalog map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog["apiVersion"] != "v0" || catalog["catalogVersion"] != float64(1) {
		t.Fatal("目录缺少协议版本")
	}
	for _, id := range []string{"com.example.gas", "com.example.feargreed"} {
		got := do(t, s, "GET", "/v0/examples/"+id, "test-token")
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "export function page") {
			t.Fatalf("官方模板 %s 不可用：%d", id, got.Code)
		}
	}
	if got := do(t, s, "GET", "/v0/examples/local.personal.private", "test-token"); got.Code != http.StatusNotFound {
		t.Fatal("模板接口能读取私人应用")
	}
}
