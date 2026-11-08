package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// examplesDir is where the example plugins live: test fixtures, not shipped
// with the app.
var examplesDir = filepath.Join("testdata", "examples")

// installExample installs one example into a fresh store and returns its
// manifest and source.
func installExample(t *testing.T, name string) (Manifest, []byte) {
	t.Helper()
	store := NewStore(filepath.Join(t.TempDir(), "plugins"), nil)
	m, err := store.Install(filepath.Join(examplesDir, name))
	if err != nil {
		t.Fatalf("installing %s: %v", name, err)
	}
	inst, err := store.Get(m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	source, err := os.ReadFile(filepath.Join(inst.Dir, filepath.FromSlash(m.Main)))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return m, source
}

func TestExampleGas(t *testing.T) {
	m, source := installExample(t, "gas")
	host := NewHost(HostDeps{
		Manifest: m,
		RPC: func(chain, method string, params any) (json.RawMessage, error) {
			switch method {
			case "eth_gasPrice":
				return json.RawMessage(`"0x3b9aca00"`), nil // 1 gwei
			case "eth_blockNumber":
				return json.RawMessage(`"0x10"`), nil
			}
			return nil, fmt.Errorf("unexpected method %s", method)
		},
	})
	r, err := NewRunner(m, source, host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "Gas" || len(page.Sections) != 3 {
		t.Fatalf("page = %+v", page)
	}
	rows := page.Sections[1].Table.Rows
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	want := []string{"Ethereum", "1.00", "16"}
	for i := range want {
		if rows[0][i] != want[i] {
			t.Fatalf("row = %v, want %v", rows[0], want)
		}
	}
}

func TestExampleFearGreedCompiles(t *testing.T) {
	m, source := installExample(t, "fear-greed")
	if len(m.Permissions.Network) != 1 || m.Permissions.Network[0] != "api.alternative.me" {
		t.Fatalf("permissions = %+v", m.Permissions)
	}
	r, err := NewRunner(m, source, NewHost(HostDeps{Manifest: m}), "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r.Close()
}
