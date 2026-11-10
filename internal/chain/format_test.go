package chain

import (
	"math/big"
	"strings"
	"testing"
)

func bi(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad test number " + s)
	}
	return v
}

func TestFormatUnits(t *testing.T) {
	for _, tc := range []struct {
		v        string
		decimals int
		want     string
	}{
		{"0", 18, "0"},
		{"1", 18, "0.000000000000000001"},
		{"1500000000000000000", 18, "1.5"},
		{"1000000000000000000", 18, "1"},
		{"1234500", 6, "1.2345"},
		{"1000000", 6, "1"},
		{"42", 0, "42"},
		{"-1500000000000000000", 18, "-1.5"},
	} {
		if got := FormatUnits(bi(tc.v), tc.decimals); got != tc.want {
			t.Errorf("FormatUnits(%s, %d) = %q, want %q", tc.v, tc.decimals, got, tc.want)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	for _, tc := range []struct {
		v        string
		decimals int
		maxFrac  int
		want     string
	}{
		{"0", 18, 4, "0"},
		{"1500000000000000000", 18, 4, "1.5"},
		{"1234567890000000000", 18, 4, "1.2346"},
		{"1234567890123456789012", 18, 2, "1,234.57"},
		{"1", 18, 4, "<0.0001"},
		{"-1", 18, 4, ">-0.0001"},
		{"999999999999999999", 18, 4, "1"},
		{"1000000", 6, 2, "1"},
		{"1234567", 6, 2, "1.23"},
		{"1239999", 6, 2, "1.24"},
		{"1200000000", 6, 2, "1,200"},
		{"-1500000000000000000", 18, 4, "-1.5"},
	} {
		if got := FormatAmount(bi(tc.v), tc.decimals, tc.maxFrac); got != tc.want {
			t.Errorf("FormatAmount(%s, %d, %d) = %q, want %q", tc.v, tc.decimals, tc.maxFrac, got, tc.want)
		}
	}
}

func TestParseUnits(t *testing.T) {
	for _, tc := range []struct {
		s        string
		decimals int
		want     string
		ok       bool
	}{
		{"1", 24, "1000000000000000000000000", true},
		{"1.5", 6, "1500000", true},
		{"0.000001", 6, "1", true},
		{".5", 6, "500000", true},
		{"1.", 6, "1000000", true},
		{" 1.25 ", 2, "125", true},
		{"0", 6, "0", true},
		{"1.2345678", 6, "", false},
		{"1.2.3", 6, "", false},
		{"-1", 6, "", false},
		{"1e6", 6, "", false},
		{"", 6, "", false},
		{".", 6, "", false},
		{"1", -1, "", false},
		{"1", -3, "", false},
		{"1", 256, "", false},
		{"1", MaxTokenDecimals, "1" + strings.Repeat("0", MaxTokenDecimals), true},
	} {
		got, ok := ParseUnits(tc.s, tc.decimals)
		if ok != tc.ok {
			t.Errorf("ParseUnits(%q, %d) ok = %v, want %v", tc.s, tc.decimals, ok, tc.ok)
			continue
		}
		if ok && got.String() != tc.want {
			t.Errorf("ParseUnits(%q, %d) = %s, want %s", tc.s, tc.decimals, got, tc.want)
		}
	}
}

func TestDisplayFrac(t *testing.T) {
	for _, tc := range []struct {
		decimals int
		want     int
	}{
		{6, 2},
		{18, 4},
		{8, 2},
		{2, 2},
		{0, 0},
	} {
		if got := (Token{Decimals: tc.decimals}).DisplayFrac(); got != tc.want {
			t.Errorf("DisplayFrac(%d) = %d, want %d", tc.decimals, got, tc.want)
		}
	}
}
