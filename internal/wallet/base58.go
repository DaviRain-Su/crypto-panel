package wallet

import (
	"fmt"
	"math/big"
	"strings"
)

// base58Alphabet is Bitcoin's alphabet, which Solana uses: no 0, O, I or l,
// so the characters that look alike cannot be confused.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58Radix = big.NewInt(58)

// decodeBase58 decodes s and requires exactly 32 bytes, the length of a
// Solana public key. Each leading '1' is a leading zero byte.
func decodeBase58(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("empty address")
	}
	n := new(big.Int)
	for _, c := range s {
		idx := strings.IndexRune(base58Alphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("%q is not a base58 character", c)
		}
		n.Mul(n, base58Radix)
		n.Add(n, big.NewInt(int64(idx)))
	}
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	body := n.Bytes()
	out := make([]byte, zeros+len(body))
	copy(out[zeros:], body)
	if len(out) != 32 {
		return nil, fmt.Errorf("want 32 bytes, got %d", len(out))
	}
	return out, nil
}
