// Package chain is the trusted chain layer's vocabulary: the two families
// the panel speaks (EVM networks and Solana), what a network is, and the
// balances a reader returns. The readers themselves live in the evm and
// solana packages, and internal/chains picks one for a network.
package chain

import (
	"context"
	"encoding/json"
	"math/big"
)

// Family is a kind of chain with its own address format, RPC dialect and
// token standard. EVM covers every eip155 network; Solana is the other
// family the panel speaks for now.
type Family string

const (
	EVM    Family = "evm"
	Solana Family = "solana"
)

// Network is one chain the panel can watch.
type Network struct {
	// ID is the CAIP-2 identifier: "eip155:1", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp".
	ID     string `json:"id"`
	Family Family `json:"family"`
	Name   string `json:"name"`
	// Symbol and Decimals describe the native coin: ETH at 18, SOL at 9.
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	RPCURL   string `json:"rpcUrl"`
	Explorer string `json:"explorer"`
	// ChainID is the EVM chain id; 0 for other families.
	ChainID uint64 `json:"chainId,omitempty"`
	// Genesis pins a Solana network: the endpoint must report this
	// getGenesisHash. Empty for EVM, whose chain id is the pin.
	Genesis string  `json:"genesis,omitempty"`
	Tokens  []Token `json:"tokens"`
}

// Token is a token the panel shows by default. Address is an ERC-20
// contract on EVM and an SPL mint on Solana.
type Token struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}

// DisplayFrac is how many fraction digits the panel shows for this token.
// Stablecoins read as money; nine-decimal tokens keep enough to see dust.
func (t Token) DisplayFrac() int {
	switch {
	case t.Decimals <= 0:
		return 0
	case t.Decimals >= 9:
		return 4
	case t.Decimals >= 4:
		return 2
	default:
		return t.Decimals
	}
}

// TokenBalance is one token's amount for one account.
type TokenBalance struct {
	Token  Token
	Amount *big.Int
}

// AccountBalances is what one address holds on one network.
type AccountBalances struct {
	Address  string
	Native   *big.Int
	Tokens   []TokenBalance
	Warnings []string
}

// Reader reads one network through its own RPC dialect. The app builds one
// for the selected network; plugins reach it only through host.rpc.
type Reader interface {
	// Account reads the native balance and the network's default tokens.
	Account(ctx context.Context, addr string) (AccountBalances, error)
	// Block returns the latest block or slot.
	Block(ctx context.Context) (uint64, error)
	// RPC makes one call through the broker, pinned, cached, limited and audited.
	RPC(ctx context.Context, method string, params any) (json.RawMessage, error)
}
