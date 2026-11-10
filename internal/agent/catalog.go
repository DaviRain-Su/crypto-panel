package agent

import (
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/version"
)

// Catalog 是当前构建的机器可读边界，管理能力取决于实际宿主。
func Catalog(managed bool) map[string]any {
	networks := []map[string]any{}
	chainIDs := []string{}
	for _, network := range chains.All() {
		networks = append(networks, map[string]any{"id": network.ID, "name": network.Name, "family": network.Family})
		chainIDs = append(chainIDs, network.ID)
	}
	schemas := applicationSchemas(chainIDs)
	functions := []map[string]any{}
	add := func(name, permission string, inputs, output any, contexts ...string) {
		if tuple, ok := inputs.(map[string]any); ok {
			if name == "host.fetch" {
				tuple["minItems"] = 1
			}
			if name == "host.rpc" {
				tuple["minItems"] = 2
			}
		}
		functions = append(functions, map[string]any{
			"name": name, "permission": permission, "inputSchema": inputs,
			"outputSchema": output, "contexts": contexts,
		})
	}
	key := schemaString(1, 256)
	args := func(items ...any) map[string]any {
		out := map[string]any{"type": "array", "maxItems": len(items), "minItems": len(items)}
		if len(items) > 0 {
			out["prefixItems"] = items
		}
		return out
	}
	both := []string{"ui", "apiRefresh"}
	add("host.setting", "declared setting", args(key), schemaString(0, 0), both...)
	add("host.kv.get", "storageKb", args(key), map[string]any{}, both...)
	add("host.kv.put", "storageKb", args(key, map[string]any{}), map[string]any{}, both...)
	add("host.kv.delete", "storageKb", args(key), map[string]any{}, both...)
	add("host.fetch", "network", args(schemaString(1, 0), schemaObject(map[string]any{
		"method": schemaEnum("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"),
		"headers": map[string]any{"type": "object", "properties": map[string]any{
			"accept": schemaString(0, 0), "content-type": schemaString(0, 0), "authorization": schemaString(0, 0),
		}, "additionalProperties": false}, "body": schemaString(0, 0),
	})), schemaObject(map[string]any{"status": map[string]any{"type": "integer"}, "ok": map[string]any{"type": "boolean"}, "headers": map[string]any{"type": "object"}, "body": schemaString(0, 0)}, "status", "ok", "headers", "body"), both...)
	add("host.rpc", "chains + rpcMethods", args(schemaEnum(chainIDs...), schemaString(1, 64), map[string]any{}), map[string]any{}, both...)
	decimalString := schemaString(1, 1024)
	decimals := map[string]any{"type": "integer", "minimum": float64(0), "maximum": float64(255)}
	add("host.units.parse", "none", args(decimalString, decimals), decimalString, both...)
	add("host.units.format", "none", args(decimalString, decimals), decimalString, both...)
	add("host.units.formatDisplay", "none", args(decimalString, decimals, decimals), decimalString, both...)
	add("host.near.view", "NEAR chain", args(schemaObject(map[string]any{
		"chainId": schemaEnum("near:mainnet", "near:testnet"), "contract": schemaString(2, 64),
		"method": schemaString(1, 128), "args": map[string]any{},
	}, "chainId", "contract", "method")), map[string]any{}, both...)
	add("host.hyperliquid.info", "network: api.hyperliquid.xyz or api.hyperliquid-testnet.xyz", args(schemaObject(map[string]any{
		"type":    schemaEnum("meta", "spotMeta", "metaAndAssetCtxs", "spotMetaAndAssetCtxs", "allMids", "l2Book", "candleSnapshot", "clearinghouseState", "spotClearinghouseState", "portfolio", "openOrders", "userFillsByTime"),
		"testnet": map[string]any{"type": "boolean"},
		"coin":    schemaString(1, 128), "user": schemaString(1, 128), "dex": schemaString(1, 128),
		"nSigFigs": map[string]any{"type": "integer", "minimum": float64(2), "maximum": float64(5)},
		"mantissa": map[string]any{"type": "integer", "minimum": float64(1)},
		"req":      map[string]any{"type": "object"}, "aggregateByTime": map[string]any{"type": "boolean"},
		"startTime": map[string]any{"type": "integer", "minimum": float64(0)},
		"endTime":   map[string]any{"type": "integer", "minimum": float64(0)},
	}, "type")), map[string]any{}, both...)
	intentsNetwork := "network: 1click.chaindefuser.com"
	add("host.intents.tokens", intentsNetwork, args(), schemaArray(schemaObject(map[string]any{
		"assetId": schemaString(1, 0), "symbol": schemaString(1, 0), "blockchain": schemaString(1, 0),
		"decimals": decimals, "price": map[string]any{"type": "number"}, "icon": schemaString(0, 0),
	}, "assetId", "symbol", "blockchain", "decimals"), 0), both...)
	quoteInput := schemaObject(map[string]any{
		"originAsset": schemaString(1, 512), "destinationAsset": schemaString(1, 512), "amount": decimalString,
		"recipient": schemaString(1, 512), "refundTo": schemaString(1, 512),
		"slippageBps": map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(1000)},
		"deadline":    schemaString(0, 64),
	}, "originAsset", "destinationAsset", "amount", "recipient", "refundTo")
	dryQuoteOutput := schemaObject(map[string]any{
		"amountIn": decimalString, "amountOut": decimalString, "minAmountOut": decimalString,
		"verified": map[string]any{"const": true}, "timestamp": schemaString(1, 64),
	}, "amountIn", "amountOut", "minAmountOut", "verified", "timestamp")
	// Return the complete trusted quote, including formatted amounts, deadline
	// and echoed signed request, rather than rejecting its extra read fields.
	dryQuoteOutput["additionalProperties"] = true
	add("host.intents.dryQuote", intentsNetwork+"; dry only", args(quoteInput), dryQuoteOutput, both...)
	statusOutput := schemaObject(map[string]any{
		"status":        schemaEnum("PENDING_DEPOSIT", "KNOWN_DEPOSIT_TX", "INCOMPLETE_DEPOSIT", "PROCESSING", "SUCCESS", "REFUNDED", "FAILED"),
		"correlationId": schemaString(0, 0), "updatedAt": schemaString(0, 0),
		"quoteResponse": map[string]any{"type": "object"}, "swapDetails": map[string]any{},
	}, "status", "quoteResponse")
	statusOutput["additionalProperties"] = true
	add("host.intents.status", intentsNetwork, args(schemaObject(map[string]any{
		"depositAddress": schemaString(1, 512), "depositMemo": schemaString(0, 512),
	}, "depositAddress")), statusOutput, both...)
	add("host.accounts", "read: balances", args(), schemaArray(schemaObject(map[string]any{
		"address": schemaString(0, 0), "label": schemaString(0, 0), "family": schemaEnum("evm", "near", "solana"),
	}, "address", "family"), 0), both...)
	add("host.requestSign", "sign + EVM chain + native approval", args(signSchema()), schemaObject(map[string]any{"hash": schemaString(1, 0)}, "hash"), "ui")
	add("host.requestNearSign", "sign + NEAR chain + native approval", args(nearSignSchema()), schemaObject(map[string]any{"hash": schemaString(1, 0)}, "hash"), "ui")
	add("host.log", "none; never log credentials", args(schemaEnum("debug", "info", "warn", "error"), schemaString(0, 0)), map[string]any{}, both...)
	add("host.i18n", "none", map[string]any{"type": "array", "minItems": 1}, schemaString(0, 0), both...)
	add("host.render", "none", args(schemas["page"]), schemas["page"], both...)
	examples := []map[string]any{}
	for _, id := range []string{"com.example.gas", "com.example.feargreed", "com.example.hyperliquid", "com.example.intents"} {
		pkg, hash, err := plugin.OfficialPackage(id)
		if err != nil {
			continue
		}
		manifest, err := plugin.ParseManifest(pkg.Plugin)
		if err == nil {
			examples = append(examples, map[string]any{"id": id, "version": manifest.Version, "url": "/v0/examples/" + id, "packageHash": hash})
		}
	}
	return map[string]any{
		"apiVersion": "v0", "catalogVersion": 1, "shellVersion": version.Version,
		"schemas": schemas, "hostFunctions": functions, "rpcMethods": plugin.RPCMethods(), "chains": networks,
		"permissions": map[string]any{
			"network": "HTTPS hostname allowlist; redirects checked on every hop", "chains": chainIDs,
			"read": []string{"balances"}, "sign": "native decoded approval on every request; never available in apiRefresh",
			"storageKb": map[string]int{"minimum": 0, "maximum": 1024},
		},
		"features": map[string]bool{"install": managed, "update": managed, "singleFile": managed, "settings": managed},
		"examples": examples,
		"limits": map[string]int{
			"manifestBytes": 64 << 10, "sourceBytes": 1 << 20, "fileCount": 200, "fileBytes": 2 << 20,
			"packageBytes": plugin.MaxPackageBytes, "applicationRequestBytes": maxApplicationRequest,
			"settingsRequestBytes": maxSettingsRequest, "settingValueBytes": 8192, "pageBytes": 256 << 10,
			"sections": 32, "stats": 12, "tableColumns": 8, "tableRows": 200, "cards": 32, "links": 16,
			"chartPoints": 60, "sparklinePoints": 32, "gaugeMin": 0, "gaugeMax": 100,
			"settings": 20, "navigation": 8, "kvValueBytes": 64 << 10,
			"memoryBytes": 256 << 20, "executionSeconds": 30, "fetchSeconds": 30, "fetchResponseBytes": 8 << 20,
			"pendingTasks": 5, "pendingSeconds": 1800,
		},
	}
}

