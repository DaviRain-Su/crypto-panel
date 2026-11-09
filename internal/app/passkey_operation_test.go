package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/wallet"
	"github.com/davirain-su/crypto-panel/internal/webauthn"
)

type hookedPasskeyCeremony struct {
	softCeremony
	before func(context.Context, webauthn.CeremonyRequest) error
	after  func(*webauthn.CeremonyResult)
}

func (c *hookedPasskeyCeremony) run(ctx context.Context, req webauthn.CeremonyRequest) (webauthn.CeremonyResult, error) {
	if c.before != nil {
		if err := c.before(ctx, req); err != nil {
			return webauthn.CeremonyResult{}, err
		}
	}
	res, err := c.softCeremony.run(ctx, req)
	if err == nil && c.after != nil {
		c.after(&res)
	}
	return res, err
}

func hookedPasskeyFactory(auth *webauthn.SoftAuthenticator, before func(context.Context, webauthn.CeremonyRequest) error, after func(*webauthn.CeremonyResult)) func(func(string) error) (passkeyCeremony, error) {
	return func(func(string) error) (passkeyCeremony, error) {
		return &hookedPasskeyCeremony{
			softCeremony: softCeremony{auth: auth, pageOrigin: "http://localhost:1234"},
			before:       before, after: after,
		}, nil
	}
}

type passkeyRPCReader struct {
	chain.Reader
	call func(context.Context, string, any) (json.RawMessage, error)
}

func (r *passkeyRPCReader) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return r.call(ctx, method, params)
}

func TestSendPasskeyNearRechecksStateAfterBrowserWait(t *testing.T) {
	cases := []struct {
		name   string
		change func(*App, wallet.NearAccount)
	}{
		{"lock all", func(a *App, _ wallet.NearAccount) { a.lockAll() }},
		{"lock relayer", func(a *App, r wallet.NearAccount) { a.lockNearAccount(r.AccountID) }},
		{"lock then unlock", func(a *App, r wallet.NearAccount) { a.lockNearAccount(r.AccountID); a.finishNearUnlock(r.AccountID) }},
		{"remove relayer", func(a *App, r wallet.NearAccount) { a.removeNearAccount(r.AccountID) }},
		{"forget passkey", func(a *App, _ wallet.NearAccount) { a.removePasskey() }},
		{"credential changed", func(a *App, _ wallet.NearAccount) { a.settings.Passkey.ID = "AQ" }},
		{"public key changed", func(a *App, _ wallet.NearAccount) {
			a.settings.Passkey.PublicKey = softPasskey(t, newSoftAuthenticator(t)).PublicKey
		}},
		{"relayer key changed", func(a *App, _ wallet.NearAccount) { a.settings.NearWallet[0].PublicKey = "ed25519:different" }},
		{"account changed", func(a *App, _ wallet.NearAccount) {
			a.settings.Passkey.NEARAccount = "0s0000000000000000000000000000000000000000"
		}},
		{"network changed", func(a *App, _ wallet.NearAccount) { a.settings.NetworkID = chains.NearTestnet.ID }},
		{"network changed back", func(a *App, _ wallet.NearAccount) {
			a.setNetwork(chains.NearTestnet.ID)
			a.setNetwork(chains.NearMainnet.ID)
		}},
		{"RPC changed", func(a *App, _ wallet.NearAccount) { a.settings.RPCURL = "https://other.example" }},
		{"quitting", func(a *App, _ wallet.NearAccount) { a.quitting = true }},
		{"keyring locked", func(a *App, r wallet.NearAccount) { a.nearKeyring.LockAccount(r.AccountID) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, rpc, auth, relayer := newPasskeyApp(t)
			// 避免本测试在 setNetwork 时触发无关的 factory 查询。
			a.settings.Passkey.EVMAddress = "0x1111111111111111111111111111111111111111"
			a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
			a.newCeremony = hookedPasskeyFactory(auth, func(context.Context, webauthn.CeremonyRequest) error {
				tc.change(a, relayer)
				return nil // A late valid assertion must still be rejected.
			}, nil)
			a.sendPasskeyNear()
			if rpc.broadcast != nil || a.passkeySendHash != "" {
				t.Fatal("a revoked browser wait broadcast a transaction")
			}
			if a.passkeySendBusy || a.passkeyOp != nil {
				t.Fatal("the revoked operation is still pending")
			}
		})
	}
}

