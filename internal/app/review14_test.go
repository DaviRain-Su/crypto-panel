package app

import (
	"archive/zip"
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

func TestInstallRefusesFilesSwappedAfterApproval(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "src")
	writeReviewPlugin(t, dir, "com.example.swap")
	previous := pickInstallPath
	pickInstallPath = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if !opts.Directory {
			t.Fatal("folder install opened a file dialog")
		}
		return []string{dir}, nil
	}
	t.Cleanup(func() { pickInstallPath = previous })
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) {
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("SWAPPED AFTER APPROVAL"), 0o644); err != nil {
			t.Fatal(err)
		}
		done(true)
	}
	a.installPlugin()
	if a.pluginNotice != a.t(i18n.StoreChanged) {
		t.Fatalf("notice = %q", a.pluginNotice)
	}
	if _, err := a.plugins.Get("com.example.swap"); err == nil {
		t.Fatal("a swapped folder was installed")
	}
}

func TestZipInstallRefusesFilesSwappedAfterApproval(t *testing.T) {
	a := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "app.zip")
	if err := os.WriteFile(archive, zipPlugin(t, "com.example.zipswap", `export function page() { return {}; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := pickInstallPath
	pickInstallPath = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if opts.Directory {
			t.Fatal("zip install opened a folder dialog")
		}
		return []string{archive}, nil
	}
	t.Cleanup(func() { pickInstallPath = previous })
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) {
		matches, err := filepath.Glob(filepath.Join(a.plugins.Dir(), ".local-*"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("unpacked dirs = %v %v", matches, err)
		}
		if err := os.WriteFile(filepath.Join(matches[0], "index.js"), []byte("SWAPPED AFTER APPROVAL"), 0o644); err != nil {
			t.Fatal(err)
		}
		done(true)
	}
	a.installPluginZip()
	if a.pluginNotice != a.t(i18n.StoreChanged) {
		t.Fatalf("notice = %q", a.pluginNotice)
	}
	if _, err := a.plugins.Get("com.example.zipswap"); err == nil {
		t.Fatal("a swapped zip was installed")
	}
}

func TestZipSizeIsCheckedBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(plugin.MaxArchiveBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	reads := 0
	previous := readPluginZip
	readPluginZip = func(string) ([]byte, error) {
		reads++
		return nil, os.ErrInvalid
	}
	t.Cleanup(func() { readPluginZip = previous })
	_, err = readZipLimited(path)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
	if reads != 0 {
		t.Fatalf("oversized zip was read %d times", reads)
	}
}

func TestApplyAgentPortUsesTheUIBounds(t *testing.T) {
	a := newTestApp(t)
	a.settings.AgentPort = 8765
	for _, raw := range []string{"1", "1023", "65536"} {
		a.agentErr = ""
		a.agentPortDraft = raw
		a.applyAgentPort()
		if a.agentErr == "" || a.settings.AgentPort != 8765 {
			t.Fatalf("port %s: err %q saved %d", raw, a.agentErr, a.settings.AgentPort)
		}
	}
	a.agentErr = ""
	a.agentPortDraft = "1024"
	a.applyAgentPort()
	if a.agentErr != "" || a.settings.AgentPort != 1024 {
		t.Fatalf("port 1024: err %q saved %d", a.agentErr, a.settings.AgentPort)
	}
	a.agentPortDraft = "65535"
	a.applyAgentPort()
	if a.agentErr != "" || a.settings.AgentPort != 65535 {
		t.Fatalf("port 65535: err %q saved %d", a.agentErr, a.settings.AgentPort)
	}
}

func TestFiatTextUsesThePriceKey(t *testing.T) {
	a := newTestApp(t)
	a.snap = &snapshot{usd: map[string]float64{"CBETH": 2.5, "ETH": 3}}
	one := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	text, ok := a.fiatText(one, 18, "cbETH")
	if !ok || text != "$2.50" {
		t.Fatalf("cbETH fiat = %q ok=%v", text, ok)
	}
	text, ok = a.fiatText(one, 18, "ETH")
	if !ok || text != "$3.00" {
		t.Fatalf("ETH fiat = %q ok=%v", text, ok)
	}
}

func writeReviewPlugin(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
	  "id": "` + id + `",
	  "name": "Swap",
	  "version": "1.0.0",
	  "main": "index.js",
	  "permissions": {"storageKb": 1}
	}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`export function page() { return {}; }`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipPlugin(t *testing.T, id, source string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := `{
	  "id": "` + id + `",
	  "name": "Zip",
	  "version": "1.0.0",
	  "main": "index.js",
	  "permissions": {"storageKb": 1}
	}`
	for name, body := range map[string]string{"plugin.json": manifest, "index.js": source} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
