package chains

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chain/evm"
	"github.com/davirain-su/lintel/internal/chain/near"
	"github.com/davirain-su/lintel/internal/chain/solana"
	"github.com/davirain-su/lintel/internal/wallet"
)

var live = flag.Bool("live", false, "hit the public endpoints in TestLiveReaders")

// TestPresetsAreComplete checks every preset names a family, a coin, an
// endpoint and a pin, so a reader can be built for it.
func TestPresetsAreComplete(t *testing.T) {
	for _, n := range All() {
		if n.ID == "" || n.Name == "" || n.Symbol == "" || n.RPCURL == "" {
			t.Errorf("incomplete preset: %+v", n)
		}
		// CAIP-2 names the EVM namespace eip155, not evm.
		namespace := "eip155"
		switch n.Family {
		case chain.Solana:
			namespace = "solana"
		case chain.Near:
			namespace = "near"
		}
		if !strings.HasPrefix(n.ID, namespace+":") {
			t.Errorf("%s: id %q does not start with %q", n.Name, n.ID, namespace)
		}
		switch n.Family {
		case chain.EVM:
			if n.ChainID == 0 || n.Genesis != "" {
				t.Errorf("%s: EVM pins by chain id, got %+v", n.Name, n)
			}
		case chain.Solana:
			if n.Genesis == "" || n.ChainID != 0 {
				t.Errorf("%s: Solana pins by genesis, got %+v", n.Name, n)
			}
		case chain.Near:
			// NEAR has no numeric chain id; the RPC's chain_id string names
			// the network and is checked when the client starts.
			if n.ChainName == "" || n.ChainID != 0 {
				t.Errorf("%s: NEAR pins by chain name, got %+v", n.Name, n)
			}
		default:
			t.Errorf("%s: unknown family %q", n.Name, n.Family)
		}
		if n.Decimals <= 0 && n.Symbol != "" {
			t.Errorf("%s: decimals = %d", n.Name, n.Decimals)
		}
		for _, token := range n.Tokens {
			if token.Symbol == "" || token.Address == "" {
				t.Errorf("%s: incomplete token %+v", n.Name, token)
			}
		}
	}
}

// TestTokenAddresses pins the default list to the tokens' published forms:
// a typo in one address would watch the wrong contract.
func TestTokenAddresses(t *testing.T) {
	knownEVM := map[string]string{
		// Ethereum
		"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
		"0xdac17f958d2ee523a2206206994597c13d831ec7": "0xdAC17F958D2ee523a2206206994597C13D831ec7", // USDT
		"0x6b175474e89094c44da98b954eedeac495271d0f": "0x6B175474E89094C44Da98b954EedeAC495271d0F", // DAI
		"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", // WETH
		// Base
		"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913": "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", // USDC
		"0x50c5725949a6f0c72e6c4a641f24049a917db0cb": "0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb", // DAI
		"0x4200000000000000000000000000000000000006": "0x4200000000000000000000000000000000000006", // WETH
		"0x2ae3f1ec7f1f5012cfeab0185bfc7aa3cf0dec22": "0x2Ae3F1Ec7F1F5012CFEab0185bfc7aa3cf0DEc22", // cbETH
		// Arbitrum One
		"0xaf88d065e77c8cc2239327c5edb3a432268e5831": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831", // USDC
		"0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9": "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", // USDT
		// Optimism
		"0x0b2c639c533813f4aa9d7837caf62653d097ff85": "0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85", // USDC
		"0x94b008aa00579c1307b0ef2c499ad98a8ce58e58": "0x94b008aA00579c1307B0EF2c499aD98a8ce58e58", // USDT
		// Polygon
		"0x3c499c542cef5e3811e1192ce70d8cc03d5c3359": "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", // USDC
		"0xc2132d05d31c914a87c6611c10748aeb04b58e8f": "0xc2132D05D31c914a87C6611C10748AEb04B58e8F", // USDT
		// BNB Smart Chain
		"0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d": "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d", // USDC
		"0x55d398326f99059ff775485246999027b3197955": "0x55d398326f99059fF775485246999027B3197955", // USDT
		// Avalanche C-Chain
		"0xb97ef9ef8734c71904d8002f8b6bc66dd9c48a6e": "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E", // USDC
		"0x9702230a8ea53601f5cd2dc00fdbc13d4df4a8c7": "0x9702230A8Ea53601f5cD2dc00fDBc13d4dF4A8c7", // USDT
	}
	knownSolana := map[string]bool{
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v": true, // USDC
		"Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB": true, // USDT
		"JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN":  true, // JUP
		"DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263": true, // BONK
	}
	for _, n := range All() {
		for _, token := range n.Tokens {
			switch n.Family {
			case chain.EVM:
				want, ok := knownEVM[token.Address]
				if !ok {
					t.Errorf("%s: %s is not a pinned address", n.Name, token.Symbol)
					continue
				}
				if got := wallet.Checksum(token.Address); got != want {
					t.Errorf("%s %s: checksum %s, want %s", n.Name, token.Symbol, got, want)
				}
			case chain.Solana:
				if !knownSolana[token.Address] {
					t.Errorf("%s: %s is not a pinned mint", n.Name, token.Symbol)
				}
				if _, err := wallet.Normalize(wallet.Solana, token.Address); err != nil {
					t.Errorf("%s %s: %v", n.Name, token.Symbol, err)
				}
			}
		}
	}
}

