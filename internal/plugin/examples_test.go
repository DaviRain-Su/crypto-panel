package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// examplesDir is the source of the official examples. The shell embeds these
// folders and copies them in on first launch; tests also install them by
// hand, which stays an unsigned folder install.
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

func TestExampleNearApprovalOnlySignsOnRequest(t *testing.T) {
	m, source := installExample(t, "near-approval")
	if !m.Permissions.Sign || len(m.Permissions.Chains) != 1 || m.Permissions.Chains[0] != "near:testnet" || len(m.Permissions.Network) != 0 {
		t.Fatalf("permissions = %+v", m.Permissions)
	}
	calls := 0
	var got NearSignRequest
	host := NewHost(HostDeps{Manifest: m, NearSign: func(req NearSignRequest) (SignResult, error) {
		calls++
		got = req
		return SignResult{Hash: "real-testnet-hash"}, nil
	}})
	r, err := NewRunner(m, source, host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	overview, err := r.Page("overview")
	if err != nil || calls != 0 || len(overview.Sections) == 0 {
		t.Fatalf("overview = %+v, calls = %d, err = %v", overview, calls, err)
	}
	page, err := r.Page("transfer")
	if err != nil || calls != 1 || page.Title != "Transaction submitted" {
		t.Fatalf("transfer = %+v, calls = %d, err = %v", page, calls, err)
	}
	if got.ChainID != "near:testnet" || got.Receiver != "bob.testnet" || len(got.Actions) != 1 || got.Actions[0].Amount != "1000000000000000000000" {
		t.Fatalf("request = %+v", got)
	}
	data, _ := json.Marshal(page)
	if !strings.Contains(string(data), "real-testnet-hash") {
		t.Fatalf("hash missing from %s", data)
	}
}

func TestExampleNearApprovalShowsARejectionWithoutAHash(t *testing.T) {
	m, source := installExample(t, "near-approval")
	host := NewHost(HostDeps{Manifest: m, NearSign: func(NearSignRequest) (SignResult, error) {
		return SignResult{}, errors.New("the user rejected the transaction")
	}})
	r, err := NewRunner(m, source, host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page("transfer")
	if err != nil || page.Title != "Request not signed" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	data, _ := json.Marshal(page)
	if !strings.Contains(string(data), "rejected") || strings.Contains(string(data), "transactionHash") {
		t.Fatalf("rejection page = %s", data)
	}
}
