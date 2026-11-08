// Package base58 is Bitcoin's base58 encoding, which NEAR and Solana use for
// keys and hashes: no 0, O, I or l, so the characters that look alike cannot
// be confused.
package base58

import (
	"fmt"
	"math/big"
	"strings"
)

const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var radix = big.NewInt(58)

// Encode returns the base58 text of b. Each leading zero byte is a leading
// '1'.
func Encode(b []byte) string {
	zeros := 0
	for zeros < len(b) && b[zeros] == 0 {
		zeros++
	}
	n := new(big.Int).SetBytes(b)
	var out []byte
	mod := new(big.Int)
	for n.Sign() > 0 {
		n.QuoRem(n, radix, mod)
		out = append(out, alphabet[mod.Int64()])
	}
	for i := 0; i < zeros; i++ {
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// Decode returns the bytes base58 encodes. Each leading '1' is a leading zero
// byte.
func Decode(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	n := new(big.Int)
	for _, c := range s {
		idx := strings.IndexRune(alphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("%q is not a base58 character", c)
		}
		n.Mul(n, radix)
		n.Add(n, big.NewInt(int64(idx)))
	}
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	body := n.Bytes()
	out := make([]byte, zeros+len(body))
	copy(out[zeros:], body)
	return out, nil
}
