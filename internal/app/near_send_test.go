package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chains"
)

func TestSendNearSignsAfterApproval(t *testing.T) {
	a, rpc, acc := newDappApp(t)
	var asked approvalRequest
	a.approve = func(req approvalRequest) bool { asked = req; return true }
	a.nearSendFrom = acc.AccountID
	a.nearSendTo = "bob.testnet"
	a.nearSendAmount = "1.5"
	a.sendNear()
	if rpc.broadcast == nil {
		t.Fatal("nothing was broadcast")
	}
	if asked.sourceKind != "wallet" {
		t.Fatalf("approval source = %q", asked.sourceKind)
	}
	if asked.near == nil || asked.near.ReceiverID != "bob.testnet" {
		t.Fatalf("approval = %+v", asked.near)
	}
	if got := asked.near.Actions[0].Amount.String(); got != "1500000000000000000000000" {
		t.Fatalf("amount = %s", got)
	}
	if a.nearSendHash == "" || a.nearSendNotice == "" {
		t.Fatalf("hash = %q, notice = %q", a.nearSendHash, a.nearSendNotice)
	}
	if a.nearSendTo != "" || a.nearSendAmount != "" {
		t.Fatalf("the form was not cleared: %q %q", a.nearSendTo, a.nearSendAmount)
	}
}

func TestSendNearRejected(t *testing.T) {
	a, rpc, acc := newDappApp(t)
	a.approve = func(approvalRequest) bool { return false }
	a.nearSendFrom = acc.AccountID
	a.nearSendTo = "bob.testnet"
	a.nearSendAmount = "1"
	a.sendNear()
	if rpc.broadcast != nil {
		t.Fatal("a rejected transfer was broadcast")
	}
	if !strings.Contains(a.nearSendErr, "rejected") {
		t.Fatalf("err = %q", a.nearSendErr)
	}
	if a.nearSendHash != "" {
		t.Fatal("a rejected transfer has a hash")
	}
}

func TestSendNearNeedsAnUnlockedAccount(t *testing.T) {
	a, rpc, acc := newDappApp(t)
	a.nearKeyring.LockAccount(acc.AccountID)
	a.nearSendFrom = acc.AccountID
	a.nearSendTo = "bob.testnet"
	a.nearSendAmount = "1"
	a.sendNear()
	if rpc.broadcast != nil {
		t.Fatal("a locked account signed")
	}
	if a.nearSendErr == "" {
		t.Fatal("the locked account was not reported")
	}
}

func TestSendNearNeedsANearNetwork(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	a.nearSendTo = "bob.testnet"
	a.nearSendAmount = "1"
	a.sendNear()
	if a.nearSendErr == "" {
		t.Fatal("a transfer on another network was accepted")
	}
}

func TestSendNearValidatesTheForm(t *testing.T) {
	a, rpc, acc := newDappApp(t)
	a.nearSendFrom = acc.AccountID
	cases := map[string]func(){
		"bad recipient": func() { a.nearSendTo, a.nearSendAmount = "not an account!", "1" },
		"bad amount":    func() { a.nearSendTo, a.nearSendAmount = "bob.testnet", "0" },
		"fraction":      func() { a.nearSendTo, a.nearSendAmount = "bob.testnet", "1.0000000000000000000000001" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			a.nearSendErr = ""
			change()
			a.sendNear()
			if a.nearSendErr == "" {
				t.Fatal("the form was accepted")
			}
		})
	}
	if rpc.broadcast != nil {
		t.Fatal("an invalid form was broadcast")
	}
}

func TestNearSendFormView(t *testing.T) {
	a, _, _ := newDappApp(t)
	a.route = routeWallet
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Send NEAR", "From", "Amount (NEAR)", "Recipient", "Review and send",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestNearExplorerURL(t *testing.T) {
	a, _, _ := newDappApp(t)
	if got := a.nearExplorerURL("abc123"); got != chains.NearTestnet.Explorer+"/txns/abc123" {
		t.Fatalf("url = %q", got)
	}
	if a.nearExplorerURL("") != "" {
		t.Fatal("an empty hash has a URL")
	}
}