func TestSendPasskeyNearCancelPropagatesToBrowserContext(t *testing.T) {
	a, rpc, auth, _ := newPasskeyApp(t)
	a.newCeremony = hookedPasskeyFactory(auth, func(ctx context.Context, _ webauthn.CeremonyRequest) error {
		a.lockAll() // The app window's close path uses the same cancellation.
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("browser context = %v", ctx.Err())
		}
		return ctx.Err()
	}, nil)
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.sendPasskeyNear()
	if rpc.broadcast != nil || a.passkeySendBusy {
		t.Fatal("canceling did not terminate the pending send")
	}
}

func TestSendPasskeyNearLockDuringRPCPreparationDoesNotSignOrBroadcast(t *testing.T) {
	a, rpc, _, _ := newPasskeyApp(t)
	base := a.reader
	a.reader = &passkeyRPCReader{Reader: base, call: func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method == "block" {
			a.lockAll()
		}
		return base.RPC(ctx, method, params)
	}}
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.sendPasskeyNear()
	if rpc.broadcast != nil || a.passkeySendHash != "" || a.passkeySendBusy {
		t.Fatal("a lock during preparation did not revoke the operation")
	}
}

func TestPasskeyPreflightFailureDoesNotRequestApprovalOrBrowser(t *testing.T) {
	for _, mode := range []string{"send", "deploy"} {
		for _, failure := range []string{"error", "wrong chain"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				a, rpc, auth, _ := newPasskeyApp(t)
				approvals, ceremonies := 0, 0
				a.approve = func(approvalRequest) bool { approvals++; return true }
				a.newCeremony = func(open func(string) error) (passkeyCeremony, error) {
					ceremonies++
					return softCeremonyFactory(auth)(open)
				}
				base := a.reader
				a.reader = &passkeyRPCReader{Reader: base, call: func(ctx context.Context, method string, params any) (json.RawMessage, error) {
					if method == "status" {
						if failure == "error" {
							return nil, errors.New("status unavailable")
						}
						return json.RawMessage(`{"chain_id":"testnet"}`), nil
					}
					return base.RPC(ctx, method, params)
				}}
				a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
				if mode == "send" {
					a.sendPasskeyNear()
				} else {
					a.deployPasskey()
				}
				if approvals != 0 || ceremonies != 0 || rpc.broadcast != nil {
					t.Fatalf("approval=%d, ceremonies=%d, broadcast=%v", approvals, ceremonies, rpc.broadcast != nil)
				}
				if a.passkeySendErr == "" || a.passkeySendBusy {
					t.Fatalf("error=%q, busy=%v", a.passkeySendErr, a.passkeySendBusy)
				}
			})
		}
	}
}

func TestSendPasskeyNearRevocationDuringApprovalDoesNotOpenBrowser(t *testing.T) {
	a, rpc, auth, _ := newPasskeyApp(t)
	ceremonies := 0
	a.approve = func(approvalRequest) bool { a.lockAll(); return true }
	a.newCeremony = func(open func(string) error) (passkeyCeremony, error) {
		ceremonies++
		return softCeremonyFactory(auth)(open)
	}
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.sendPasskeyNear()
	if ceremonies != 0 || rpc.broadcast != nil {
		t.Fatal("a revoked approval continued to the browser")
	}
}

func TestSendPasskeyNearOldCompletionCannotOverwriteNewOperation(t *testing.T) {
	a, rpc, auth, _ := newPasskeyApp(t)
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.newCeremony = hookedPasskeyFactory(auth, func(context.Context, webauthn.CeremonyRequest) error {
		a.cancelPasskeyOperations()
		a.newCeremony = softCeremonyFactory(auth)
		a.sendPasskeyNear()
		return nil
	}, nil)
	a.sendPasskeyNear()
	broadcasts := 0
	for _, method := range rpc.calls {
		if method == "broadcast_tx_commit" {
			broadcasts++
		}
	}
	if broadcasts != 1 || a.passkeySendHash == "" || a.passkeySendErr != "" || a.passkeySendBusy {
		t.Fatalf("broadcasts=%d hash=%q error=%q busy=%v", broadcasts, a.passkeySendHash, a.passkeySendErr, a.passkeySendBusy)
	}
}

