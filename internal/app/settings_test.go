package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chain/evm"
	"github.com/davirain-su/lintel/internal/chain/solana"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/wallet"
)

const (
	testAddr  = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	testSol   = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	testSolB  = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	testEVM2  = "0x27b1fdb04752bbc536007a920d24acb045561c26"
	badEVMAdd = "0x123"
)

func TestLoadSettingsMissingFile(t *testing.T) {
	s, err := LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.NetworkID != chains.Ethereum.ID || s.RefreshSeconds != DefaultRefreshSeconds {
		t.Fatalf("defaults = %+v", s)
	}
	if len(s.Accounts) != 0 {
		t.Fatalf("accounts = %v", s.Accounts)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	want := Settings{
		NetworkID:      chains.Base.ID,
		RPCURL:         "https://example.com/rpc",
		Language:       i18n.Chinese,
		RefreshSeconds: 120,
		Accounts: []wallet.Address{
			{Addr: testAddr, Label: "Main"},
			{Addr: testSol, Label: "Solana"},
		},
	}
	if err := SaveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.NetworkID != want.NetworkID || got.RPCURL != want.RPCURL ||
		got.Language != want.Language || got.RefreshSeconds != want.RefreshSeconds {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Accounts) != 2 || got.Accounts[0] != want.Accounts[0] || got.Accounts[1] != want.Accounts[1] {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
}

func TestLoadSettingsMigratesChainID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"chainId":8453,"accounts":[{"address":"`+testAddr+`"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.NetworkID != chains.Base.ID {
		t.Fatalf("network = %q, want %q", s.NetworkID, chains.Base.ID)
	}
	if s.ChainID != 0 {
		t.Fatalf("legacy chainId = %d, want it cleared", s.ChainID)
	}
	if len(s.Accounts) != 1 {
		t.Fatalf("accounts = %+v", s.Accounts)
	}
}

func TestLoadSettingsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err == nil {
		t.Fatal("want an error for a corrupt file")
	}
	if s.NetworkID != chains.Ethereum.ID {
		t.Fatalf("fallback = %+v", s)
	}
}

func TestValidate(t *testing.T) {
	s := Settings{
		NetworkID:      "eip155:999",
		RPCURL:         "ftp://example.com",
		Language:       "fr",
		RefreshSeconds: 1,
		Accounts: []wallet.Address{
			{Addr: strings.ToUpper(testAddr), Label: "  Main  "},
			{Addr: testAddr, Label: "duplicate"},
			{Addr: badEVMAdd, Label: "broken"},
			{Addr: testEVM2, Label: strings.Repeat("x", 100)},
			{Addr: testSol, Label: "Solana"},
			{Addr: testSol, Label: "duplicate Solana"},
		},
	}
	got := s.Validate()
	if got.NetworkID != chains.Ethereum.ID {
		t.Errorf("network = %q", got.NetworkID)
	}
	if got.RPCURL != "" {
		t.Errorf("rpc = %q, want empty", got.RPCURL)
	}
	if got.Language != i18n.System {
		t.Errorf("language = %q", got.Language)
	}
	if got.RefreshSeconds != DefaultRefreshSeconds {
		t.Errorf("refresh = %d", got.RefreshSeconds)
	}
	if len(got.Accounts) != 3 {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	if got.Accounts[0].Addr != testAddr || got.Accounts[0].Label != "Main" {
		t.Errorf("first = %+v", got.Accounts[0])
	}
	if n := len([]rune(got.Accounts[1].Label)); n != 40 {
		t.Errorf("label length = %d, want 40", n)
	}
	if got.Accounts[2].Addr != testSol {
		t.Errorf("third = %+v, want the Solana address", got.Accounts[2])
	}
}

func TestAccountsOf(t *testing.T) {
	s := DefaultSettings()
	s.Accounts = []wallet.Address{
		{Addr: testAddr},
		{Addr: testSol},
		{Addr: testEVM2},
		{Addr: testSolB},
	}
	evm := s.AccountsOf(chain.EVM)
	if len(evm) != 2 || evm[0].Addr != testAddr || evm[1].Addr != testEVM2 {
		t.Fatalf("EVM accounts = %+v", evm)
	}
	sol := s.AccountsOf(chain.Solana)
	if len(sol) != 2 || sol[0].Addr != testSol || sol[1].Addr != testSolB {
		t.Fatalf("Solana accounts = %+v", sol)
	}
}

func TestReader(t *testing.T) {
	s := DefaultSettings()
	reader, err := s.Reader()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reader.(*evm.Client)
	if !ok {
		t.Fatalf("reader = %T, want *evm.Client", reader)
	}
	if c.Endpoint().URL != chains.Ethereum.RPCURL || c.Endpoint().ChainID != chains.Ethereum.ChainID {
		t.Fatalf("default endpoint = %+v", c.Endpoint())
	}

	s.RPCURL = "https://example.com/rpc"
	reader, err = s.Reader()
	if err != nil {
		t.Fatal(err)
	}
	if c := reader.(*evm.Client); c.Endpoint().URL != s.RPCURL {
		t.Fatalf("custom endpoint = %+v", c.Endpoint())
	}

	s.NetworkID = chains.SolanaMainnet.ID
	reader, err = s.Reader()
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := reader.(*solana.Client)
	if !ok {
		t.Fatalf("reader = %T, want *solana.Client", reader)
	}
	if sc.Endpoint().URL != s.RPCURL || sc.Endpoint().Genesis != chains.SolanaMainnet.Genesis {
		t.Fatalf("solana endpoint = %+v", sc.Endpoint())
	}
}

func TestValidRPCURL(t *testing.T) {
	for _, good := range []string{
		"https://ethereum-rpc.publicnode.com",
		"http://localhost:8545",
		"https://example.com/rpc?key=1",
	} {
		if !ValidRPCURL(good) {
			t.Errorf("ValidRPCURL(%q) = false", good)
		}
	}
	for _, bad := range []string{
		"",
		"example.com",
		"ftp://example.com",
		"https://",
		"javascript:alert(1)",
	} {
		if ValidRPCURL(bad) {
			t.Errorf("ValidRPCURL(%q) = true", bad)
		}
	}
}