func TestByID(t *testing.T) {
	for _, n := range All() {
		got, ok := ByID(n.ID)
		if !ok || got.ID != n.ID {
			t.Errorf("ByID(%q) = %+v, %v", n.ID, got, ok)
		}
	}
	if _, ok := ByID("eip155:999"); ok {
		t.Error("ByID(eip155:999) must not exist")
	}
}

func TestNetworksOf(t *testing.T) {
	evm := NetworksOf(chain.EVM)
	want := []chain.Network{Ethereum, Base, Arbitrum, Optimism, Polygon, BSC, Avalanche}
	if len(evm) != len(want) {
		t.Fatalf("EVM networks = %+v", evm)
	}
	for i, network := range want {
		if evm[i].ID != network.ID || evm[i].ChainID != network.ChainID {
			t.Errorf("EVM network %d = %+v, want %+v", i, evm[i], network)
		}
		if network.Decimals != 18 {
			t.Errorf("%s native decimals = %d, want 18", network.Name, network.Decimals)
		}
	}
	sol := NetworksOf(chain.Solana)
	if len(sol) != 1 || sol[0].ID != SolanaMainnet.ID {
		t.Fatalf("Solana networks = %+v", sol)
	}
	near := NetworksOf(chain.Near)
	if len(near) != 2 || near[0].ID != NearTestnet.ID || near[1].ID != NearMainnet.ID {
		t.Fatalf("NEAR networks = %+v", near)
	}
	if len(Families()) != 3 {
		t.Fatalf("families = %v", Families())
	}
}

func TestNewReader(t *testing.T) {
	reader, err := NewReader(Ethereum, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reader.(*evm.Client); !ok {
		t.Fatalf("Ethereum reader = %T", reader)
	}
	reader, err = NewReader(SolanaMainnet, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reader.(*solana.Client); !ok {
		t.Fatalf("Solana reader = %T", reader)
	}
	reader, err = NewReader(NearTestnet, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reader.(*near.Client); !ok {
		t.Fatalf("NEAR reader = %T", reader)
	}
	if _, err := NewReader(chain.Network{Family: "cosmos"}, ""); err == nil {
		t.Error("an unknown family must fail")
	}
}

// TestLiveReaders walks the whole path the app takes — preset, reader,
// pinned endpoint, typed read — against the real networks. It is opt-in:
// go test ./internal/chains -run TestLiveReaders -live.
func TestLiveReaders(t *testing.T) {
	if !*live {
		t.Skip("pass -live to hit the public endpoints")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	eth, err := NewReader(Ethereum, "")
	if err != nil {
		t.Fatal(err)
	}
	const vitalik = "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	acct, err := eth.Account(ctx, vitalik)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Native.Sign() <= 0 {
		t.Fatalf("Ethereum native = %s", acct.Native)
	}
	block, err := eth.Block(ctx)
	if err != nil || block == 0 {
		t.Fatalf("Ethereum block = %d, %v", block, err)
	}
	t.Logf("Ethereum: %s wei, block %d, %d tokens", acct.Native, block, len(acct.Tokens))

	sol, err := NewReader(SolanaMainnet, "")
	if err != nil {
		t.Fatal(err)
	}
	const binance = "5tzFkiKscXHK5ZXCGbXZxdw7gTjjD1mBwuoFbhUvuAi9"
	solAcct, err := sol.Account(ctx, binance)
	if err != nil {
		t.Fatal(err)
	}
	if solAcct.Native.Sign() <= 0 {
		t.Fatalf("Solana native = %s", solAcct.Native)
	}
	slot, err := sol.Block(ctx)
	if err != nil || slot == 0 {
		t.Fatalf("Solana slot = %d, %v", slot, err)
	}
	t.Logf("Solana: %s lamports, slot %d, %d tokens, %d warnings",
		solAcct.Native, slot, len(solAcct.Tokens), len(solAcct.Warnings))
	for _, w := range solAcct.Warnings {
		t.Logf("  warning: %s", w)
	}

	nearReader, err := NewReader(NearTestnet, "")
	if err != nil {
		t.Fatal(err)
	}
	nearAcct, err := nearReader.Account(ctx, "testnet")
	if err != nil {
		t.Fatal(err)
	}
	if nearAcct.Native.Sign() <= 0 {
		t.Fatalf("NEAR native = %s", nearAcct.Native)
	}
	height, err := nearReader.Block(ctx)
	if err != nil || height == 0 {
		t.Fatalf("NEAR height = %d, %v", height, err)
	}
	t.Logf("NEAR testnet: %s yoctoNEAR, height %d", nearAcct.Native, height)
}