func TestSendPasskeyNearRejectsAnotherReturnedCredential(t *testing.T) {
	a, rpc, auth, _ := newPasskeyApp(t)
	a.newCeremony = hookedPasskeyFactory(auth, nil, func(res *webauthn.CeremonyResult) { res.CredentialID = []byte("different") })
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.sendPasskeyNear()
	if rpc.broadcast != nil || !strings.Contains(a.passkeySendErr, "another credential") {
		t.Fatalf("broadcast=%v, error=%q", rpc.broadcast != nil, a.passkeySendErr)
	}
}

func TestPasskeyApprovalCancelReleasesPromptAndLock(t *testing.T) {
	a := newTestApp(t)
	a.approve = nil
	a.win = new(mygo.Window)
	op := a.beginPasskeyOperation(true)
	opened := make(chan struct{})
	closed := make(chan struct{})
	var late func(bool)
	a.openApproval = func(_ approvalRequest, done func(bool)) func() {
		late = done
		close(opened)
		return func() { close(closed) }
	}
	result := make(chan bool, 1)
	go func() {
		result <- a.approvePasskey(op, approvalRequest{pluginName: "Crypto Panel", sourceKind: "wallet"})
	}()
	<-opened
	// Hide the stand-in window so cancel does not call Invalidate.
	win := a.win
	a.win = nil
	a.cancelPasskeyOperations()
	a.win = win
	late(true) // A click that lands after cancel must not approve.
	if <-result {
		t.Fatal("a canceled passkey approval was accepted")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("a canceled passkey approval left its window open")
	}
	if !a.approvalMu.TryLock() {
		t.Fatal("a canceled passkey approval retained the shared approval lock")
	}
	defer a.approvalMu.Unlock()

	op = a.beginPasskeyOperation(true)
	queued := make(chan bool, 1)
	a.openApproval = func(_ approvalRequest, _ func(bool)) func() {
		t.Error("a canceled waiter opened a passkey approval")
		return func() {}
	}
	go func() {
		queued <- a.approvePasskey(op, approvalRequest{})
	}()
	a.win = nil
	a.cancelPasskeyOperations()
	a.win = win
	select {
	case ok := <-queued:
		if ok {
			t.Fatal("a canceled queue waiter succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("a canceled passkey approval remained queued behind another approval")
	}
}

func TestPasskeySendCancelDuringNativeApprovalDoesNotBroadcast(t *testing.T) {
	a, rpc, _, _ := newPasskeyApp(t)
	a.approve = nil
	a.win = new(mygo.Window)
	destroyed := false
	a.openApproval = func(_ approvalRequest, done func(bool)) func() {
		win := a.win
		a.win = nil
		a.cancelPasskeyOperations()
		a.win = win
		done(true)
		return func() { destroyed = true }
	}
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.sendPasskeyNear()
	if !destroyed {
		t.Fatal("cancel during native approval left the prompt open")
	}
	if rpc.broadcast != nil || a.passkeySendHash != "" {
		t.Fatal("a late approval after cancel signed or broadcast")
	}
	if !a.approvalMu.TryLock() {
		t.Fatal("a canceled passkey approval retained the shared approval lock")
	}
	a.approvalMu.Unlock()
}

func TestCreatePasskeyCanceledBeforeLateRegistrationIsNotStored(t *testing.T) {
	a := newTestApp(t)
	a.newCeremony = hookedPasskeyFactory(newSoftAuthenticator(t), func(context.Context, webauthn.CeremonyRequest) error {
		a.cancelPasskeyOperations()
		return nil
	}, nil)
	a.createPasskey()
	if a.settings.Passkey != nil || a.passkeyBusy || a.passkeyOp != nil {
		t.Fatal("a late registration revived a canceled operation")
	}
}
