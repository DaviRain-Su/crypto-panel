package wallet

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestKeccak256(t *testing.T) {
	got := hex.EncodeToString(Keccak256(nil))
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got != want {
		t.Fatalf("Keccak256(empty) = %s, want %s", got, want)
	}
}

func TestChecksumVectors(t *testing.T) {
	// The EIP-55 test vectors.
	for _, addr := range []string{
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
		"0x52908400098527886E0F7030069857D2E4169EE7",
		"0x8617E340B3D01FA5F11F306F4090FD50E238070D",
		"0xde709f2102306220921060314715629080e2fb77",
		"0x27b1fdb04752bbc536007a920d24acb045561c26",
	} {
		if got := Checksum(addr); got != addr {
			t.Errorf("Checksum(%s) = %s", addr, got)
		}
		if got := Checksum(lower(addr)); got != addr {
			t.Errorf("Checksum(lowercase %s) = %s", addr, got)
		}
	}
}

func TestNormalizeEVM(t *testing.T) {
	const mixed = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	const lowerAddr = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"

	for _, in := range []string{mixed, lowerAddr, "  " + mixed + "\n"} {
		got, err := Normalize(EVM, in)
		if err != nil {
			t.Fatalf("Normalize(EVM, %q): %v", in, err)
		}
		if got != lowerAddr {
			t.Errorf("Normalize(EVM, %q) = %q, want %q", in, got, lowerAddr)
		}
	}
}

func TestNormalizeRejects(t *testing.T) {
	const mixed = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	// The same address with one character's case flipped: the checksum no
	// longer matches.
	badChecksum := "0x5AAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	for _, in := range []string{
		"",
		"0x",
		"5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAe",
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed00",
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAeZ",
		badChecksum,
		// A Solana address is not an EVM one.
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
	} {
		if _, err := Normalize(EVM, in); !errors.Is(err, ErrInvalidAddress) {
			t.Errorf("Normalize(EVM, %q) error = %v, want ErrInvalidAddress", in, err)
		}
	}
	if _, err := Normalize(EVM, mixed); err != nil {
		t.Fatalf("the valid vector must stay valid: %v", err)
	}
}

func TestNormalizeSolana(t *testing.T) {
	for _, addr := range []string{
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", // USDC mint
		"11111111111111111111111111111111",             // the system program
		"7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
	} {
		got, err := Normalize(Solana, addr)
		if err != nil {
			t.Fatalf("Normalize(Solana, %q): %v", addr, err)
		}
		if got != addr {
			t.Errorf("Normalize(Solana, %q) = %q, want it unchanged", addr, got)
		}
	}
	for _, in := range []string{
		"",
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"short",
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v0", // 0 is not base58
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1O",  // O is not base58
	} {
		if _, err := Normalize(Solana, in); !errors.Is(err, ErrInvalidAddress) {
			t.Errorf("Normalize(Solana, %q) error = %v, want ErrInvalidAddress", in, err)
		}
	}
}

func TestKindOf(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want Kind
	}{
		{"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", EVM},
		{"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", EVM},
		{"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Solana},
		{"11111111111111111111111111111111", Solana},
		{"0s9dcb623ddaebc66fffc13ed50e61a3041ddae822", Near},
	} {
		got, ok := KindOf(tc.addr)
		if !ok || got != tc.want {
			t.Errorf("KindOf(%q) = %q, %v; want %q", tc.addr, got, ok, tc.want)
		}
	}
	for _, in := range []string{"", "0x", "hello", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAeZ"} {
		if _, ok := KindOf(in); ok {
			t.Errorf("KindOf(%q) must fail", in)
		}
	}
}

func TestDisplay(t *testing.T) {
	if got, want := Display(EVM, "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"), "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"; got != want {
		t.Errorf("Display(EVM) = %q, want %q", got, want)
	}
	sol := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	if got := Display(Solana, sol); got != sol {
		t.Errorf("Display(Solana) = %q, want %q", got, sol)
	}
}

func TestShort(t *testing.T) {
	addr := "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	if got, want := Short(addr), "0x5aAe…eAed"; got != want {
		t.Errorf("Short = %q, want %q", got, want)
	}
	if got := Short("0x1234"); got != "0x1234" {
		t.Errorf("Short(short) = %q", got)
	}
	if got, want := Short("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"), "EPjFWd…Dt1v"; got != want {
		t.Errorf("Short(Solana) = %q, want %q", got, want)
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'F' {
			b[i] = c - 'A' + 'a'
		}
	}
	return string(b)
}
