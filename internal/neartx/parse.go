package neartx

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/davirain-su/crypto-panel/internal/base58"
	"github.com/davirain-su/crypto-panel/internal/nearid"
)

// ErrInvalidKey reports a string that is not an ed25519 key in a form the
// wallet reads.
var ErrInvalidKey = errors.New("not a valid NEAR key")

// ErrInvalidAccount reports a string that is not a NEAR account id.
var ErrInvalidAccount = nearid.ErrInvalidAccount

// ParseAccountID validates a NEAR account id.
func ParseAccountID(s string) (string, error) { return nearid.ParseAccountID(s) }

// ParsePublicKey reads an ed25519 public key as "ed25519:<base58>" or bare
// base58.
func ParsePublicKey(s string) (PublicKey, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "ed25519:"); ok {
		s = rest
	} else if strings.Contains(s, ":") {
		return PublicKey{}, fmt.Errorf("%w: only ed25519 keys are supported", ErrInvalidKey)
	}
	raw, err := base58.Decode(s)
	if err != nil {
		return PublicKey{}, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return PublicKey{}, fmt.Errorf("%w: want 32 bytes, got %d", ErrInvalidKey, len(raw))
	}
	var key PublicKey
	copy(key.Bytes[:], raw)
	return key, nil
}

// String returns the key as NEAR writes it, "ed25519:<base58>".
func (k PublicKey) String() string {
	return "ed25519:" + base58.Encode(k.Bytes[:])
}

// ParseSecretKey reads an ed25519 secret key as "ed25519:<base58>" (64 bytes,
// seed and public key, as near-cli exports it), 64 hex digits (the seed), or
// bare base58 of 32 or 64 bytes.
func ParseSecretKey(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "ed25519:"); ok {
		s = rest
	} else if strings.Contains(s, ":") {
		return nil, fmt.Errorf("%w: only ed25519 keys are supported", ErrInvalidKey)
	}
	if len(s) == 2*ed25519.SeedSize && isHex(s) {
		seed, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	raw, err := base58.Decode(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		key := ed25519.PrivateKey(append([]byte(nil), raw...))
		derived := ed25519.NewKeyFromSeed(key.Seed())
		if !bytes.Equal(derived.Public().(ed25519.PublicKey), key.Public().(ed25519.PublicKey)) {
			return nil, fmt.Errorf("%w: the public key does not match the seed", ErrInvalidKey)
		}
		return derived, nil
	}
	return nil, fmt.Errorf("%w: want 32 or 64 bytes, got %d", ErrInvalidKey, len(raw))
}

// PublicKeyOf returns the public key of an ed25519 private key.
func PublicKeyOf(key ed25519.PrivateKey) PublicKey {
	var out PublicKey
	copy(out.Bytes[:], key.Public().(ed25519.PublicKey))
	return out
}

// SecretKeyString returns the key as near-cli exports it,
// "ed25519:<base58 of seed and public key>".
func SecretKeyString(key ed25519.PrivateKey) string {
	return "ed25519:" + base58.Encode(key)
}

// ParseAmount reads a non-negative decimal yoctoNEAR amount. An empty string
// is zero.
func ParseAmount(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return new(big.Int), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Sign() < 0 {
		return nil, fmt.Errorf("%q is not a whole yoctoNEAR amount", s)
	}
	return v, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}
