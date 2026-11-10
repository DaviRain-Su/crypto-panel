package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func pngDeclaringSize(width, height uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 6
	var buf bytes.Buffer
	buf.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	writePNGChunk(&buf, "IHDR", ihdr)
	writePNGChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writePNGChunk(buf *bytes.Buffer, kind string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	buf.Write(n[:])
	buf.WriteString(kind)
	buf.Write(data)
	sum := crc32.ChecksumIEEE(append([]byte(kind), data...))
	binary.BigEndian.PutUint32(n[:], sum)
	buf.Write(n[:])
}

func TestAssetIconsRejectHugeDimensions(t *testing.T) {
	header := pngDeclaringSize(8000, 8000)
	if len(header) > 1024 {
		t.Fatalf("header is %d bytes; the test must not allocate the decoded image", len(header))
	}
	wide := image.NewNRGBA(image.Rect(0, 0, 600, 600))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, wide); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "image/png")
		if strings.HasSuffix(r.URL.Path, "wide.png") {
			w.Write(encoded.Bytes())
			return
		}
		w.Write(header)
	}))
	defer srv.Close()
	icons := newAssetIconCache(srv.Client())
	icons.allowHTTP = srv.URL
	for _, path := range []string{"/bomb.png", "/wide.png"} {
		_, err := icons.load(context.Background(), srv.URL+path)
		if err == nil || !strings.Contains(err.Error(), "dimensions are too large") {
			t.Fatalf("load(%s) = %v, want the dimension cap", path, err)
		}
		if _, ok := icons.cached(srv.URL + path); ok {
			t.Fatalf("rejected icon %s was cached", path)
		}
	}
	if _, err := icons.load(context.Background(), srv.URL+"/bomb.png"); err == nil {
		t.Fatal("the huge icon was accepted on retry")
	}
	if calls.Load() != 3 {
		t.Fatalf("requests = %d, want the failure not to be cached", calls.Load())
	}
}

func TestAssetIconsCoalesceInFlightLoads(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	release := make(chan struct{})
	pngData := testPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		w.Write(pngData)
	}))
	defer srv.Close()
	releaseOnce := sync.Once{}
	defer releaseOnce.Do(func() { close(release) })
	icons := newAssetIconCache(srv.Client())
	icons.allowHTTP = srv.URL
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := icons.load(context.Background(), srv.URL+"/hung.png")
			errs <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no icon request started")
	}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("in-flight requests = %d, want 1", got)
	}
	releaseOnce.Do(func() { close(release) })
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAssetIconsRejectPrivateHostsAndCrossHostRedirects(t *testing.T) {
	icons := newAssetIconCache(&http.Client{Timeout: time.Second})
	for _, raw := range []string{
		"https://127.0.0.1/icon.png", "https://10.1.2.3/a.png", "https://192.168.0.5/a.png",
		"https://localhost./icon.png", "https://foo.localhost/icon.png", "https://127.1/icon.png",
		"https://2130706433/icon.png", "https://0x7f000001/icon.png", "https://100.64.0.1/icon.png",
		"https://[fe80::1%25eth0]/icon.png",
	} {
		_, err := icons.load(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), "public HTTPS") && !strings.Contains(err.Error(), "invalid") {
			t.Errorf("load(%q) = %v, want a public-HTTPS refusal", raw, err)
		}
	}
	if err := icons.validateURL("https://example.com/icon.png"); err != nil {
		t.Fatalf("public icon URL rejected: %v", err)
	}
	if err := icons.validateURL("https://8.8.8.8/icon.png"); err != nil {
		t.Fatalf("public address rejected: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/icon.png", http.StatusFound)
	}))
	defer srv.Close()
	icons = newAssetIconCache(srv.Client())
	icons.allowHTTP = srv.URL
	_, err := icons.load(context.Background(), srv.URL+"/icon.png")
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("cross-host redirect = %v", err)
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
