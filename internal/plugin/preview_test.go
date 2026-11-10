package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPendingCandidatePreviewRequiresPendingOwnerAndFrozenPackage(t *testing.T) {
	_, m := newTestManager(t, nil)
	job := proposeTest(t, m, personalPackage(t, "local.personal.preview", "1.0.0"), "install", "")
	candidate, hash, err := m.PendingCandidate(job.RequestID, "local-api")
	if err != nil || candidate.Manifest.ID != job.PluginID || hash != job.PackageHash || candidate.Dir == "" {
		t.Fatalf("candidate = %+v, err = %v", candidate, err)
	}
	if _, _, err := m.PendingCandidate(job.RequestID, "acp"); err == nil {
		t.Fatal("another manager origin accessed the candidate")
	}
	if _, _, err := m.PendingCandidate("missing", "local-api"); err == nil {
		t.Fatal("an unknown candidate was returned")
	}
	candidatePath := filepath.Join(candidate.Dir, "index.js")
	if err := os.WriteFile(candidatePath, []byte(`export function page() { return { title: "tampered", sections: [] }; }`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.PendingCandidate(job.RequestID, "local-api"); err == nil {
		t.Fatal("a tampered candidate was returned")
	}
}

func TestApprovalCannotStartDuringCandidatePreview(t *testing.T) {
	_, m := newTestManager(t, nil)
	job := proposeTest(t, m, personalPackage(t, "local.personal.lease", "1.0.0"), "install", "")
	release, err := m.TryExecution(job.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decide(job.RequestID, true); err == nil {
		t.Fatal("approval started during candidate preview")
	}
	current, err := m.GetInstallation(job.RequestID)
	if err != nil || current.Status != "pending" {
		t.Fatalf("status = %+v, err = %v", current, err)
	}
	release()
	if _, err := m.Decide(job.RequestID, true); err != nil {
		t.Fatalf("approval did not resume after preview: %v", err)
	}
}

func TestPendingCandidatePreviewRejectsInstalledJob(t *testing.T) {
	_, m := newTestManager(t, nil)
	job := installTest(t, m, personalPackage(t, "local.personal.installed", "1.0.0"))
	if _, _, err := m.PendingCandidate(job.RequestID, "local-api"); err == nil {
		t.Fatal("an installed candidate was returned")
	}
}
