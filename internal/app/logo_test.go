package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestAppLogoMatchesResource keeps the embedded Dock icon and the bundled
// app's icon the same art: tools/icon writes both, and this fails when one
// is regenerated without the other.
func TestAppLogoMatchesResource(t *testing.T) {
	bundled, err := os.ReadFile(filepath.Join("..", "..", "resources", "icon.png"))
	if err != nil {
		t.Fatalf("reading the bundled icon: %v", err)
	}
	if !bytes.Equal(bundled, appLogoPNG) {
		t.Fatal("internal/app/logo.png differs from resources/icon.png; run `go run ./tools/icon` to regenerate both")
	}
}
