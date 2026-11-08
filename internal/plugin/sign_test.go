package plugin

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const signTo = "0x27b1fdb04752bbc536007a920d24acb045561c26"

func signManifest(t *testing.T, perms Permissions) Manifest {
	t.Helper()
	return testManifest(t, perms)
}

func TestRequestSignWithoutPermissionIsRefused(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestSign({ chainId: "eip155:1", to: "`+signTo+`" });
    return { title: "signed" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "sign permission") {
		t.Fatalf("requestSign was not refused: %+v", page)
	}
}

func TestRequestSignPassesTheIntent(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}, Sign: true})
	var got SignRequest
	host := NewHost(HostDeps{
		Manifest: m,
		Sign: func(req SignRequest) (SignResult, error) {
			got = req
			return SignResult{Hash: "0xabc"}, nil
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  const res = host.requestSign({
    chainId: "eip155:1",
    to: "`+signTo+`",
    value: "1000",
    data: "0xa9059cbb",
    gas: 21000,
    description: "Swap",
  });
  return { title: res.hash };
}
`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "0xabc" {
		t.Fatalf("title = %q", page.Title)
	}
	if got.ChainID != "eip155:1" || got.To != signTo || got.Value != "1000" ||
		got.Data != "0xa9059cbb" || got.Gas != 21000 || got.Description != "Swap" {
		t.Fatalf("request = %+v", got)
	}
}

func TestRequestSignRejectsAnUnknownKey(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}, Sign: true})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestSign({ chainId: "eip155:1", to: "`+signTo+`", valeu: "1" });
    return { title: "signed" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "not supported") {
		t.Fatalf("a typo was not refused: %+v", page)
	}
}

func TestRequestSignRejectsAnotherChain(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}, Sign: true})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestSign({ chainId: "eip155:8453", to: "`+signTo+`" });
    return { title: "signed" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "permissions") {
		t.Fatalf("another chain was not refused: %+v", page)
	}
}

func TestRequestSignErrorReachesThePlugin(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}, Sign: true})
	host := NewHost(HostDeps{
		Manifest: m,
		Sign: func(SignRequest) (SignResult, error) {
			return SignResult{}, errors.New("the user rejected the transaction")
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.requestSign({ chainId: "eip155:1", to: "`+signTo+`" });
    return { title: "signed" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "rejected") {
		t.Fatalf("the rejection did not reach the plugin: %+v", page)
	}
}

// A wait on the user is not the plugin's time: the watchdog is paused while
// requestSign blocks, so a slow approval does not restart the runtime.
func TestRequestSignPausesTheWatchdog(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}, Sign: true})
	host := NewHost(HostDeps{
		Manifest: m,
		Sign: func(SignRequest) (SignResult, error) {
			time.Sleep(300 * time.Millisecond)
			return SignResult{Hash: "0xabc"}, nil
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  const res = host.requestSign({ chainId: "eip155:1", to: "`+signTo+`" });
  return { title: res.hash };
}
`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	r.timeout = 80 * time.Millisecond
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "0xabc" {
		t.Fatalf("title = %q", page.Title)
	}
	if r.Restarts() != 0 {
		t.Fatalf("restarts = %d", r.Restarts())
	}
}
