package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/intents"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestAssetIconsValidateAndCacheImages(t *testing.T) {
	data := testPNG(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write(data)
	}))
	defer srv.Close()
	icons := newAssetIconCache(srv.Client())
	icons.allowHTTP = srv.URL
	for i := 0; i < 2; i++ {
		bitmap, err := icons.load(context.Background(), srv.URL+"/near.png")
		if err != nil || bitmap == nil {
			t.Fatalf("load = %v, %v", bitmap, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("requests = %d, want one cached request", calls.Load())
	}
}

func TestAssetIconsRejectBadSourcesAndImages(t *testing.T) {
	icons := newAssetIconCache(&http.Client{Timeout: time.Second})
	for _, raw := range []string{"", "javascript:alert(1)", "http://127.0.0.1/private", "https://user:pass@example.com/a.png"} {
		if _, err := icons.load(context.Background(), raw); err == nil {
			t.Errorf("load(%q) succeeded", raw)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{1}, maxAssetIconBytes+1))
	}))
	defer srv.Close()
	icons.allowHTTP = srv.URL
	if _, err := icons.load(context.Background(), srv.URL+"/large.png"); err == nil {
		t.Fatal("an oversized icon was accepted")
	}
}

func TestBuiltinAssetIconsCoverTheDefaultSwap(t *testing.T) {
	for _, token := range []intents.Token{
		{Symbol: "wNEAR", Blockchain: "near"},
		{Symbol: "USDC", Blockchain: "eth"},
		{Symbol: "ETH", Blockchain: "base"},
	} {
		if builtinAssetIcon(token) == nil {
			t.Errorf("no built-in icon for %+v", token)
		}
	}
	if builtinAssetIcon(intents.Token{Symbol: "UNKNOWN"}) != nil {
		t.Fatal("an unknown asset received somebody else's icon")
	}
}

func TestAssetMonogramAlwaysHasAFallback(t *testing.T) {
	if got := assetMonogram(""); got != "?" {
		t.Fatalf("empty monogram = %q", got)
	}
	if got := assetMonogram("wNEAR"); got != "W" {
		t.Fatalf("monogram = %q", got)
	}
}
