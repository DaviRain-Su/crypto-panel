package chains

import (
	"fmt"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chain/evm"
	"github.com/davirain-su/lintel/internal/chain/near"
	"github.com/davirain-su/lintel/internal/chain/solana"
)

// All lists the presets, in the order the settings offer them.
func All() []chain.Network {
	return []chain.Network{Ethereum, Base, NearTestnet, NearMainnet, SolanaMainnet}
}

// Default is the network a fresh install watches.
func Default() chain.Network { return Ethereum }

// ByID finds a preset by its CAIP-2 identifier.
func ByID(id string) (chain.Network, bool) {
	for _, n := range All() {
		if n.ID == id {
			return n, true
		}
	}
	return chain.Network{}, false
}

// Families lists the chain families the app speaks, in display order.
func Families() []chain.Family {
	return []chain.Family{chain.EVM, chain.Near, chain.Solana}
}

// NetworksOf lists the presets of one family.
func NetworksOf(family chain.Family) []chain.Network {
	out := make([]chain.Network, 0, len(All()))
	for _, n := range All() {
		if n.Family == family {
			out = append(out, n)
		}
	}
	return out
}

// NewReader builds the reader for network, at rpcURL or its default
// endpoint.
func NewReader(network chain.Network, rpcURL string) (chain.Reader, error) {
	switch network.Family {
	case chain.EVM:
		return evm.NewClient(network, rpcURL), nil
	case chain.Near:
		return near.NewClient(network, rpcURL), nil
	case chain.Solana:
		return solana.NewClient(network, rpcURL), nil
	}
	return nil, fmt.Errorf("chains: unknown family %q", network.Family)
}
