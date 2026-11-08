// Package chains is the registry of networks the panel ships with: the EVM
// family (Ethereum, Base) and the Solana family, and the reader each one
// needs. A network's ID is its CAIP-2 identifier, so it stays unique across
// families and is what settings store.
package chains

import "github.com/davirain-su/crypto-panel/internal/chain"

// Ethereum is the default network.
var Ethereum = chain.Network{
	ID:       "eip155:1",
	Family:   chain.EVM,
	Name:     "Ethereum",
	Symbol:   "ETH",
	Decimals: 18,
	RPCURL:   "https://ethereum-rpc.publicnode.com",
	Explorer: "https://etherscan.io",
	ChainID:  1,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Decimals: 6},
		{Symbol: "USDT", Name: "Tether USD", Address: "0xdac17f958d2ee523a2206206994597c13d831ec7", Decimals: 6},
		{Symbol: "DAI", Name: "Dai Stablecoin", Address: "0x6b175474e89094c44da98b954eedeac495271d0f", Decimals: 18},
		{Symbol: "WETH", Name: "Wrapped Ether", Address: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", Decimals: 18},
	},
}

// Base is the second EVM preset, for the L2 the design names as an example.
var Base = chain.Network{
	ID:       "eip155:8453",
	Family:   chain.EVM,
	Name:     "Base",
	Symbol:   "ETH",
	Decimals: 18,
	RPCURL:   "https://base-rpc.publicnode.com",
	Explorer: "https://basescan.org",
	ChainID:  8453,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", Decimals: 6},
		{Symbol: "DAI", Name: "Dai Stablecoin", Address: "0x50c5725949a6f0c72e6c4a641f24049a917db0cb", Decimals: 18},
		{Symbol: "WETH", Name: "Wrapped Ether", Address: "0x4200000000000000000000000000000000000006", Decimals: 18},
		{Symbol: "cbETH", Name: "Coinbase Wrapped Staked ETH", Address: "0x2ae3f1ec7f1f5012cfeab0185bfc7aa3cf0dec22", Decimals: 18},
	},
}

// NearTestnet is the NEAR family's first network, the one the wallet creates
// accounts on and the hackathon demo uses.
var NearTestnet = chain.Network{
	ID:        "near:testnet",
	Family:    chain.Near,
	Name:      "NEAR Testnet",
	Symbol:    "NEAR",
	Decimals:  24,
	RPCURL:    "https://rpc.testnet.near.org",
	Explorer:  "https://testnet.nearblocks.io",
	ChainName: "testnet",
}

// NearMainnet is NEAR's mainnet, read-only until the user imports a funded
// key.
var NearMainnet = chain.Network{
	ID:        "near:mainnet",
	Family:    chain.Near,
	Name:      "NEAR",
	Symbol:    "NEAR",
	Decimals:  24,
	RPCURL:    "https://rpc.mainnet.near.org",
	Explorer:  "https://nearblocks.io",
	ChainName: "mainnet",
}

// SolanaMainnet is the Solana family's first network. Its ID's reference is
// the genesis hash, which is also what the endpoint is pinned to.
var SolanaMainnet = chain.Network{
	ID:       "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
	Family:   chain.Solana,
	Name:     "Solana",
	Symbol:   "SOL",
	Decimals: 9,
	// publicnode answers getBalance and getSlot but treats
	// getTokenAccountsByOwner as an indexed request that needs a paid
	// token, so token balances show a warning until the user sets an
	// endpoint that serves them (Helius, QuickNode, a local validator).
	RPCURL:   "https://solana-rpc.publicnode.com",
	Explorer: "https://solscan.io",
	Genesis:  "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d",
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Decimals: 6},
		{Symbol: "USDT", Name: "Tether USD", Address: "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", Decimals: 6},
		{Symbol: "JUP", Name: "Jupiter", Address: "JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN", Decimals: 6},
		{Symbol: "BONK", Name: "Bonk", Address: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263", Decimals: 5},
	},
}
