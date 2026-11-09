package solanapasskey

import (
	"bytes"
	"testing"

	"filippo.io/edwards25519"

	"github.com/davirain-su/lintel/internal/base58"
	"github.com/davirain-su/lintel/internal/passkeywallet"
)

// pinnedKey is the same SEC-1 point the NEAR passkey tests pin: 0x02 and the
// bytes 1..32. It is a real P-256 point.
func pinnedKey(t *testing.T) passkeywallet.PublicKey {
	t.Helper()
	raw := make([]byte, 33)
	raw[0] = 0x02
	for i := 0; i < 32; i++ {
		raw[i+1] = byte(i + 1)
	}
	key, err := passkeywallet.ParsePublicKey("p256:" + base58.Encode(raw))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestVaultIsAStableOffCurveAddress(t *testing.T) {
	key := pinnedKey(t)
	first, err := Derive(key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Derive(key)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("derivation is not stable: %+v vs %+v", first, second)
	}
	if first.Program != ProgramMainnetV1 {
		t.Fatalf("program = %s", first.Program)
	}
	if first.Vault == first.Wallet {
		t.Fatal("the vault and the wallet PDA are the same address")
	}
	vault, err := base58.Decode(first.Vault)
	if err != nil || len(vault) != 32 {
		t.Fatalf("vault %q: %v", first.Vault, err)
	}
	if _, err := new(edwards25519.Point).SetBytes(vault); err == nil {
		t.Fatal("the vault is an ed25519 point, so a private key could exist for it")
	}
	// A hash of the public key is not an address this program can own.
	if bytes.Equal(vault, UserSeed(key)) {
		t.Fatal("the vault is just the user seed")
	}
	otherRaw := make([]byte, 33)
	otherRaw[0] = 0x03
	for i := 0; i < 32; i++ {
		otherRaw[i+1] = byte(i + 1)
	}
	other, err := passkeywallet.ParsePublicKey("p256:" + base58.Encode(otherRaw))
	if err != nil {
		t.Fatal(err)
	}
	alt, err := Derive(other)
	if err != nil {
		t.Fatal(err)
	}
	if alt.Vault == first.Vault {
		t.Fatal("two public keys derived the same vault")
	}
	devnet, err := DeriveFor(key, ProgramDevnetV1)
	if err != nil {
		t.Fatal(err)
	}
	if devnet.Vault == first.Vault {
		t.Fatal("mainnet and devnet derived the same vault")
	}
}

func TestVaultMatchesTheSolanaReference(t *testing.T) {
	// Computed with @solana/web3.js PublicKey.findProgramAddressSync:
	// user seed = sha256("crypto-panel passkey solana v1\0" || compressed key),
	// wallet = findProgramAddress(["wallet", seed], v1 mainnet program),
	// vault = findProgramAddress(["vault", wallet], same program).
	// The pinned key is 0x02 followed by the bytes 1..32.
	key := pinnedKey(t)
	got, err := Derive(key)
	if err != nil {
		t.Fatal(err)
	}
	const (
		wantVault  = "HLzSiScX24n8113sZXahHktXvNhL8JoVYCppEjiHguKW"
		wantWallet = "3539MaLz4ypgZTHjN3dv6gevdCbwRy44DTPTARf414dy"
	)
	if got.Vault != wantVault || got.Wallet != wantWallet {
		t.Fatalf("got vault %s wallet %s", got.Vault, got.Wallet)
	}
}
