package plugin

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestSignedAppUpdateRejectsTamperedInstallRecord(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "nohash", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, m := newTestManager(t, nil)
			id := "local.personal." + kind
			dir := t.TempDir()
			writeVersion(t, dir, id, "1.0.0", personalSource)
			_, private, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			entry := signEntry(t, dir, "https://example.com/signed.zip", private)
			archive, err := ZipDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := s.Prepare(archive, entry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepared.Install(); err != nil {
				t.Fatal(err)
			}
			hash, err := s.PackageHash(id)
			if err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(s.Dir(), id, "install.json")
			switch kind {
			case "missing":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
			case "empty":
				if err := os.WriteFile(record, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "nohash":
				if err := os.WriteFile(record, []byte("{\"installedVia\":\"local-api\"}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
				forged := filepath.Join(t.TempDir(), "forged.json")
				if err := os.WriteFile(forged, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(forged, record); err != nil {
					t.Fatal(err)
				}
			}
			next := personalPackage(t, id, "2.0.0")
			_, err = m.Propose(Source{Kind: "inline", Package: &next}, "update", id, hash, "local-api")
			requireCode(t, err, "SOURCE_NOT_ALLOWED")
		})
	}
}
