// Package wallet holds what the app knows about the addresses it watches.
// The panel is watch-only: an address here is a string to read balances
// for, never a key. It speaks both families: EVM addresses (0x + 40 hex,
// EIP-55) and Solana addresses (base58, 32 bytes).
package wallet

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/sha3"

	"github.com/davirain-su/crypto-panel/internal/nearid"
)

// Kind is an address format, one per chain family.
type Kind string

const (
	EVM    Kind = "evm"
	Near   Kind = "near"
	Solana Kind = "solana"
)

// ErrInvalidAddress reports a string that is not an address of the kind
// asked for.
var ErrInvalidAddress = errors.New("not a valid address")

// Address is one watched account. Addr is the normalized form the app
// stores and compares: lowercase 0x-prefixed for EVM, base58 as written for
// Solana.
type Address struct {
	Addr  string `json:"address"`
	Label string `json:"label,omitempty"`
}

// KindOf infers an address's family from its shape: 0x and 40 hex digits is
// EVM; 64 hex digits or a dotted lowercase name is NEAR; a base58 string
// that decodes to 32 bytes is Solana.
func KindOf(s string) (Kind, bool) {
	s = strings.TrimSpace(s)
	if isEVMLike(s) {
		return EVM, true
	}
	if nearid.IsImplicit(s) || nearid.IsDeterministic(s) {
		return Near, true
	}
	if strings.Contains(s, ".") {
		if _, err := nearid.ParseAccountID(s); err == nil {
			return Near, true
		}
	}
	if _, err := decodeBase58(s); err == nil {
		return Solana, true
	}
	return "", false
}

// Normalize validates s as an address of kind and returns its stored form.
// An EVM address must carry a valid EIP-55 checksum when it is mixed case,
// so a typo in one character is refused rather than watched. A Solana
// address is case-sensitive base58 and is kept as written.
func Normalize(kind Kind, s string) (string, error) {
	s = strings.TrimSpace(s)
	switch kind {
	case EVM:
		return normalizeEVM(s)
	case Near:
		addr, err := nearid.ParseAccountID(s)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidAddress, err)
		}
		return addr, nil
	case Solana:
		if _, err := decodeBase58(s); err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidAddress, err)
		}
		return s, nil
	}
	return "", fmt.Errorf("%w: unknown address kind %q", ErrInvalidAddress, kind)
}

func normalizeEVM(s string) (string, error) {
	if !isEVMLike(s) {
		return "", fmt.Errorf("%w: want 0x followed by 40 hex digits", ErrInvalidAddress)
	}
	hexPart := s[2:]
	lower := strings.ToLower(hexPart)
	if hexPart != lower && hexPart != strings.ToUpper(hexPart) {
		if Checksum(lower) != "0x"+hexPart {
			return "", fmt.Errorf("%w: the EIP-55 checksum does not match", ErrInvalidAddress)
		}
	}
	return "0x" + lower, nil
}

func isEVMLike(s string) bool {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	hexPart := s[2:]
	if len(hexPart) != 40 {
		return false
	}
	for _, c := range hexPart {
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

// Display returns addr in the form a person reads: EIP-55 mixed case for
// EVM, unchanged for Solana. An invalid address is returned unchanged.
func Display(kind Kind, addr string) string {
	if kind == EVM {
		return Checksum(addr)
	}
	return addr
}

// Checksum returns addr in EIP-55 mixed case. addr must be a valid EVM
// address in any case; an invalid one is returned unchanged.
func Checksum(addr string) string {
	a := strings.TrimSpace(addr)
	if strings.HasPrefix(a, "0x") || strings.HasPrefix(a, "0X") {
		a = a[2:]
	}
	if len(a) != 40 {
		return addr
	}
	a = strings.ToLower(a)
	hash := Keccak256([]byte(a))
	out := []byte(a)
	for i, c := range out {
		if c < 'a' || c > 'f' {
			continue
		}
		nibble := hash[i/2]
		if i%2 == 0 {
			nibble >>= 4
		} else {
			nibble &= 0x0f
		}
		if nibble >= 8 {
			out[i] = c - 'a' + 'A'
		}
	}
	return "0x" + string(out)
}

// Short returns the first and last few characters of addr, for a row that
// has no label.
func Short(addr string) string {
	a := strings.TrimSpace(addr)
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "…" + a[len(a)-4:]
}

// Keccak256 is Ethereum's hash: the original Keccak padding, not SHA3's.
func Keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func isHexDigit(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