func schemaString(minimum, maximum int) map[string]any {
	s := map[string]any{"type": "string"}
	if minimum > 0 {
		s["minLength"] = minimum
	}
	if maximum > 0 {
		s["maxLength"] = maximum
	}
	return s
}

func schemaEnum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func schemaObject(properties map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func schemaArray(items any, maximum int) map[string]any {
	s := map[string]any{"type": "array", "items": items}
	if maximum > 0 {
		s["maxItems"] = maximum
	}
	return s
}

func applicationSchemas(chainIDs []string) map[string]any {
	pageName := schemaString(1, 32)
	pageName["pattern"] = `^[a-z][a-z0-9-]{0,31}$`
	settingKey := schemaString(1, 64)
	settingKey["pattern"] = `^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`
	semver := schemaString(1, 0)
	semver["pattern"] = `^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`
	id := schemaString(1, 128)
	id["pattern"] = `^[a-z][a-z0-9]*(\.[a-z0-9][a-z0-9-]*)+$`
	manifest := schemaObject(map[string]any{
		"id": id, "name": schemaString(1, 64), "version": semver, "main": schemaString(1, 0),
		"minAppVersion": semver, "description": schemaString(0, 400), "author": schemaString(0, 0),
		"permissions": schemaObject(map[string]any{
			"network": schemaArray(schemaString(1, 0), 0), "chains": schemaArray(schemaEnum(chainIDs...), 0),
			"read": schemaArray(schemaEnum("balances"), 0), "sign": map[string]any{"type": "boolean"},
			"storageKb": map[string]any{"type": "integer", "minimum": 0, "maximum": 1024},
		}),
		"settings": schemaArray(schemaObject(map[string]any{
			"key": settingKey, "label": schemaString(1, 80), "secret": map[string]any{"type": "boolean"},
			"help": schemaString(0, 200), "link": map[string]any{"type": "string", "pattern": `^https://`},
		}, "key", "label"), 20),
		"navigation": schemaArray(schemaObject(map[string]any{
			"title": schemaString(1, 40), "page": pageName, "icon": schemaString(0, 32),
		}, "title", "page"), 8),
	}, "id", "name", "version", "main")
	columns := schemaArray(schemaObject(map[string]any{"title": schemaString(0, 40), "align": schemaEnum("", "start", "end")}, "title"), 8)
	columns["minItems"] = 1
	points := schemaArray(schemaObject(map[string]any{"label": schemaString(0, 24), "value": map[string]any{"type": "number"}}, "label", "value"), 60)
	points["minItems"] = 1
	section := schemaObject(map[string]any{
		"title": schemaString(0, 120),
		"stats": schemaArray(schemaObject(map[string]any{
			"label": schemaString(0, 40), "value": schemaString(0, 80),
			"trend":     schemaEnum("", "up", "down"),
			"sparkline": schemaArray(map[string]any{"type": "number"}, 32),
		}, "label", "value"), 12),
		"table": schemaObject(map[string]any{"columns": columns, "rows": schemaArray(schemaArray(schemaString(0, 200), 8), 200)}, "columns", "rows"),
		"cards": schemaArray(schemaObject(map[string]any{"title": schemaString(0, 80), "subtitle": schemaString(0, 120), "body": schemaString(0, 500)}, "title"), 32),
		"links": schemaArray(schemaObject(map[string]any{"title": schemaString(0, 60), "page": pageName}, "title", "page"), 16),
		"text":  schemaString(0, 2000), "divider": map[string]any{"type": "boolean"},
		"chart": schemaObject(map[string]any{"kind": schemaEnum("", "line", "bar"), "points": points}, "points"),
		"gauge": schemaObject(map[string]any{
			"label": schemaString(0, 40), "text": schemaString(0, 20),
			"value": map[string]any{"type": "number", "minimum": float64(0), "maximum": float64(100)},
		}, "value"),
	})
	page := schemaObject(map[string]any{"title": schemaString(0, 120), "subtitle": schemaString(0, 160), "sections": schemaArray(section, 32)}, "sections")
	pkgManifest := map[string]any{}
	for key, value := range manifest {
		pkgManifest[key] = value
	}
	pkgManifest["allOf"] = []any{map[string]any{"properties": map[string]any{"main": map[string]any{"const": "index.js"}}}}
	return map[string]any{
		"manifest": manifest, "page": page,
		"package":         schemaObject(map[string]any{"plugin": pkgManifest, "main": map[string]any{"type": "string", "maxUtf8Bytes": 1 << 20}}, "plugin", "main"),
		"validationNotes": []string{"文本拒绝控制字符及双向覆盖字符", "table 每行单元格数必须等于列数", "JSON Schema 通过后仍须调用 validate 检查路径、字节上限与跨字段约束"},
	}
}

func signSchema() map[string]any {
	return schemaObject(map[string]any{
		"chainId": schemaString(1, 0), "from": schemaString(0, 0), "to": schemaString(1, 0),
		"value": schemaString(0, 0), "data": schemaString(0, 0), "gas": map[string]any{"type": "integer", "minimum": 0},
		"nonce": map[string]any{"type": "integer", "minimum": 0}, "maxFeePerGas": schemaString(0, 0),
		"maxPriorityFeePerGas": schemaString(0, 0), "description": schemaString(0, 0),
	}, "chainId", "to")
}

func nearSignSchema() map[string]any {
	action := map[string]any{"oneOf": []any{
		schemaObject(map[string]any{"type": schemaEnum("transfer"), "amount": schemaString(1, 0)}, "type", "amount"),
		schemaObject(map[string]any{"type": schemaEnum("functionCall"), "method": schemaString(1, 0), "args": schemaString(0, 0), "gas": schemaString(1, 0), "deposit": schemaString(1, 0)}, "type", "method", "args", "gas", "deposit"),
		schemaObject(map[string]any{"type": schemaEnum("addFunctionCallKey"), "publicKey": schemaString(1, 0), "receiverId": schemaString(1, 0), "methodNames": schemaArray(schemaString(0, 0), 0), "allowance": schemaString(1, 0)}, "type", "publicKey", "receiverId", "methodNames", "allowance"),
	}}
	actions := schemaArray(action, 8)
	actions["minItems"] = 1
	return schemaObject(map[string]any{"chainId": schemaEnum("near:testnet", "near:mainnet"), "from": schemaString(0, 0), "receiver": schemaString(1, 0), "description": schemaString(0, 200), "actions": actions}, "chainId", "receiver", "actions")
}
