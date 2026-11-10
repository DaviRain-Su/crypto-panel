package app

import (
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/wallet"
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
		return nil, os.ErrNotExist
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
	if len(a.settings.NearWallet) != 1 || len(a.settings.SolanaAccounts()) != 1 {
		t.Fatalf("wallet chains: evm %d near %d solana %d", len(a.settings.Wallet), len(a.settings.NearWallet), len(a.settings.SolanaAccounts()))
	}
	if len(a.settings.Wallets) != 1 || a.settings.ActiveWallet != a.settings.Wallets[0].ID {
		t.Fatalf("container = %+v active %q", a.settings.Wallets, a.settings.ActiveWallet)
	}
	if a.walletErr != "" {
		t.Fatalf("err = %q", a.walletErr)
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Save these private keys", a.newKey, "Unlocked", "Lock all", "EVM", "NEAR", "Solana"} {
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

func TestCreateWalletGroupsThreeChains(t *testing.T) {
	a := newTestApp(t)
	a.createWalletAccount()
	if len(a.settings.Wallets) != 1 {
		t.Fatalf("wallets = %+v", a.settings.Wallets)
	}
	w := a.settings.Wallets[0]
	if w.EVM == nil || w.Near == nil || w.Solana == nil {
		t.Fatalf("wallet = %+v", w)
	}
	if w.EVM.Addr == w.Solana.Address || w.Near.AccountID == "" {
		t.Fatalf("accounts are not distinct: %+v", w)
	}
	data, err := os.ReadFile(a.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range a.shownKeys {
		if key.Key == "" || strings.Contains(string(data), key.Key) {
			t.Fatalf("settings hold a %s key", key.Chain)
		}
	}
}

func TestNetworkMenuFollowsTheSelectedChain(t *testing.T) {
	a := newTestApp(t)
	a.route = routeWallet
	a.createWalletAccount()
	a.hideKey()
	addr := wallet.Checksum(a.settings.Wallets[0].EVM.Addr)
	sol := a.settings.Wallets[0].Solana.Address
	page := func() *ui.Tester { return ui.NewTester(a.view, windowWidth, windowHeight) }
	if !page().HasText("Network") || !page().HasText("Ethereum") || !page().HasText(addr) {
		t.Fatalf("ethereum is not the open network: %q", page().Texts())
	}
	if page().HasText(sol) {
		t.Fatal("the Solana address is shown while Ethereum is selected")
	}
	a.setNetwork(chains.Base.ID)
	if a.network().Name != "Base" {
		t.Fatalf("network = %s", a.network().Name)
	}
	if !page().HasText("Base") || !page().HasText(addr) {
		t.Fatalf("Base does not show the same EVM account: %q", page().Texts())
	}
	if page().HasText(sol) {
		t.Fatal("the Solana address is shown on Base")
	}
}

func TestChainSwitchShowsThatAccountsAddress(t *testing.T) {
	a := newTestApp(t)
	a.route = routeWallet
	a.createWalletAccount()
	a.hideKey()
	w := a.settings.Wallets[0]
	page := func() *ui.Tester { return ui.NewTester(a.view, windowWidth, windowHeight) }
	if !page().HasText(wallet.Checksum(w.EVM.Addr)) {
		t.Fatal("the EVM address is not shown")
	}
	if page().HasText(w.Solana.Address) {
		t.Fatal("the Solana address is shown while an EVM network is selected")
	}
	a.showFamily(chain.Solana)
	if a.network().Family != chain.Solana {
		t.Fatalf("network = %s", a.network().Family)
	}
	if !page().HasText(w.Solana.Address) {
		t.Fatal("the Solana address is not shown")
	}
	if page().HasText(wallet.Checksum(w.EVM.Addr)) {
		t.Fatal("the EVM address is still shown on Solana")
	}
	a.showFamily(chain.Near)
	if !page().HasText(w.Near.AccountID) {
		t.Fatal("the NEAR account is not shown")
	}
}

func TestImportFillsTheOpenWalletsEmptyChain(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	a.hideKey()
	if len(a.settings.Wallets) != 1 || a.settings.Wallets[0].EVM != nil {
		t.Fatalf("setup = %+v", a.settings.Wallets)
	}
	a.draftKey = walletTestKey
	a.draftWalletLabel = "Desk"
	a.importWalletKey()
	if len(a.settings.Wallets) != 1 {
		t.Fatalf("import created another wallet: %+v", a.settings.Wallets)
	}
	w := a.settings.Wallets[0]
	if w.EVM == nil || w.EVM.Addr != walletTestAddr || w.EVM.Label != "Desk" {
		t.Fatalf("evm = %+v", w.EVM)
	}
	if w.Near == nil {
		t.Fatal("the NEAR account was dropped")
	}
}

func TestImportOfASecondKeyBecomesItsOwnWallet(t *testing.T) {
	a := newTestApp(t)
	a.createWalletAccount()
	a.hideKey()
	first := a.settings.Wallets[0]
	a.draftKey = walletTestKey
	a.draftWalletLabel = "Imported"
	a.importWalletKey()
	if len(a.settings.Wallets) != 2 {
		t.Fatalf("wallets = %d", len(a.settings.Wallets))
	}
	if a.settings.Wallets[0].EVM.Addr != first.EVM.Addr {
		t.Fatal("the open wallet's EVM account was replaced")
	}
	second := a.settings.Wallets[1]
	if second.EVM == nil || second.EVM.Addr != walletTestAddr || second.Near != nil || second.Solana != nil {
		t.Fatalf("second = %+v", second)
	}
	if a.settings.ActiveWallet != second.ID {
		t.Fatalf("active = %q, want %q", a.settings.ActiveWallet, second.ID)
	}
}

func TestLegacyAccountsBecomeSeparateWallets(t *testing.T) {
	a := newTestApp(t)
	evm, err := a.keyring.Import(walletTestKey, "Trading")
	if err != nil {
		t.Fatal(err)
	}
	near, err := a.nearKeyring.Create("Savings")
	if err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	s.Wallet = []wallet.Account{evm}
	s.NearWallet = []wallet.NearAccount{near}
	s = s.Validate()
	if len(s.Wallets) != 2 {
		t.Fatalf("wallets = %+v", s.Wallets)
	}
	if s.Wallets[0].Name != "Trading" || s.Wallets[0].EVM == nil || s.Wallets[0].Near != nil {
		t.Fatalf("first = %+v", s.Wallets[0])
	}
	if s.Wallets[1].Name != "Savings" || s.Wallets[1].Near == nil || s.Wallets[1].EVM != nil {
		t.Fatalf("second = %+v", s.Wallets[1])
	}
	if len(s.Wallet) != 1 || len(s.NearWallet) != 1 {
		t.Fatalf("projections evm %+v near %+v", s.Wallet, s.NearWallet)
	}
}

func TestSolanaImportUsesTheOpenWallet(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	a.hideKey()
	made, err := a.solanaKeyring.Create("")
	if err != nil {
		t.Fatal(err)
	}
	key, ok := a.solanaKeyring.Key(made.Address)
	if !ok {
		t.Fatal("the new solana key is not unlocked")
	}
	secret := wallet.SolanaSecretString(key)
	if err := a.solanaKeyring.Remove(made.Address); err != nil {
		t.Fatal(err)
	}
	a.showFamily(chain.Solana)
	a.draftKey = secret
	a.importWalletKey()
	w := a.settings.Wallets[0]
	if w.Near == nil || w.Solana == nil || w.Solana.Address != made.Address {
		t.Fatalf("wallet = %+v", w)
	}
	if strings.Contains(a.walletNotice, secret) {
		t.Fatal("the notice includes the secret")
	}
}
