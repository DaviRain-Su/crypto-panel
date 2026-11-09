package wallet

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// PasskeyContainerID is the wallet-switcher id of the passkey identity. It
// is not a key container: the passkey's private key stays in the authenticator.
const PasskeyContainerID = "passkey"

// SolanaAccount is one Solana account of a wallet. Its ed25519 key is not
// here: it is in the SecretStore under solanaKeySecret(Address).
type SolanaAccount struct {
	Address string `json:"address"`
	Label   string `json:"label,omitempty"`
}

// Container is one named wallet: a single identity's account on each chain
// family the panel can sign for. A slot is empty when the wallet was migrated
// from an older single-chain account, or when the user imported only one key.
// There is no seed phrase. The three keys are independent.
type Container struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	EVM    *Account       `json:"evm,omitempty"`
	Near   *NearAccount   `json:"near,omitempty"`
	Solana *SolanaAccount `json:"solana,omitempty"`
}

// Has reports whether c holds an account of kind.
func (c Container) Has(kind Kind) bool {
	switch kind {
	case EVM:
		return c.EVM != nil && c.EVM.Addr != ""
	case Near:
		return c.Near != nil && c.Near.AccountID != ""
	case Solana:
		return c.Solana != nil && c.Solana.Address != ""
	}
	return false
}

// Empty reports a container with no account on any chain.
func (c Container) Empty() bool {
	return !c.Has(EVM) && !c.Has(Near) && !c.Has(Solana)
}

// Address returns the account address of kind, or "" when the slot is empty.
func (c Container) Address(kind Kind) string {
	switch kind {
	case EVM:
		if c.EVM != nil {
			return c.EVM.Addr
		}
	case Near:
		if c.Near != nil {
			return c.Near.AccountID
		}
	case Solana:
		if c.Solana != nil {
			return c.Solana.Address
		}
	}
	return ""
}

// NewID is a new wallet id. It is not derived from a key.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("w-%d", time.Now().UnixNano())
	}
	return "w-" + hex.EncodeToString(b[:])
}
