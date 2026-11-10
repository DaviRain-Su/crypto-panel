package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInfoPostsExactRequest(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.RequestURI() != "/info" {
			t.Errorf("request URI = %q, want /info", r.URL.RequestURI())
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"universe":[{"name":"ETH"}]}`)
	}))
	defer srv.Close()

	got, err := NewAt(srv.URL).Info(context.Background(), "meta", map[string]any{
		"coin":  "ETH",
		"count": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request is not JSON: %v", err)
	}
	wantBody := map[string]any{"type": "meta", "coin": "ETH", "count": float64(2)}
	if !reflect.DeepEqual(body, wantBody) {
		t.Fatalf("request body = %#v, want %#v", body, wantBody)
	}
	if string(got) != `{"universe":[{"name":"ETH"}]}` {
		t.Fatalf("response = %s", got)
	}
}

func TestInfoAllowsExactlyDocumentedOperations(t *testing.T) {
	allowed := []string{
		"meta",
		"spotMeta",
		"metaAndAssetCtxs",
		"spotMetaAndAssetCtxs",
		"allMids",
		"l2Book",
		"candleSnapshot",
		"clearinghouseState",
		"spotClearinghouseState",
		"portfolio",
		"openOrders",
		"userFillsByTime",
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 1 {
			t.Errorf("body = %#v, want only type", body)
		}
		_, _ = io.WriteString(w, `null`)
	}))
	defer srv.Close()
	c := NewAt(srv.URL)

	for _, operation := range allowed {
		t.Run(operation, func(t *testing.T) {
			if _, err := c.Info(context.Background(), operation, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, operation := range []string{"", "Meta", "exchange", "order", "userFills", " https://example.com"} {
		t.Run("deny_"+operation, func(t *testing.T) {
			if _, err := c.Info(context.Background(), operation, nil); err == nil {
				t.Fatal("unknown operation was accepted")
			}
		})
	}
	if calls != len(allowed) {
		t.Fatalf("server calls = %d, want %d (only allowed operations)", calls, len(allowed))
	}
}

func TestInfoRejectsTypeOverrideAndInvalidJSONParamsBeforeNetwork(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer srv.Close()
	c := NewAt(srv.URL)

	for name, params := range map[string]map[string]any{
		"same type":         {"type": "meta"},
		"conflicting type":  {"type": "exchange"},
		"nil type":          {"type": nil},
		"unsupported value": {"coin": func() {}},
		"non-finite number": {"coin": math.Inf(1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Info(context.Background(), "meta", params); err == nil {
				t.Fatal("invalid parameters were accepted")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("%d invalid requests reached the server", calls)
	}
}

func TestInfoAcceptsAnyJSONValueAndExactSizeLimit(t *testing.T) {
	values := []string{`{"ok":true}`, `[1,2]`, `"text"`, `42`, `true`, `false`, `null`}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, value)
			}))
			defer srv.Close()
			got, err := NewAt(srv.URL).Info(context.Background(), "allMids", nil)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != value {
				t.Fatalf("response = %q, want %q", got, value)
			}
		})
	}

	exact := `"` + strings.Repeat("x", maxResponseBytes-2) + `"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, exact)
	}))
	defer srv.Close()
	got, err := NewAt(srv.URL).Info(context.Background(), "allMids", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxResponseBytes || string(got) != exact {
		t.Fatalf("response length = %d, want %d", len(got), maxResponseBytes)
	}
}

func TestInfoRejectsHTTPFailuresAndMalformedResponses(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "non-200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"accepted":true}`)
			},
		},
		{
			name: "redirect",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
			},
		},
		{
			name: "malformed JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"unterminated":`)
			},
		},
		{
			name: "multiple JSON values",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{} {}`)
			},
		},
		{
			name:    "empty response",
			handler: func(w http.ResponseWriter, r *http.Request) {},
		},
		{
			name: "oversized response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `"`+strings.Repeat("x", maxResponseBytes-1)+`"`)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			if _, err := NewAt(srv.URL).Info(context.Background(), "meta", nil); err == nil {
				t.Fatal("invalid response was accepted")
			}
		})
	}
}

func TestInfoDoesNotFollowRedirects(t *testing.T) {
	followed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			followed = true
			_, _ = io.WriteString(w, `{}`)
			return
		}
		http.Redirect(w, r, "/followed", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	if _, err := NewAt(srv.URL).Info(context.Background(), "meta", nil); err == nil {
		t.Fatal("redirect was accepted")
	}
	if followed {
		t.Fatal("redirect target was requested")
	}
}

func TestInfoHonorsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewAt(srv.URL).Info(ctx, "meta", nil)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Info did not return after cancellation")
	}
}

func TestNewUsesMainnetAndClientHasBoundedTimeout(t *testing.T) {
	c := New()
	if c.baseURL != "https://api.hyperliquid.xyz" {
		t.Fatalf("base URL = %q", c.baseURL)
	}
	if c.http.Timeout != 25*time.Second {
		t.Fatalf("timeout = %v, want 25s", c.http.Timeout)
	}
	if c.http.CheckRedirect == nil {
		t.Fatal("redirect policy is not configured")
	}
	if err := c.http.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error = %v", err)
	}
}

func TestInfoReturnsRawResponseWithoutNormalizingIt(t *testing.T) {
	response := append(bytes.Repeat([]byte(" "), 3), []byte("{\n  \"answer\": 1e3\n}\n")...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(response)
	}))
	defer srv.Close()

	got, err := NewAt(srv.URL).Info(context.Background(), "meta", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("response = %q, want exact bytes %q", got, response)
	}
}
