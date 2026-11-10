package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/wallet"
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
	_, err = readZipLimited(path)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
	if _, err := readZipLimited(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory: %v", err)
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

func TestInstallReadsThePrivateCopy(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "src")
	writeReviewPlugin(t, dir, "com.example.copy")
	previousPick := pickInstallPath
	pickInstallPath = func(mygo.OpenDialogOptions) ([]string, error) { return []string{dir}, nil }
	previousHook := afterSourceFrozen
	afterSourceFrozen = func(src, _ string) {
		if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("SWAPPED SOURCE"), 0o644); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() {
		pickInstallPath = previousPick
		afterSourceFrozen = previousHook
	})
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(true) }
	a.installPlugin()
	inst, err := a.plugins.Get("com.example.copy")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(inst.Dir, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "SWAPPED SOURCE") {
		t.Fatal("install read the original folder after the private copy was taken")
	}
	if inst.InstalledVia != "folder" {
		t.Fatalf("folder install recorded %q", inst.InstalledVia)
	}
	if left := tempInstalls(t, tmp); len(left) != 0 {
		t.Fatalf("private copies left in TMPDIR: %v", left)
	}
}

func TestZipInstallRecordsZipSource(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	a := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "app.zip")
	if err := os.WriteFile(archive, zipPlugin(t, "com.example.zipvia", `export function page() { return {}; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := pickInstallPath
	pickInstallPath = func(mygo.OpenDialogOptions) ([]string, error) { return []string{archive}, nil }
	t.Cleanup(func() { pickInstallPath = previous })
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(true) }
	a.installPluginZip()
	inst, err := a.plugins.Get("com.example.zipvia")
	if err != nil {
		t.Fatal(err)
	}
	if inst.InstalledVia != "zip" {
		t.Fatalf("zip install recorded %q", inst.InstalledVia)
	}
	if left := tempInstalls(t, tmp); len(left) != 0 {
		t.Fatalf("private copies left in TMPDIR: %v", left)
	}
}

func TestSymlinkFolderSaysWhy(t *testing.T) {
	a := newTestApp(t)
	real := filepath.Join(t.TempDir(), "real")
	writeReviewPlugin(t, real, "com.example.link")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	previous := pickInstallPath
	pickInstallPath = func(mygo.OpenDialogOptions) ([]string, error) { return []string{link}, nil }
	t.Cleanup(func() { pickInstallPath = previous })
	a.installPlugin()
	if !strings.Contains(a.pluginNotice, "cannot be a symlink") {
		t.Fatalf("notice = %q", a.pluginNotice)
	}
	if _, err := a.plugins.Get("com.example.link"); err == nil {
		t.Fatal("a symlink folder was installed")
	}
}

func TestInstallFailureUsesTranslatedPluginError(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "src")
	writeReviewPlugin(t, dir, "com.example.busy")
	release, err := a.pluginManager.TryExecution("com.example.busy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	m, err := plugin.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plugin.DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.installApprovedSource(dir, m, digest, "folder")
	if strings.Contains(a.pluginNotice, "应用正忙") || !strings.Contains(a.pluginNotice, "busy") {
		t.Fatalf("notice = %q", a.pluginNotice)
	}
}

func TestProvenanceDeleteFailureStillClearsTheDraft(t *testing.T) {
	secrets := failProvDelete{memStore: memStore{}}
	a := newTestAppWithPluginSecrets(t, secrets)
	id := "local.personal.provfail"
	dir := filepath.Join(t.TempDir(), "src")
	writePluginWithSecret(t, dir, id)
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	*a.pluginDrafts[id+"/apiKey"] = "stale-secret"
	a.pluginRunners[id] = &plugin.Runner{}
	previous := confirmPluginRemoval
	confirmPluginRemoval = func(string, string, string, string) bool { return true }
	t.Cleanup(func() { confirmPluginRemoval = previous })
	a.removePlugin(id)
	if _, ok := a.pluginDrafts[id+"/apiKey"]; ok {
		t.Fatal("provenance failure kept the secret draft")
	}
	if _, ok := a.pluginRunners[id]; ok {
		t.Fatal("provenance failure kept the runner")
	}
	for _, item := range a.pluginItems {
		if item.Manifest.ID == id {
			t.Fatal("provenance failure left the app in the list")
		}
	}
	if !strings.Contains(a.pluginNotice, "provenance record could not be deleted") {
		t.Fatalf("notice = %q", a.pluginNotice)
	}
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.savePluginSettings(plugin.Manifest{ID: id, Settings: []plugin.SettingField{{Key: "apiKey", Label: "API key", Secret: true}}})
	if v, err := a.plugins.Setting(id, "apiKey"); err != nil || v != "" {
		t.Fatalf("reinstall saved %q (%v)", v, err)
	}
}

func TestRefreshVaultErrorShowsTheWarning(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.newReader()
	vault := "Vault1111111111111111111111111111111111111"
	a.settings.Passkey = &PasskeyAccount{ID: "cred", PublicKey: "p256:AA", NEARAccount: "0s.test", SolanaAddress: vault}
	a.settings.Accounts = []wallet.Address{{Addr: vault}}
	a.reader = vaultErrReader{}
	done := make(chan struct{})
	previous := runOnUI
	runOnUI = func(fn func()) {
		fn()
		select {
		case <-done:
		default:
			close(done)
		}
	}
	t.Cleanup(func() { runOnUI = previous })
	a.fetch(a.gen, a.reader, []string{vault})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if a.snap == nil || a.snap.vault != vaultUnknown {
		t.Fatalf("vault state = %v", a.snap)
	}
	if got := a.passkeySolanaBalance(); got != "" {
		t.Fatalf("balance = %q", got)
	}
	a.route = routeWallet
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Couldn't check whether this vault address is already taken") {
		t.Fatalf("warning missing in %q", tt.Texts())
	}
}

type failProvDelete struct{ memStore }

func (s failProvDelete) Delete(key string) error {
	if strings.HasPrefix(key, "provenance:") {
		return errors.New("credential store refused")
	}
	delete(s.memStore, key)
	return nil
}

func newTestAppWithPluginSecrets(t *testing.T, secrets plugin.Secrets) *App {
	t.Helper()
	a := &App{
		settings:      DefaultSettings(),
		settingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		systemLocale:  i18n.English,
		pluginSecrets: secrets,
	}
	a.newReader()
	a.initPlugins(t.TempDir())
	a.keyring = wallet.NewKeyring(memStore{}, nil)
	a.nearKeyring = wallet.NewNearKeyring(memStore{}, nil)
	a.solanaKeyring = wallet.NewSolanaKeyring(memStore{}, nil)
	a.startRefresh = func(int, chain.Reader, []string) {}
	a.runOnMain = func(fn func()) { fn() }
	a.goAsync = func(fn func()) { fn() }
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(true) }
	a.confirmProvenance = func(_ plugin.ProvenanceView, done func(bool)) { done(false) }
	a.confirmConnect = func(string) bool { return true }
	a.approve = func(approvalRequest) bool { return true }
	a.dappConnected = map[string]bool{}
	a.openExternal = func(string) error { return nil }
	return a
}

type vaultErrReader struct{}

func (vaultErrReader) Account(context.Context, string) (chain.AccountBalances, error) {
	return chain.AccountBalances{Native: big.NewInt(5_000_000_000)}, nil
}
func (vaultErrReader) Block(context.Context) (uint64, error) { return 1, nil }
func (vaultErrReader) RPC(context.Context, string, any) (json.RawMessage, error) {
	return nil, errors.New("no rpc")
}
func (vaultErrReader) AccountExists(context.Context, string) (bool, error) {
	return false, errors.New("rpc down")
}

func tempInstalls(t *testing.T, dir string) []string {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(dir, "lintel-install-*"))
	if err != nil {
		t.Fatal(err)
	}
	return left
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
