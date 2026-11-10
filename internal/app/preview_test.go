package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/plugin"
)

func previewSource(t *testing.T, id, source string) plugin.Source {
	t.Helper()
	manifest, err := json.Marshal(plugin.Manifest{
		ID: id, Name: "Preview candidate", Version: "1.0.0", Main: "index.js",
		Permissions: plugin.Permissions{Sign: true, Chains: []string{"eip155:1", "near:testnet"}},
		Navigation:  []plugin.NavItem{{Title: "Overview", Page: "overview"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: source}}
}

func TestPreviewPendingCandidateDoesNotApproveOrInstall(t *testing.T) {
	a := newTestApp(t)
	job, err := a.pluginManager.Propose(previewSource(t, "local.personal.preview", `export function page() {
  return { title: "Frozen preview", sections: [{ stats: [{ label: "Value", value: "42", trend: "up", sparkline: [1, 2, 3] }], gauge: { label: "Health", value: 62, text: "62%" } }] };
}`), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	confirmCalls := 0
	a.confirmInstall = func(plugin.Manifest, installRequest, func(bool)) { confirmCalls++ }
	result, err := a.PreviewInstallationPage(job.RequestID, "", "acp")
	if err != nil {
		t.Fatalf("PreviewInstallationPage: %v", err)
	}
	page, ok := result.Page.(pages.Page)
	if !ok || page.Title != "Frozen preview" || result.PackageHash != job.PackageHash || result.EvaluatedAt.IsZero() {
		t.Fatalf("result = %+v", result)
	}
	current, err := a.pluginManager.GetInstallation(job.RequestID)
	if err != nil || current.Status != "pending" || confirmCalls != 0 {
		t.Fatalf("preview changed the candidate: %+v, confirmations = %d", current, confirmCalls)
	}
	if _, err := a.plugins.Get(job.PluginID); !errors.Is(err, plugin.ErrNotInstalled) {
		t.Fatalf("preview installed the candidate: %v", err)
	}
	if _, err := a.PreviewInstallationPage(job.RequestID, "", "local-api"); err == nil {
		t.Fatal("the local API accessed an ACP candidate")
	}
}

func TestPreviewUsesEphemeralKVForBothOrigins(t *testing.T) {
	for _, via := range []string{"acp", "local-api"} {
		t.Run(via, func(t *testing.T) {
			a := newTestApp(t)
			manifest, err := json.Marshal(plugin.Manifest{
				ID: "local.personal.preview", Name: "Preview candidate", Version: "1.0.0", Main: "index.js",
				Permissions: plugin.Permissions{StorageKb: 4},
				Navigation:  []plugin.NavItem{{Title: "Overview", Page: "overview"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			source := `export function page() {
  const initial = host.kv.get("count") === undefined;
  host.kv.put("count", 1);
  host.kv.put("count", 2);
  host.kv.delete("other");
  return { title: String(initial) + "|" + host.kv.get("count"), sections: [] };
}`
			job, err := a.pluginManager.Propose(plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: source}}, "install", "", "", via)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				result, err := a.PreviewInstallationPage(job.RequestID, "overview", via)
				if err != nil {
					t.Fatal(err)
				}
				page := result.Page.(pages.Page)
				if page.Title != "true|2" {
					t.Fatalf("preview %d retained or lost KV: %q", i, page.Title)
				}
				if result.Signing != "disabled" || result.Storage != "ephemeral" || result.Network != "declared methods allowed" {
					t.Fatalf("preview metadata = %+v", result)
				}
			}
			if _, err := os.Stat(filepath.Join(a.plugins.Dir(), "local.personal.preview", "kv.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview persisted KV: %v", err)
			}
		})
	}
}

func TestUpdatePreviewCannotReadOrMutateInstalledKV(t *testing.T) {
	a := newTestApp(t)
	manifest, err := json.Marshal(plugin.Manifest{
		ID: "local.personal.preview", Name: "Preview candidate", Version: "1.0.0", Main: "index.js",
		Permissions: plugin.Permissions{StorageKb: 4},
		Navigation:  []plugin.NavItem{{Title: "Overview", Page: "overview"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := a.pluginManager.Propose(plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: `export function page(){return {title:"old",sections:[]};}`}}, "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pluginManager.Decide(installed.RequestID, true); err != nil {
		t.Fatal(err)
	}
	kv, err := a.plugins.KV("local.personal.preview")
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Put("count", json.RawMessage(`99`)); err != nil {
		t.Fatal(err)
	}
	var updated plugin.Manifest
	if err := json.Unmarshal(manifest, &updated); err != nil {
		t.Fatal(err)
	}
	updated.Version = "1.0.1"
	updatedManifest, _ := json.Marshal(updated)
	job, err := a.pluginManager.Propose(plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: updatedManifest, Main: `export function page(){
  const before=host.kv.get("count");
  host.kv.put("count", 2);
  host.kv.delete("count");
  return {title:String(before),sections:[]};
}`}}, "update", "local.personal.preview", installed.PackageHash, "acp")
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.PreviewInstallationPage(job.RequestID, "overview", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if result.Page.(pages.Page).Title != "undefined" {
		t.Fatalf("preview read installed KV: %+v", result.Page)
	}
	stored, ok := kv.Get("count")
	if !ok || string(stored) != "99" {
		t.Fatalf("installed KV changed: %s, %v", stored, ok)
	}
}

func nearApprovalExamplePackage(t *testing.T) *plugin.Package {
	t.Helper()
	root := filepath.Join("..", "plugin", "testdata", "examples", "near-approval")
	manifest, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed plugin.Manifest
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.ID = "local.personal.near-approval"
	manifest, err = json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return &plugin.Package{Plugin: manifest, Main: string(source)}
}

func TestPreviewNearApprovalExampleRefusesSigning(t *testing.T) {
	a := newTestApp(t)
	job, err := a.pluginManager.Propose(plugin.Source{Kind: "inline", Package: nearApprovalExamplePackage(t)}, "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewInstallationPage(job.RequestID, "overview", "acp"); err != nil {
		t.Fatalf("safe overview preview: %v", err)
	}
	result, err := a.PreviewInstallationPage(job.RequestID, "transfer", "acp")
	if err != nil {
		t.Fatalf("the example should render its refusal: %v", err)
	}
	page, ok := result.Page.(pages.Page)
	if !ok || page.Title != "Request not signed" {
		t.Fatalf("preview page = %+v", result.Page)
	}
	current, _ := a.pluginManager.GetInstallation(job.RequestID)
	if current.Status != "pending" {
		t.Fatalf("preview changed status to %s", current.Status)
	}
	if _, err := a.plugins.Get(job.PluginID); !errors.Is(err, plugin.ErrNotInstalled) {
		t.Fatalf("preview installed the example: %v", err)
	}
}

func TestPreviewPendingCandidateRefusesSigning(t *testing.T) {
	for _, tc := range []struct{ name, call string }{
		{"evm", `host.requestSign({ chainId: "eip155:1", to: "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23", value: "1" });`},
		{"near", `host.requestNearSign({ chainId: "near:testnet", receiver: "bob.testnet", actions: [{ type: "transfer", amount: "1" }] });`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			job, err := a.pluginManager.Propose(previewSource(t, "local.personal.preview", `export function page() {
  `+tc.call+`
  return { title: "Must not return", sections: [] };
}`), "install", "", "", "acp")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.PreviewInstallationPage(job.RequestID, "overview", "acp"); err == nil || !strings.Contains(err.Error(), "signing is not available") {
				t.Fatalf("signing was available: %v", err)
			}
			current, _ := a.pluginManager.GetInstallation(job.RequestID)
			if current.Status != "pending" {
				t.Fatalf("status = %q", current.Status)
			}
		})
	}
}

func TestPreviewPendingCandidateRejectsUnknownInstalledUndeclaredAndTampered(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.PreviewInstallationPage("unknown", "", "acp"); err == nil {
		t.Fatal("an unknown request was previewed")
	}
	job, err := a.pluginManager.Propose(previewSource(t, "local.personal.preview", `export function page() { return { title: "Preview", sections: [] }; }`), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewInstallationPage(job.RequestID, "secret", "acp"); err == nil {
		t.Fatal("an undeclared page was previewed")
	}
	candidate, _, err := a.pluginManager.PendingCandidate(job.RequestID, "acp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate.Dir, "index.js"), []byte(`export function page() { return { title: "Tampered", sections: [] }; }`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewInstallationPage(job.RequestID, "overview", "acp"); err == nil {
		t.Fatal("a tampered package was previewed")
	}
}

func TestPreviewPendingCandidateRejectsInstalledJob(t *testing.T) {
	a := newTestApp(t)
	job, err := a.pluginManager.Propose(previewSource(t, "local.personal.preview", `export function page() { return { title: "Preview", sections: [] }; }`), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pluginManager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PreviewInstallationPage(job.RequestID, "", "acp"); err == nil {
		t.Fatal("an installed request was previewed as pending")
	}
}

func TestCreatorPreviewRejectsCandidateWithoutPages(t *testing.T) {
	a := newTestApp(t)
	manifest, err := json.Marshal(plugin.Manifest{ID: "local.personal.empty", Name: "Empty", Version: "1.0.0", Main: "index.js"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := a.pluginManager.Propose(plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: `export function page() { return { sections: [] }; }`}}, "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	a.coding.installationID = job.RequestID
	a.previewGeneratedApp()
	if a.route.kind == "preview" || a.coding.err != a.t(i18n.AppNoPages) {
		t.Fatalf("route = %+v, err = %q", a.route, a.coding.err)
	}
}

func TestCreatorShowsPreviewAndNativeReviewSeparately(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.Language = language
			job, err := a.pluginManager.Propose(previewSource(t, "local.personal.preview", `export function page() { return { title: "Actual candidate page", sections: [] }; }`), "install", "", "", "acp")
			if err != nil {
				t.Fatal(err)
			}
			a.openCreator("")
			a.coding.installationID = job.RequestID
			tt := ui.NewTester(a.view, windowWidth, windowHeight)
			for _, label := range []string{a.t(i18n.CodingPreview), a.t(i18n.PendingReview)} {
				if !tt.HasText(label) {
					t.Fatalf("missing %q in %q", label, tt.Texts())
				}
			}
			a.previewGeneratedApp()
			tt = ui.NewTester(a.view, windowWidth, windowHeight)
			if a.route.kind != "preview" || !tt.HasText("Actual candidate page") || !tt.HasText(a.t(i18n.CodingPreviewNotice)) {
				t.Fatalf("preview did not render the candidate page: route = %+v, texts = %q", a.route, tt.Texts())
			}
			current, _ := a.pluginManager.GetInstallation(job.RequestID)
			if current.Status != "pending" {
				t.Fatalf("status = %q", current.Status)
			}
		})
	}
}
