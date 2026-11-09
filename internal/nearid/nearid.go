// Package nearid validates NEAR account ids, which the watch list, the
// wallet and the transaction builder all read.
package nearid

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidAccount reports a string that is not a NEAR account id.
var ErrInvalidAccount = errors.New("not a valid NEAR account id")

// ParseAccountID validates a NEAR account id: 2 to 64 lowercase characters,
// digits, '-' and '_', with '.' separating parts that cannot start or end
// with '-' or '_'. An implicit account is the 64 hex digits of its public
// key.
func ParseAccountID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || len(s) > 64 {
		return "", fmt.Errorf("%w: want 2 to 64 characters", ErrInvalidAccount)
	}
	if s != strings.ToLower(s) {
		return "", fmt.Errorf("%w: account ids are lowercase", ErrInvalidAccount)
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") {
		return "", fmt.Errorf("%w: a dot must separate two parts", ErrInvalidAccount)
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return "", fmt.Errorf("%w: a dot must separate two parts", ErrInvalidAccount)
		}
		if strings.HasPrefix(part, "-") || strings.HasPrefix(part, "_") ||
			strings.HasSuffix(part, "-") || strings.HasSuffix(part, "_") {
			return "", fmt.Errorf("%w: a part cannot start or end with '-' or '_'", ErrInvalidAccount)
		}
		for _, r := range part {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				continue
			}
			return "", fmt.Errorf("%w: %q is not an account id character", ErrInvalidAccount, r)
		}
	}
	return s, nil
}

// IsImplicit reports whether s is an implicit account id: 64 lowercase hex
// digits, which is how NEAR names an account that is only a public key.
func IsImplicit(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// IsDeterministic reports whether s is a NEP-616 deterministic account id:
// "0s" followed by 40 lowercase hex digits. The id is derived from the
// account's StateInit, so a passkey wallet and its address are one thing.
func IsDeterministic(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0s") {
		return false
	}
	for _, c := range s[2:] {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}
