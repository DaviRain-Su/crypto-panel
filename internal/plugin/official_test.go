package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureOfficialInstallsTheTwoExamples(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if err := store.EnsureOfficial(); err != nil {
		t.Fatalf("EnsureOfficial: %v", err)
	}
	if err := store.EnsureOfficial(); err != nil {
		t.Fatalf("second EnsureOfficial: %v", err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List = %+v", list)
	}
	got := map[string]Installed{}
	for _, inst := range list {
		got[inst.Manifest.ID] = inst
	}
	for _, id := range firstLaunchExampleIDs {
		inst, ok := got[id]
		if !ok {
			t.Fatalf("missing %s in %+v", id, list)
		}
		if !inst.Official || inst.Signed || inst.Source != OfficialSource || !inst.Enabled || inst.Err != "" {
			t.Fatalf("%s = %+v", id, inst)
		}
		if inst.Manifest.Permissions.Sign || len(inst.Manifest.Permissions.Read) != 0 || inst.Manifest.Permissions.StorageKb != 0 {
			t.Fatalf("%s permissions = %+v", id, inst.Manifest.Permissions)
		}
	}
	gas := got["com.example.gas"]
	if len(gas.Manifest.Permissions.Chains) != 2 || len(gas.Manifest.Permissions.Network) != 0 {
		t.Fatalf("gas permissions = %+v", gas.Manifest.Permissions)
	}
	fear := got["com.example.feargreed"]
	if len(fear.Manifest.Permissions.Network) != 1 || fear.Manifest.Permissions.Network[0] != "api.alternative.me" ||
		len(fear.Manifest.Permissions.Chains) != 0 {
		t.Fatalf("feargreed permissions = %+v", fear.Manifest.Permissions)
	}
	assertSameFiles(t, gas.Dir, filepath.Join(examplesDir, "gas"))
	assertSameFiles(t, fear.Dir, filepath.Join(examplesDir, "fear-greed"))
}

func TestAuthoringExamplesAreEmbeddedButNotAutoInstalled(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"com.example.hyperliquid", "com.example.intents"} {
		pkg, hash, err := OfficialPackage(id)
		if err != nil || hash == "" {
			t.Fatalf("OfficialPackage(%s): hash=%q err=%v", id, hash, err)
		}
		manifest, err := ParseManifest(pkg.Plugin)
		if err != nil || manifest.Permissions.Sign || len(manifest.Permissions.Network) != 1 {
			t.Fatalf("%s manifest=%+v err=%v", id, manifest, err)
		}
		if _, err := store.Get(id); !errors.Is(err, ErrNotInstalled) {
			t.Fatalf("authoring example %s was auto-installed: %v", id, err)
		}
	}
}

func TestSeededGasRunsWithItsManifest(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	inst, err := store.Get("com.example.gas")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(inst.Dir, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	host := NewHost(HostDeps{
		Manifest: inst.Manifest,
		RPC: func(chain, method string, params any) (json.RawMessage, error) {
			switch method {
			case "eth_gasPrice":
				return json.RawMessage(`"0x3b9aca00"`), nil
			case "eth_blockNumber":
				return json.RawMessage(`"0x10"`), nil
			}
			return nil, fmt.Errorf("unexpected method %s", method)
		},
	})
	r, err := NewRunner(inst.Manifest, source, host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "Gas" || len(page.Sections) < 2 || len(page.Sections[1].Table.Rows) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Sections[1].Table.Rows[0][0] != "Ethereum" {
		t.Fatalf("row = %v", page.Sections[1].Table.Rows[0])
	}
}

func TestEnsureOfficialIsFirstLaunchOnly(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("com.example.gas"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("com.example.gas"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("removed example was reinstalled: %v", err)
	}
	fear, err := store.Get("com.example.feargreed")
	if err != nil || !fear.Official {
		t.Fatalf("feargreed = %+v, %v", fear, err)
	}
}

func TestEnsureOfficialDoesNotReplaceAFolderInstall(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if _, err := store.Install(filepath.Join(examplesDir, "gas")); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	gas, err := store.Get("com.example.gas")
	if err != nil || gas.Official || gas.Signed || gas.Err != "" {
		t.Fatalf("folder install was rewritten: %+v, %v", gas, err)
	}
	fear, err := store.Get("com.example.feargreed")
	if err != nil || !fear.Official || fear.Signed {
		t.Fatalf("feargreed = %+v, %v", fear, err)
	}
}

func TestFolderInstallOfExampleStaysUnsigned(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if _, err := store.Install(filepath.Join(examplesDir, "gas")); err != nil {
		t.Fatal(err)
	}
	gas, err := store.Get("com.example.gas")
	if err != nil || gas.Official || gas.Signed || gas.Source != "" || gas.Err != "" {
		t.Fatalf("folder install = %+v, %v", gas, err)
	}
}

func TestCopyOfOfficialInstallStaysUnsigned(t *testing.T) {
	src := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if err := src.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	inst, err := src.Get("com.example.gas")
	if err != nil {
		t.Fatal(err)
	}
	dst := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if _, err := dst.Install(inst.Dir); err != nil {
		t.Fatal(err)
	}
	got, err := dst.Get("com.example.gas")
	if err != nil || got.Official || got.Signed || got.Err != "" {
		t.Fatalf("copied install = %+v, %v", got, err)
	}
}

func TestTamperedOfficialExampleIsBroken(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	if err := store.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	inst, err := store.Get("com.example.gas")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst.Dir, "index.js"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("com.example.gas")
	if err != nil {
		t.Fatal(err)
	}
	if got.Official || got.Signed || !strings.Contains(got.Err, "changed") {
		t.Fatalf("tampered = %+v", got)
	}
}

func TestFakeOfficialRecordDoesNotRun(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	inst, err := store.Get("com.example.store")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DigestDir(inst.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInstallInfo(inst.Dir, installInfo{
		Source:   OfficialSource,
		Hash:     digest,
		Official: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("com.example.store")
	if err != nil {
		t.Fatal(err)
	}
	if got.Official || got.Signed || !strings.Contains(got.Err, "changed") {
		t.Fatalf("fake official = %+v", got)
	}
}

func assertSameFiles(t *testing.T, gotDir, wantDir string) {
	t.Helper()
	for _, name := range []string{"plugin.json", "index.js"} {
		got, err := os.ReadFile(filepath.Join(gotDir, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(wantDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs from the testdata example", name)
		}
	}
}
