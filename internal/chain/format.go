package chain

import (
	"math/big"
	"strings"
)

var bigTen = big.NewInt(10)

// pow10 returns 10^n as a new big.Int.
func pow10(n int) *big.Int {
	if n <= 0 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil)
}

// FormatUnits writes v scaled by 10^-decimals exactly, without trailing
// zeros: 1234500 at 6 decimals is "1.2345".
func FormatUnits(v *big.Int, decimals int) string {
	if v == nil {
		return "0"
	}
	neg := v.Sign() < 0
	abs := new(big.Int).Abs(v)
	if decimals <= 0 {
		return sign(neg, abs.String())
	}
	unit := pow10(decimals)
	q, r := new(big.Int).QuoRem(abs, unit, new(big.Int))
	if r.Sign() == 0 {
		return sign(neg, q.String())
	}
	frac := r.String()
	frac = strings.Repeat("0", decimals-len(frac)) + frac
	frac = strings.TrimRight(frac, "0")
	return sign(neg, q.String()+"."+frac)
}

// FormatAmount writes v scaled by 10^-decimals, rounded half up to at most
// maxFrac fraction digits and grouped by thousands: 1234567890000000000 at
// 18 decimals with 4 fraction digits is "1.2346". A nonzero amount smaller
// than the last digit shown reads "<0.0001" rather than "0", so a dust
// balance is not mistaken for an empty one.
func FormatAmount(v *big.Int, decimals, maxFrac int) string {
	if v == nil {
		return "0"
	}
	neg := v.Sign() < 0
	abs := new(big.Int).Abs(v)
	if maxFrac < 0 {
		maxFrac = 0
	}
	if maxFrac > decimals {
		maxFrac = decimals
	}
	unit := pow10(decimals)
	q, r := new(big.Int).QuoRem(abs, unit, new(big.Int))
	var frac string
	if maxFrac < decimals {
		drop := pow10(decimals - maxFrac)
		qr, rr := new(big.Int).QuoRem(r, drop, new(big.Int))
		if new(big.Int).Lsh(rr, 1).Cmp(drop) >= 0 {
			qr.Add(qr, big.NewInt(1))
		}
		if qr.Cmp(pow10(maxFrac)) >= 0 {
			q.Add(q, big.NewInt(1))
			qr.SetInt64(0)
		}
		frac = leftPad(qr.String(), maxFrac)
	} else {
		frac = leftPad(r.String(), decimals)
	}
	frac = strings.TrimRight(frac, "0")
	if q.Sign() == 0 && frac == "" && abs.Sign() != 0 {
		dust := FormatUnits(pow10(decimals-maxFrac), decimals)
		if neg {
			return ">-" + dust
		}
		return "<" + dust
	}
	out := group(q.String())
	if frac != "" {
		out += "." + frac
	}
	return sign(neg, out)
}

// ParseUnits reads a decimal amount of a token with decimals places and
// returns its smallest unit: "1.5" at 6 decimals is 1500000. It accepts at
// most decimals fraction digits, and no sign or exponent.
//
// MaxTokenDecimals is the largest decimals value ParseUnits will expand.
// A multi-gigabyte repeat (decimals of 4e9) would exhaust the process, and
// a value past int range (1e300) overflows the decimals argument. 255 covers
// every asset this app formats.
const MaxTokenDecimals = 255

func ParseUnits(s string, decimals int) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || decimals < 0 || decimals > MaxTokenDecimals {
		return nil, false
	}
	whole, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		whole, frac = s[:i], s[i+1:]
		if strings.ContainsRune(frac, '.') {
			return nil, false
		}
	}
	if whole == "" && frac == "" {
		return nil, false
	}
	for _, part := range []string{whole, frac} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return nil, false
			}
		}
	}
	if len(frac) > decimals {
		return nil, false
	}
	digits := whole + frac + strings.Repeat("0", decimals-len(frac))
	if digits == "" {
		return nil, false
	}
	v, ok := new(big.Int).SetString(digits, 10)
	return v, ok
}

func leftPad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

func group(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	head := len(s) % 3
	if head > 0 {
		b.WriteString(s[:head])
	}
	for i := head; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func sign(neg bool, s string) string {
	if neg && s != "0" {
		return "-" + s
	}
	return s
}
