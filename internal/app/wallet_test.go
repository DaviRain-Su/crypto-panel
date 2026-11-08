package app

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// memStore is a SecretStore in memory, standing in for the OS keychain.
type memStore map[string][]byte

func (m memStore) Set(key string, value []byte) error {
	m[key] = append([]byte(nil), value...)
	return nil
}

func (m memStore) Get(key string) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, errors.New("no such secret")
	}
	return v, nil
}

func (m memStore) Delete(key string) error {
	delete(m, key)
	return nil
}

// The key and address of the web3.js documentation's example account.
const (
	walletTestKey  = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	walletTestAddr = "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23"
)

func TestWalletPageShowsAccounts(t *testing.T) {
	a := newTestApp(t)
	a.settings.Wallet = []wallet.Account{{Addr: viewAddr, Label: "Main"}}
	a.settings = a.settings.Validate()
	a.route = routeWallet
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Wallet", "1 ACCOUNT · 0 UNLOCKED", "Main", wallet.Checksum(viewAddr),
		"Locked", "Import a private key", "system keychain",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestWalletPageShowsBalances(t *testing.T) {
	a := newTestApp(t)
	a.settings.Wallet = []wallet.Account{{Addr: viewAddr, Label: "Main"}}
	a.settings = a.settings.Validate()
	a.route = routeWallet
	a.snap = &snapshot{accounts: []accountSnapshot{{
		addr:   viewAddr,
		native: big.NewInt(1_500_000_000_000_000_000),
	}}}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("1.5 ETH") {
		t.Errorf("the balance is not shown: %q", tt.Texts())
	}
}

func TestCreateWalletAccount(t *testing.T) {
	a := newTestApp(t)
	a.route = routeWallet
	a.createWalletAccount()
	if len(a.settings.Wallet) != 1 {
		t.Fatalf("wallet = %+v", a.settings.Wallet)
	}
	acc := a.settings.Wallet[0]
	if !a.keyring.Has(acc.Addr) || !a.keyring.Unlocked(acc.Addr) {
		t.Fatal("the new account is not in the keyring, unlocked")
	}
	if len(a.newKey) != 64 || a.newKeyAddr != acc.Addr {
		t.Fatalf("the key is not shown once: %q for %q", a.newKey, a.newKeyAddr)
	}
	if a.walletErr != "" {
		t.Fatalf("err = %q", a.walletErr)
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Save this private key", a.newKey, "Unlocked", "Lock all"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestImportWalletAccount(t *testing.T) {
	a := newTestApp(t)
	a.draftKey = walletTestKey
	a.draftWalletLabel = "Main"
	a.importWalletAccount()
	if len(a.settings.Wallet) != 1 {
		t.Fatalf("wallet = %+v", a.settings.Wallet)
	}
	acc := a.settings.Wallet[0]
	if acc.Addr != walletTestAddr || acc.Label != "Main" {
		t.Fatalf("account = %+v", acc)
	}
	if a.draftKey != "" || a.draftWalletLabel != "" {
		t.Fatal("the form was not cleared")
	}
	if a.newKey != "" {
		t.Fatal("an imported key is shown again")
	}
	if !a.keyring.Unlocked(acc.Addr) {
		t.Fatal("an imported account is not unlocked")
	}
}

func TestImportRejectsABadKey(t *testing.T) {
	a := newTestApp(t)
	a.draftKey = "nonsense"
	a.importWalletAccount()
	if len(a.settings.Wallet) != 0 {
		t.Fatalf("wallet = %+v", a.settings.Wallet)
	}
	if !strings.Contains(a.walletErr, "not a valid private key") {
		t.Fatalf("err = %q", a.walletErr)
	}
}

func TestImportRejectsADuplicate(t *testing.T) {
	a := newTestApp(t)
	a.draftKey = walletTestKey
	a.importWalletAccount()
	a.draftKey = walletTestKey
	a.importWalletAccount()
	if len(a.settings.Wallet) != 1 {
		t.Fatalf("wallet = %+v", a.settings.Wallet)
	}
	if !strings.Contains(a.walletErr, "already in the wallet") {
		t.Fatalf("err = %q", a.walletErr)
	}
}

func TestRemoveWalletAccount(t *testing.T) {
	a := newTestApp(t)
	a.draftKey = walletTestKey
	a.importWalletAccount()
	a.removeWalletAccount(walletTestAddr)
	if len(a.settings.Wallet) != 0 {
		t.Fatalf("wallet = %+v", a.settings.Wallet)
	}
	if a.keyring.Has(walletTestAddr) {
		t.Fatal("the keyring still holds the account")
	}
}

func TestLockAllForgetsTheKeys(t *testing.T) {
	a := newTestApp(t)
	a.createWalletAccount()
	a.lockAll()
	if a.keyring.Unlocked(a.settings.Wallet[0].Addr) {
		t.Fatal("the key is still in memory")
	}
	if a.newKey != "" {
		t.Fatal("the shown key outlived the lock")
	}
	if a.walletUnlocked() != 0 {
		t.Fatalf("unlocked = %d", a.walletUnlocked())
	}
}

func TestRevealKeyLeavesWithThePage(t *testing.T) {
	a := newTestApp(t)
	a.route = routeWallet
	a.draftKey = walletTestKey
	a.importWalletAccount()
	a.lockAll()
	a.revealKey(walletTestAddr)
	if a.revealedKey != walletTestKey || a.revealedAddr != walletTestAddr {
		t.Fatalf("revealed = %q for %q", a.revealedKey, a.revealedAddr)
	}
	a.goTo(routePortfolio)
	if a.revealedKey != "" || a.revealedAddr != "" {
		t.Fatal("a revealed key outlived the wallet page")
	}
}

func TestPortfolioIncludesWalletAccounts(t *testing.T) {
	a := newTestApp(t)
	a.draftKey = walletTestKey
	a.importWalletAccount()
	if got := a.settings.WatchedOf(chain.EVM); len(got) != 1 || got[0].Addr != walletTestAddr {
		t.Fatalf("watched = %+v", got)
	}
	if got := a.headline(); got != "Ethereum · 1 account" {
		t.Fatalf("headline = %q", got)
	}
	// The same address cannot also be added as a watch-only one.
	a.draftAddr = walletTestAddr
	a.addAccount()
	if len(a.settings.Accounts) != 0 {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if !strings.Contains(a.addErr, "already") {
		t.Fatalf("err = %q", a.addErr)
	}
}

func TestWalletAccountsAreNotWatchedTwice(t *testing.T) {
	s := DefaultSettings()
	s.Accounts = []wallet.Address{{Addr: walletTestAddr}}
	s.Wallet = []wallet.Account{{Addr: walletTestAddr}}
	s = s.Validate()
	if len(s.Accounts) != 0 || len(s.Wallet) != 1 {
		t.Fatalf("accounts = %+v, wallet = %+v", s.Accounts, s.Wallet)
	}
	if got := s.WatchedOf(chain.EVM); len(got) != 1 {
		t.Fatalf("watched = %+v", got)
	}
}
