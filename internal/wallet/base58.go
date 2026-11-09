package wallet

import (
	"fmt"

	"github.com/davirain-su/lintel/internal/base58"
)

// decodeBase58 decodes s and requires exactly 32 bytes, the length of a
// Solana public key.
func decodeBase58(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("empty address")
	}
	out, err := base58.Decode(s)
	if err != nil {
		return nil, err
	}
	if len(out) != 32 {
		return nil, fmt.Errorf("want 32 bytes, got %d", len(out))
	}
	return out, nil
}
