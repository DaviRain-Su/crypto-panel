package agent

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

func TestAPIErrorsAreEnglish(t *testing.T) {
	s, manager, store := managementServer(t)
	dir := t.TempDir()
	manifest := `{"id":"com.example.locked","name":"Locked","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"settings":[{"key":"label","label":"Label"}],"navigation":[{"title":"Overview","page":"overview"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`export function page(){return {sections:[]};}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install(dir); err != nil {
		t.Fatal(err)
	}
	denied := jsonRequest(t, s, http.MethodPut, "/v0/plugins/com.example.locked/settings", map[string]any{"values": map[string]string{"label": "nope"}})
	assertEnglishCode(t, denied, http.StatusForbidden, "API_MUTATION_FORBIDDEN", i18n.PluginErrAPIMutationForbidden)

	acp, err := manager.Propose(plugin.Source{Kind: "inline", Package: &plugin.Package{
		Plugin: json.RawMessage(`{"id":"local.personal.acp","name":"ACP","version":"1.0.0","main":"index.js","permissions":{"storageKb":1},"navigation":[{"title":"Overview","page":"overview"}]}`),
		Main:   `export function page(){return {sections:[]};}`,
	}}, "install", "", "", "acp")
	if err != nil {
		t.Fatal(err)
	}
	origin := do(t, s, http.MethodDelete, "/v0/installations/"+acp.RequestID, "test-token")
	assertEnglishCode(t, origin, http.StatusForbidden, "INSTALLATION_ORIGIN_FORBIDDEN", i18n.PluginErrInstallationOriginForbidden)

	proposed := jsonRequest(t, s, http.MethodPost, "/v0/plugins", httpPackage(t, "1.0.0"))
	if proposed.Code != http.StatusAccepted {
		t.Fatalf("propose: %d %s", proposed.Code, proposed.Body)
	}
	var job plugin.Installation
	if err := json.Unmarshal(proposed.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(store.Dir(), ".lifecycle", "pending")
	changed := false
	err = filepath.WalkDir(pending, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || filepath.Base(path) != "index.js" {
			return walkErr
		}
		changed = true
		return os.WriteFile(path, []byte(`export function page(){return {title:"changed"};}`), 0o600)
	})
	if err != nil || !changed {
		t.Fatalf("could not change the frozen candidate: %v changed=%v", err, changed)
	}
	if _, err := manager.Decide(job.RequestID, true); err == nil {
		t.Fatal("a changed candidate was installed")
	}
	status := do(t, s, http.MethodGet, "/v0/installations/"+job.RequestID, "test-token")
	assertEnglishCode(t, status, http.StatusOK, "CANDIDATE_CHANGED", i18n.PluginErrCandidateChanged)
}

func TestProvenanceDeleteFailureIsEnglish(t *testing.T) {
	_, base := newTestServer(t)
	base.plugins = nil
	secrets := provenanceDeleteFails{credentialMemory: credentialMemory{}}
	store := plugin.NewStore(t.TempDir(), secrets)
	manager, err := plugin.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	s := New(&managedData{base, manager}, "test-token")
	proposed := jsonRequest(t, s, http.MethodPost, "/v0/plugins", httpPackage(t, "1.0.0"))
	if proposed.Code != http.StatusAccepted {
		t.Fatalf("propose: %d %s", proposed.Code, proposed.Body)
	}
	var job plugin.Installation
	if err := json.Unmarshal(proposed.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	removed := do(t, s, http.MethodDelete, "/v0/plugins/"+job.PluginID, "test-token")
	assertEnglishCode(t, removed, http.StatusInternalServerError, "PROVENANCE_DELETE_FAILED", i18n.PluginErrProvenanceDeleteFailed)
	if strings.Contains(removed.Body.String(), "provenance:") {
		t.Fatalf("error names the credential account: %s", removed.Body)
	}
}

func TestSettingsStatusAlwaysSerializesSecret(t *testing.T) {
	s, manager, _ := managementServer(t)
	body := map[string]any{"source": map[string]any{"kind": "inline", "package": map[string]any{
		"plugin": map[string]any{
			"id": "local.personal.plain", "name": "Plain", "version": "1.0.0", "main": "index.js",
			"permissions": map[string]any{"storageKb": 1},
			"settings":    []any{map[string]any{"key": "label", "label": "Label"}, map[string]any{"key": "token", "label": "Token", "secret": true}},
			"navigation":  []any{map[string]any{"page": "overview", "title": "Overview"}},
		},
		"main": `export function page() { return { title: "Plain", sections: [] }; }`,
	}}}
	proposed := jsonRequest(t, s, http.MethodPost, "/v0/plugins", body)
	if proposed.Code != http.StatusAccepted {
		t.Fatalf("propose: %d %s", proposed.Code, proposed.Body)
	}
	var job plugin.Installation
	if err := json.Unmarshal(proposed.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decide(job.RequestID, true); err != nil {
		t.Fatal(err)
	}
	settings := jsonRequest(t, s, http.MethodPut, "/v0/plugins/"+job.PluginID+"/settings", map[string]any{"values": map[string]string{"label": "pool"}})
	if settings.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", settings.Code, settings.Body)
	}
	text := settings.Body.String()
	if !strings.Contains(text, `"secret":false`) || !strings.Contains(text, `"secret":true`) {
		t.Fatalf("settings status omitted secret: %s", text)
	}
	if !strings.Contains(text, `"value":"pool"`) {
		t.Fatalf("plain value missing: %s", text)
	}
}

type provenanceDeleteFails struct{ credentialMemory }

func (s provenanceDeleteFails) Delete(key string) error {
	if strings.HasPrefix(key, "provenance:") {
		return errors.New("credential store refused")
	}
	delete(s.credentialMemory, key)
	return nil
}

func assertEnglishCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string, key i18n.Key) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	want := i18n.T(i18n.English, key)
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code || body.Error != want {
		t.Fatalf("error %q code %q, want English %q / %s; body %s", body.Error, body.Code, want, code, rec.Body)
	}
	if strings.Contains(body.Error, "应用") || strings.Contains(body.Error, "候选") || strings.Contains(body.Error, "本机") {
		t.Fatalf("API returned the plugin-layer Chinese message: %s", body.Error)
	}
}
