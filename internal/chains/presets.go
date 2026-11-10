// Package chains is the registry of networks the app ships with: curated
// EVM, NEAR and Solana networks, and the reader each one
// needs. A network's ID is its CAIP-2 identifier, so it stays unique across
// families and is what settings store.
package chains

import "github.com/davirain-su/lintel/internal/chain"

// Ethereum is the default network.
var Ethereum = chain.Network{
	ID:       "eip155:1",
	Family:   chain.EVM,
	Name:     "Ethereum",
	Symbol:   "ETH",
	Decimals: 18,
	PriceID:  "ethereum",
	RPCURL:   "https://ethereum-rpc.publicnode.com",
	Explorer: "https://etherscan.io",
	ChainID:  1,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "0xdac17f958d2ee523a2206206994597c13d831ec7", Decimals: 6, PriceID: "tether"},
		{Symbol: "DAI", Name: "Dai Stablecoin", Address: "0x6b175474e89094c44da98b954eedeac495271d0f", Decimals: 18, PriceID: "dai"},
		{Symbol: "WETH", Name: "Wrapped Ether", Address: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", Decimals: 18, PriceID: "weth"},
	},
}

// Base is the second EVM preset, for the L2 the design names as an example.
var Base = chain.Network{
	ID:       "eip155:8453",
	Family:   chain.EVM,
	Name:     "Base",
	Symbol:   "ETH",
	Decimals: 18,
	PriceID:  "ethereum",
	RPCURL:   "https://base-rpc.publicnode.com",
	Explorer: "https://basescan.org",
	ChainID:  8453,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "DAI", Name: "Dai Stablecoin", Address: "0x50c5725949a6f0c72e6c4a641f24049a917db0cb", Decimals: 18, PriceID: "dai"},
		{Symbol: "WETH", Name: "Wrapped Ether", Address: "0x4200000000000000000000000000000000000006", Decimals: 18, PriceID: "weth"},
		{Symbol: "cbETH", Name: "Coinbase Wrapped Staked ETH", Address: "0x2ae3f1ec7f1f5012cfeab0185bfc7aa3cf0dec22", Decimals: 18, PriceID: "coinbase-wrapped-staked-eth"},
	},
}

// Arbitrum is Arbitrum One, an Ethereum L2 with ETH as its native coin.
var Arbitrum = chain.Network{
	ID: "eip155:42161", Family: chain.EVM, Name: "Arbitrum One", Symbol: "ETH", Decimals: 18,
	PriceID: "ethereum", RPCURL: "https://arbitrum-one-rpc.publicnode.com", Explorer: "https://arbiscan.io", ChainID: 42161,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0xaf88d065e77c8cc2239327c5edb3a432268e5831", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9", Decimals: 6, PriceID: "tether"},
	},
}

// Optimism is OP Mainnet, an Ethereum L2 with ETH as its native coin.
var Optimism = chain.Network{
	ID: "eip155:10", Family: chain.EVM, Name: "Optimism", Symbol: "ETH", Decimals: 18,
	PriceID: "ethereum", RPCURL: "https://optimism-rpc.publicnode.com", Explorer: "https://optimistic.etherscan.io", ChainID: 10,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0x0b2c639c533813f4aa9d7837caf62653d097ff85", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "0x94b008aa00579c1307b0ef2c499ad98a8ce58e58", Decimals: 6, PriceID: "tether"},
	},
}

// Polygon is the Polygon PoS network, whose native coin is POL.
var Polygon = chain.Network{
	ID: "eip155:137", Family: chain.EVM, Name: "Polygon", Symbol: "POL", Decimals: 18,
	PriceID: "polygon-ecosystem-token", RPCURL: "https://polygon-bor-rpc.publicnode.com", Explorer: "https://polygonscan.com", ChainID: 137,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "0xc2132d05d31c914a87c6611c10748aeb04b58e8f", Decimals: 6, PriceID: "tether"},
	},
}

// BSC is BNB Smart Chain, whose native coin is BNB.
var BSC = chain.Network{
	ID: "eip155:56", Family: chain.EVM, Name: "BNB Smart Chain", Symbol: "BNB", Decimals: 18,
	PriceID: "binancecoin", RPCURL: "https://bsc-rpc.publicnode.com", Explorer: "https://bscscan.com", ChainID: 56,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "0x55d398326f99059ff775485246999027b3197955", Decimals: 18, PriceID: "tether"},
	},
}

// Avalanche is Avalanche C-Chain, whose native coin is AVAX.
var Avalanche = chain.Network{
	ID: "eip155:43114", Family: chain.EVM, Name: "Avalanche C-Chain", Symbol: "AVAX", Decimals: 18,
	PriceID: "avalanche-2", RPCURL: "https://avalanche-c-chain-rpc.publicnode.com", Explorer: "https://snowscan.xyz", ChainID: 43114,
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "0xb97ef9ef8734c71904d8002f8b6bc66dd9c48a6e", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "0x9702230a8ea53601f5cd2dc00fdbc13d4df4a8c7", Decimals: 6, PriceID: "tether"},
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
	PriceID:   "near",
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
	PriceID:   "near",
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
	PriceID:  "solana",
	// publicnode answers getBalance and getSlot but treats
	// getTokenAccountsByOwner as an indexed request that needs a paid
	// token, so token balances show a warning until the user sets an
	// endpoint that serves them (Helius, QuickNode, a local validator).
	RPCURL:   "https://solana-rpc.publicnode.com",
	Explorer: "https://solscan.io",
	Genesis:  "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d",
	Tokens: []chain.Token{
		{Symbol: "USDC", Name: "USD Coin", Address: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Decimals: 6, PriceID: "usd-coin"},
		{Symbol: "USDT", Name: "Tether USD", Address: "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", Decimals: 6, PriceID: "tether"},
		{Symbol: "JUP", Name: "Jupiter", Address: "JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN", Decimals: 6, PriceID: "jupiter-exchange-solana"},
		{Symbol: "BONK", Name: "Bonk", Address: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263", Decimals: 5, PriceID: "bonk"},
	},
}
