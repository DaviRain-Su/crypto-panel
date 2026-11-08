package plugin

import (
	"errors"
	"strings"
	"testing"
)

const nearReceiver = "bob.testnet"

func TestRequestNearSignWithoutPermissionIsRefused(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestNearSign({ chainId: "near:testnet", receiver: "`+nearReceiver+`", actions: [{ type: "transfer", amount: "1" }] });
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
		t.Fatalf("requestNearSign was not refused: %+v", page)
	}
}

func TestRequestNearSignPassesTheIntent(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}, Sign: true})
	var got NearSignRequest
	host := NewHost(HostDeps{
		Manifest: m,
		NearSign: func(req NearSignRequest) (SignResult, error) {
			got = req
			return SignResult{Hash: "abc"}, nil
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  const res = host.requestNearSign({
    chainId: "near:testnet",
    receiver: "`+nearReceiver+`",
    description: "Donate",
    actions: [
      { type: "transfer", amount: "1000" },
      { type: "functionCall", method: "donate", args: "{}", gas: "30000000000000", deposit: "5" },
      { type: "addFunctionCallKey", publicKey: "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq", receiverId: "contract.testnet", methodNames: ["donate"], allowance: "100" },
    ],
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
	if page.Title != "abc" {
		t.Fatalf("title = %q", page.Title)
	}
	if got.ChainID != "near:testnet" || got.Receiver != nearReceiver || got.Description != "Donate" {
		t.Fatalf("request = %+v", got)
	}
	if len(got.Actions) != 3 {
		t.Fatalf("actions = %+v", got.Actions)
	}
	if got.Actions[0].Type != "transfer" || got.Actions[0].Amount != "1000" {
		t.Fatalf("transfer = %+v", got.Actions[0])
	}
	if got.Actions[1].Method != "donate" || got.Actions[1].Gas != "30000000000000" || got.Actions[1].Deposit != "5" {
		t.Fatalf("call = %+v", got.Actions[1])
	}
	if got.Actions[2].PublicKey == "" || got.Actions[2].ReceiverID != "contract.testnet" ||
		len(got.Actions[2].MethodNames) != 1 || got.Actions[2].Allowance != "100" {
		t.Fatalf("add key = %+v", got.Actions[2])
	}
}

func TestRequestNearSignRejectsAnotherChain(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}, Sign: true})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestNearSign({ chainId: "near:mainnet", receiver: "`+nearReceiver+`", actions: [{ type: "transfer", amount: "1" }] });
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

func TestRequestNearSignRejectsAnUnknownKey(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}, Sign: true})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestNearSign({ chainId: "near:testnet", receiver: "`+nearReceiver+`", actions: [{ type: "transfer", amount: "1", valeu: "1" }] });
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

func TestRequestNearSignRejectsAnUnknownAction(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}, Sign: true})
	r := testRunner(t, m, `
export function page() {
  try {
    host.requestNearSign({ chainId: "near:testnet", receiver: "`+nearReceiver+`", actions: [{ type: "stake", amount: "1" }] });
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
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "not an action") {
		t.Fatalf("a stake was not refused: %+v", page)
	}
}

func TestRequestNearSignErrorReachesThePlugin(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}, Sign: true})
	host := NewHost(HostDeps{
		Manifest: m,
		NearSign: func(NearSignRequest) (SignResult, error) {
			return SignResult{}, errors.New("the user rejected the transaction")
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.requestNearSign({ chainId: "near:testnet", receiver: "`+nearReceiver+`", actions: [{ type: "transfer", amount: "1" }] });
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
