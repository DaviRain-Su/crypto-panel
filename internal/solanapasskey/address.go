// Package solanapasskey derives the Solana account a WebAuthn passkey would
// control. A passkey is a P-256 key, so it is not a Solana ed25519 address.
// The account is a program-derived address of the LazorKit v1 smart-wallet
// program, which is deployed on Solana mainnet and spends from a vault PDA
// after the secp256r1 precompile (SIMD-0075,
// Secp256r1SigVerify1111111111111111111111111) accepts the passkey signature.
//
// The vault address is a pure function of the P-256 public key, the same way
// the NEAR account id is a function of that key's StateInit. Nothing here
// holds the passkey's private key.
//
// What this package does not do: it does not build or send a transaction.
// LazorKit's createWallet instruction needs a normal ed25519 fee payer, and
// this app does not submit that transaction. Until a create lands with this
// passkey as the owner, the vault does not exist and must not be funded.
// The program also does not bind the seed to the owner, so anyone who uses
// the same seed first can register a different owner. v1 may additionally
// refuse new creates once its sunset binary is the one on mainnet
// (RetiredDeployment, 4018). v2 (seeds prefixed lk2:, program
// LazorFroiVuAjcwwQ2me83vTr5nc5NRxSaTg3pmEXC8) was not deployed on mainnet
// when this was written, so the address we publish is the v1 vault of the
// program that is actually on mainnet. Devnet v1 is a different program id
// and therefore a different vault; the panel has no Solana devnet preset.
package solanapasskey

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"filippo.io/edwards25519"

	"github.com/davirain-su/crypto-panel/internal/base58"
	"github.com/davirain-su/crypto-panel/internal/passkeywallet"
)

const (
	// ProgramMainnetV1 is the LazorKit v1 program deployed on Solana mainnet.
	// It is the program that can sign for the vault address this package
	// derives. v1 seeds are the bare strings "wallet" and "vault".
	ProgramMainnetV1 = "LazorjRFNavitUaBu5m3WaNPjU1maipvSW2rZfAFAKi"
	// ProgramDevnetV1 is the same v1 program on devnet. Same seeds, different
	// program id, different addresses.
	ProgramDevnetV1 = "4h3XoNReAgEcHVxcZ8sw2aufi9MTr7BbvYYjzjWDyDxS"
	// ProgramMainnetV2 is the v2 program id. It was not deployed on mainnet
	// when this package was written, and its seeds are namespaced ("lk2:").
	// It is named here so a v2 address is not mistaken for the v1 one.
	ProgramMainnetV2 = "LazorFroiVuAjcwwQ2me83vTr5nc5NRxSaTg3pmEXC8"
	// ProgramDevnetV2 is the deployed v2 program on devnet.
	ProgramDevnetV2 = "57bTNWqtYTJbWuLWASKo6GqUTAK6oFDUR5c6hEc6V8nv"

	// seedLabel domains the user seed so it cannot collide with a seed some
	// other app chose for the same public key. The NUL keeps the label from
	// running into the key bytes.
	seedLabel = "crypto-panel passkey solana v1"

	pdaMarker = "ProgramDerivedAddress"
	maxSeeds  = 16
	maxSeed   = 32
)

// Account is the pair of PDAs LazorKit derives for one user seed. Vault is
// the address that would hold SOL. Wallet is the internal identity account;
// funds sent there cannot be spent.
type Account struct {
	Vault  string
	Wallet string
	// Program is the program id the addresses were derived for.
	Program string
}

// UserSeed is the 32-byte seed LazorKit's createWallet would be given for
// this public key. It is SHA-256 of a domain label and the SEC-1 compressed
// P-256 point. The program treats the seed as opaque caller input; it does
// not check that the seed commits to the passkey.
func UserSeed(pub passkeywallet.PublicKey) []byte {
	h := sha256.New()
	_, _ = h.Write([]byte(seedLabel))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(pub.Compressed())
	return h.Sum(nil)
}

// Derive returns the mainnet v1 vault and wallet PDAs for pub.
func Derive(pub passkeywallet.PublicKey) (Account, error) {
	return DeriveFor(pub, ProgramMainnetV1)
}

// VaultAddress is the mainnet deposit address for pub.
func VaultAddress(pub passkeywallet.PublicKey) (string, error) {
	acc, err := Derive(pub)
	if err != nil {
		return "", err
	}
	return acc.Vault, nil
}

// DeriveFor returns the v1-seed PDAs of programID. programID must be a
// base58 32-byte address. v2 programs use different seeds and are not
// derived here.
func DeriveFor(pub passkeywallet.PublicKey, programID string) (Account, error) {
	program, err := base58.Decode(programID)
	if err != nil || len(program) != 32 {
		return Account{}, fmt.Errorf("solanapasskey: program id %q is not a Solana address", programID)
	}
	seed := UserSeed(pub)
	wallet, _, err := findProgramAddress([][]byte{[]byte("wallet"), seed}, program)
	if err != nil {
		return Account{}, err
	}
	vault, _, err := findProgramAddress([][]byte{[]byte("vault"), wallet}, program)
	if err != nil {
		return Account{}, err
	}
	return Account{
		Vault:   base58.Encode(vault),
		Wallet:  base58.Encode(wallet),
		Program: programID,
	}, nil
}

// findProgramAddress is Solana's findProgramAddress: SHA-256 of the seeds,
// the bump, the program id and the PDA marker, from bump 255 down, keeping
// the first result that is not an ed25519 point.
func findProgramAddress(seeds [][]byte, program []byte) ([]byte, uint8, error) {
	if len(seeds)+1 > maxSeeds {
		return nil, 0, errors.New("solanapasskey: too many seeds")
	}
	for _, seed := range seeds {
		if len(seed) > maxSeed {
			return nil, 0, errors.New("solanapasskey: seed is longer than 32 bytes")
		}
	}
	bumpSeed := []byte{0}
	withBump := append(append([][]byte{}, seeds...), bumpSeed)
	for bump := 255; bump >= 0; bump-- {
		bumpSeed[0] = byte(bump)
		addr, err := createProgramAddress(withBump, program)
		if err == nil {
			out := make([]byte, len(addr))
			copy(out, addr)
			return out, uint8(bump), nil
		}
	}
	return nil, 0, errors.New("solanapasskey: no PDA bump was off the curve")
}

func createProgramAddress(seeds [][]byte, program []byte) ([]byte, error) {
	if len(program) != 32 {
		return nil, errors.New("solanapasskey: program id must be 32 bytes")
	}
	h := sha256.New()
	for _, seed := range seeds {
		if len(seed) > maxSeed {
			return nil, errors.New("solanapasskey: seed is longer than 32 bytes")
		}
		_, _ = h.Write(seed)
	}
	_, _ = h.Write(program)
	_, _ = h.Write([]byte(pdaMarker))
	sum := h.Sum(nil)
	if onCurve(sum) {
		return nil, errors.New("solanapasskey: address is on the ed25519 curve")
	}
	return sum, nil
}

// onCurve reports whether b is a valid compressed ed25519 point. Solana
// refuses those as PDAs, because a private key could exist for them.
func onCurve(b []byte) bool {
	if len(b) != 32 {
		return false
	}
	_, err := new(edwards25519.Point).SetBytes(b)
	return err == nil
}
