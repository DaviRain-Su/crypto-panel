package evm

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// ParseQuantity reads a JSON-RPC quantity: a hex string such as "0x1bc16d674ec80000".
func ParseQuantity(raw json.RawMessage) (*big.Int, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("quantity is not a string: %w", err)
	}
	return parseHex(s)
}

// ParseUint256 reads a 32-byte ABI word, as eth_call returns it: a hex
// string of up to 64 digits.
func ParseUint256(raw json.RawMessage) (*big.Int, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("word is not a string: %w", err)
	}
	s = strings.TrimPrefix(s, "0x")
	if len(s) > 64 {
		return nil, fmt.Errorf("word is %d hex digits, more than 32 bytes", len(s))
	}
	return parseHex("0x" + s)
}

func parseHex(s string) (*big.Int, error) {
	digits, ok := strings.CutPrefix(s, "0x")
	if !ok {
		return nil, fmt.Errorf("%q is not a hex quantity", s)
	}
	if digits == "" {
		return big.NewInt(0), nil
	}
	v, ok := new(big.Int).SetString(digits, 16)
	if !ok {
		return nil, fmt.Errorf("%q is not a hex quantity", s)
	}
	return v, nil
}
