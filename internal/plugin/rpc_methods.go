package plugin

import "errors"

// 只列出不提交持久链状态的查询；创建过滤器、签名、广播和钱包控制均不列入。
// 参数仍由各链 broker 与节点按其方法协议验证，不做跨 family 转译。
var rpcQueryMethods = map[string][]string{
	"evm": {
		"eth_blockNumber", "eth_chainId", "eth_gasPrice", "eth_maxPriorityFeePerGas", "eth_feeHistory",
		"eth_getBalance", "eth_getTransactionCount", "eth_getCode", "eth_getStorageAt", "eth_call", "eth_estimateGas",
		"eth_getBlockByNumber", "eth_getBlockByHash", "eth_getBlockTransactionCountByNumber", "eth_getBlockTransactionCountByHash",
		"eth_getTransactionByHash", "eth_getTransactionByBlockHashAndIndex", "eth_getTransactionByBlockNumberAndIndex", "eth_getTransactionReceipt",
		"eth_getLogs", "eth_getProof", "eth_syncing", "net_version",
	},
	"near": {
		"status", "query", "block", "chunk", "gas_price", "validators",
		"EXPERIMENTAL_protocol_config", "EXPERIMENTAL_changes", "EXPERIMENTAL_changes_in_block",
		"EXPERIMENTAL_tx_status", "light_client_proof", "next_light_client_block",
	},
	"solana": {
		"getBalance", "getAccountInfo", "getMultipleAccounts", "getProgramAccounts", "getTokenAccountBalance", "getTokenAccountsByOwner",
		"getTokenSupply", "getSupply", "getLatestBlockhash", "isBlockhashValid", "getFeeForMessage", "getSignatureStatuses",
		"getSignaturesForAddress", "getTransaction", "getSlot", "getBlock", "getBlockHeight", "getEpochInfo", "getHealth", "getVersion",
	},
}

// RPCMethods 返回可用于 catalog 的防御性副本，与执行规则共用允许表。
func RPCMethods() map[string][]string {
	out := make(map[string][]string, len(rpcQueryMethods))
	for family, methods := range rpcQueryMethods {
		out[family] = append([]string(nil), methods...)
	}
	return out
}

func checkRPCFamily(family, method string) error {
	for _, allowed := range rpcQueryMethods[family] {
		if method == allowed {
			return nil
		}
	}
	return rpcDenied(method)
}

// checkRPCMethod 保留旧调用兼容性；真正的 host.rpc 额外验证链 family。
func checkRPCMethod(method string) error {
	for family := range rpcQueryMethods {
		if checkRPCFamily(family, method) == nil {
			return nil
		}
	}
	return rpcDenied(method)
}
func rpcDenied(method string) error {
	// 不回显任意方法输入，错误仍保留旧安全断言使用的稳定语句。
	return errors.New("rpc: method is refused: plugins read the chain, they do not sign or send; query allowlist required")
}
