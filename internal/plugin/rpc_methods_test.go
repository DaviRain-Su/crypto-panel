package plugin

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/davirain-su/lintel/internal/chains"
)

func TestRPCFamilyAllowlistAndDefaultDeny(t *testing.T) {
	for _, tc := range []struct {
		chain, method string
		allowed       bool
	}{
		{"eip155:8453", "eth_gasPrice", true}, {"eip155:1", "eth_blockNumber", true},
		{"near:testnet", "query", true}, {"near:mainnet", "block", true},
		{"eip155:1", "eth_sign", false}, {"eip155:1", "eth_sendRawTransaction", false},
		{"near:testnet", "broadcast_tx_commit", false}, {"near:testnet", "tx", false},
		{"near:testnet", "eth_gasPrice", false},
		{"eip155:1", "query", false}, {"eip155:1", "wallet_switchEthereumChain", false},
		{"eip155:1", "brandNewUnknownMethod", false}, {"eip155:1", "ETH_GASPRICE", false},
	} {
		t.Run(tc.chain+"/"+tc.method, func(t *testing.T) {
			manifest := testManifest(t, Permissions{Chains: []string{tc.chain}})
			called := false
			host := NewHost(HostDeps{Manifest: manifest, RPC: func(string, string, any) (json.RawMessage, error) { called = true; return json.RawMessage(`"ok"`), nil }})
			runner, err := NewRunner(manifest, []byte(fmt.Sprintf(`export function page(){try{host.rpc(%q,%q);return {title:"allowed"};}catch(e){return {title:"denied"};}}`, tc.chain, tc.method)), host, "en-US")
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			page, err := runner.Page("home")
			if err != nil {
				t.Fatal(err)
			}
			if called != tc.allowed || (page.Title == "allowed") != tc.allowed {
				t.Fatalf("family 边界失败: called=%v page=%+v", called, page)
			}
		})
	}
	for _, network := range chains.All() {
		if string(network.Family) == "solana" {
			if checkRPCFamily(string(network.Family), "getBalance") != nil || checkRPCFamily(string(network.Family), "eth_getBalance") == nil {
				t.Fatal("Solana family 边界失败")
			}
		}
	}
	methods := RPCMethods()
	methods["evm"][0] = "eth_sign"
	delete(methods, "near")
	if checkRPCFamily("evm", "eth_sign") == nil || checkRPCFamily("near", "query") != nil {
		t.Fatal("catalog 副本修改了宿主策略")
	}
}
