package plugin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/MoeForge/moejs"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
)

// objectArg converts a host request without passing JavaScript values to a
// protocol adapter. Individual methods still reject fields they do not own.
func objectArg(r *moejs.Realm, args []moejs.Value, name string) (map[string]any, error) {
	v := moejs.Arg(args, 0)
	if v.IsUndefined() || v.IsNull() {
		return nil, fmt.Errorf("%s needs an object", name)
	}
	raw, err := r.ToGoStrict(v)
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s needs an object", name)
	}
	return obj, nil
}

func integerArg(value any, name string, maximum int) (int, error) {
	n, ok := value.(int64)
	if !ok || n < 0 || n > int64(maximum) {
		return 0, fmt.Errorf("%s must be an integer from 0 to %d", name, maximum)
	}
	return int(n), nil
}

func decimalsArg(r *moejs.Realm, v moejs.Value) (int, error) {
	raw, err := r.ToGoStrict(v)
	if err != nil {
		return 0, err
	}
	return integerArg(raw, "decimals", chain.MaxTokenDecimals)
}

func requireString(r *moejs.Realm, v moejs.Value, name string) (string, error) {
	if !v.IsString() {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return stringArg(r, []moejs.Value{v}, 0)
}

func (h *Host) unitsParse(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	amount, err := requireString(r, moejs.Arg(args, 0), "amount")
	if err != nil {
		return moejs.Undefined(), err
	}
	decimals, err := decimalsArg(r, moejs.Arg(args, 1))
	if err != nil {
		return moejs.Undefined(), err
	}
	parsed, ok := chain.ParseUnits(amount, decimals)
	if !ok {
		return moejs.Undefined(), errors.New("units.parse needs an unsigned decimal with at most decimals fractional digits")
	}
	return moejs.String(parsed.String()), nil
}

func baseUnits(amount string) (*big.Int, bool) {
	if amount == "" || strings.TrimSpace(amount) != amount {
		return nil, false
	}
	if strings.HasPrefix(amount, "+") {
		return nil, false
	}
	digits := amount
	if strings.HasPrefix(digits, "-") {
		digits = digits[1:]
	}
	if digits == "" {
		return nil, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	return new(big.Int).SetString(amount, 10)
}

func (h *Host) unitsFormat(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	amount, err := requireString(r, moejs.Arg(args, 0), "amount")
	if err != nil {
		return moejs.Undefined(), err
	}
	decimals, err := decimalsArg(r, moejs.Arg(args, 1))
	if err != nil {
		return moejs.Undefined(), err
	}
	parsed, ok := baseUnits(amount)
	if !ok {
		return moejs.Undefined(), errors.New("units.format needs an integer decimal string")
	}
	return moejs.String(chain.FormatUnits(parsed, decimals)), nil
}

func (h *Host) unitsFormatDisplay(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	amount, err := requireString(r, moejs.Arg(args, 0), "amount")
	if err != nil {
		return moejs.Undefined(), err
	}
	decimals, err := decimalsArg(r, moejs.Arg(args, 1))
	if err != nil {
		return moejs.Undefined(), err
	}
	rawFraction, err := r.ToGoStrict(moejs.Arg(args, 2))
	if err != nil {
		return moejs.Undefined(), err
	}
	maxFraction, err := integerArg(rawFraction, "maxFraction", chain.MaxTokenDecimals)
	if err != nil {
		return moejs.Undefined(), err
	}
	parsed, ok := baseUnits(amount)
	if !ok {
		return moejs.Undefined(), errors.New("units.formatDisplay needs an integer decimal string")
	}
	return moejs.String(chain.FormatAmount(parsed, decimals, maxFraction)), nil
}

func (h *Host) nearView(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	request, err := objectArg(r, args, "near.view")
	if err != nil {
		return moejs.Undefined(), err
	}
	for key := range request {
		switch key {
		case "chainId", "contract", "method", "args":
		default:
			return moejs.Undefined(), fmt.Errorf("near.view: key %q is not supported", key)
		}
	}
	chainID, chainOK := request["chainId"].(string)
	contract, contractOK := request["contract"].(string)
	method, methodOK := request["method"].(string)
	if !chainOK || !contractOK || !methodOK {
		return moejs.Undefined(), errors.New("near.view: chainId, contract and method must be strings")
	}
	if !slicesContains(h.deps.Manifest.Permissions.Chains, chainID) {
		return moejs.Undefined(), fmt.Errorf("near.view: chain %q is not in the plugin's permissions", chainID)
	}
	network, ok := chains.ByID(chainID)
	if !ok || network.Family != "near" {
		return moejs.Undefined(), errors.New("near.view: chainId must name a NEAR chain")
	}
	if len(contract) < 2 || len(contract) > 64 || len(method) < 1 || len(method) > 128 {
		return moejs.Undefined(), errors.New("near.view: invalid contract or method length")
	}
	arguments := any(map[string]any{})
	if value, present := request["args"]; present {
		arguments = value
	}
	argumentJSON, err := json.Marshal(arguments)
	if err != nil {
		return moejs.Undefined(), err
	}
	if len(argumentJSON) > 64<<10 {
		return moejs.Undefined(), errors.New("near.view: arguments are larger than 65536 bytes")
	}
	if h.deps.RPC == nil {
		return moejs.Undefined(), errors.New("near.view: rpc is not available")
	}
	raw, err := h.deps.RPC(chainID, "query", map[string]any{
		"request_type": "call_function", "account_id": contract,
		"method_name": method, "args_base64": base64.StdEncoding.EncodeToString(argumentJSON), "finality": "final",
	})
	if err != nil {
		return moejs.Undefined(), err
	}
	// Node responses also carry logs and block metadata. They are not plugin
	// input, so ignore those fields instead of applying the strict package
	// decoder's unknown-field rule to a third-party protocol response.
	var response map[string]json.RawMessage
	if len(raw) > 4<<20 || json.Unmarshal(raw, &response) != nil || response == nil {
		return moejs.Undefined(), errors.New("near.view: the node response is invalid or too large")
	}
	encoded, present := response["result"]
	if !present || string(encoded) == "null" {
		return moejs.Undefined(), errors.New("near.view: the node did not return a result")
	}
	// NEAR RPC specifies an array of bytes. Accept the legacy base64 string
	// form too so custom brokers that preserve the original string can read.
	var result []byte
	if len(encoded) != 0 && encoded[0] == '"' {
		var resultBase64 string
		if err := json.Unmarshal(encoded, &resultBase64); err != nil {
			return moejs.Undefined(), errors.New("near.view: the result is not a base64 string")
		}
		result, err = base64.StdEncoding.DecodeString(resultBase64)
		if err != nil {
			return moejs.Undefined(), errors.New("near.view: the result is not base64")
		}
	} else if err := json.Unmarshal(encoded, &result); err != nil {
		return moejs.Undefined(), errors.New("near.view: the result is not a byte array")
	}
	if len(result) == 0 {
		return moejs.Null(), nil
	}
	var decoded any
	if err := json.Unmarshal(result, &decoded); err != nil {
		return moejs.Undefined(), errors.New("near.view: the result is not JSON")
	}
	return r.FromGo(decoded)
}

func requireNetwork(m Manifest, hostname, capability string) error {
	if !hostAllowed(m.Permissions.Network, hostname) {
		return fmt.Errorf("%s: host %q is not in the plugin's network allowlist", capability, hostname)
	}
	return nil
}

func (h *Host) hyperliquidInfo(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	request, err := objectArg(r, args, "hyperliquid.info")
	if err != nil {
		return moejs.Undefined(), err
	}
	operation, _ := request["type"].(string)
	hostname := "api.hyperliquid.xyz"
	testnet := false
	if value, present := request["testnet"]; present {
		enabled, ok := value.(bool)
		if !ok {
			return moejs.Undefined(), errors.New("hyperliquid.info: testnet must be a boolean")
		}
		testnet = enabled
		if enabled {
			hostname = "api.hyperliquid-testnet.xyz"
		}
	}
	if err := requireNetwork(h.deps.Manifest, hostname, "hyperliquid.info"); err != nil {
		return moejs.Undefined(), err
	}
	params := make(map[string]any, len(request))
	for key, value := range request {
		if key != "type" && key != "testnet" {
			params[key] = value
		}
	}
	if h.deps.HyperliquidInfo == nil {
		return moejs.Undefined(), errors.New("hyperliquid.info is not available")
	}
	raw, err := h.deps.HyperliquidInfo(testnet, operation, params)
	if err != nil {
		return moejs.Undefined(), err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return moejs.Undefined(), errors.New("hyperliquid.info: the answer is not JSON")
	}
	return r.FromGo(decoded)
}

func (h *Host) intentsTokens(r *moejs.Realm, _ moejs.Value, _ []moejs.Value) (moejs.Value, error) {
	if err := requireNetwork(h.deps.Manifest, "1click.chaindefuser.com", "intents.tokens"); err != nil {
		return moejs.Undefined(), err
	}
	if h.deps.IntentsTokens == nil {
		return moejs.Undefined(), errors.New("intents.tokens is not available")
	}
	result, err := h.deps.IntentsTokens()
	if err != nil {
		return moejs.Undefined(), err
	}
	return r.FromGo(result)
}

func (h *Host) intentsDryQuote(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	if err := requireNetwork(h.deps.Manifest, "1click.chaindefuser.com", "intents.dryQuote"); err != nil {
		return moejs.Undefined(), err
	}
	request, err := objectArg(r, args, "intents.dryQuote")
	if err != nil {
		return moejs.Undefined(), err
	}
	if _, present := request["dry"]; present {
		return moejs.Undefined(), errors.New("intents.dryQuote does not accept dry; only dry quotes are available")
	}
	if h.deps.IntentsDryQuote == nil {
		return moejs.Undefined(), errors.New("intents.dryQuote is not available")
	}
	result, err := h.deps.IntentsDryQuote(request)
	if err != nil {
		return moejs.Undefined(), err
	}
	return r.FromGo(result)
}

func (h *Host) intentsStatus(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	if err := requireNetwork(h.deps.Manifest, "1click.chaindefuser.com", "intents.status"); err != nil {
		return moejs.Undefined(), err
	}
	request, err := objectArg(r, args, "intents.status")
	if err != nil {
		return moejs.Undefined(), err
	}
	for key := range request {
		if key != "depositAddress" && key != "depositMemo" {
			return moejs.Undefined(), fmt.Errorf("intents.status: key %q is not supported", key)
		}
	}
	address, addressOK := request["depositAddress"].(string)
	memo, memoOK := request["depositMemo"].(string)
	if _, present := request["depositMemo"]; present && !memoOK {
		return moejs.Undefined(), errors.New("intents.status: depositMemo must be a string")
	}
	if !addressOK || strings.TrimSpace(address) == "" || len(address) > 512 || len(memo) > 512 {
		return moejs.Undefined(), errors.New("intents.status: depositAddress must be a string of 1–512 characters and depositMemo at most 512")
	}
	if h.deps.IntentsStatus == nil {
		return moejs.Undefined(), errors.New("intents.status is not available")
	}
	result, err := h.deps.IntentsStatus(address, memo)
	if err != nil {
		return moejs.Undefined(), err
	}
	return r.FromGo(result)
}
