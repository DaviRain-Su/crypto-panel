// Package passkey asks the operating system to authenticate the user before
// the wallet unlocks a key, reveals one, or signs. It is the platform's own
// check — Touch ID on a Mac — not a WebAuthn passkey: an EVM signature needs
// a secp256k1 key, which a passkey cannot hold. What a passkey gives a wallet
// is the system's confirmation that the person at the computer is its owner,
// and that is what this package asks for.
package passkey

import "errors"

// ErrUnsupported reports a platform or a Mac without a way to authenticate
// the user.
var ErrUnsupported = errors.New("system authentication is not available")

// ErrCanceled reports that the user dismissed the prompt.
var ErrCanceled = errors.New("authentication was canceled")

// Kind is the check the platform offers.
type Kind int

const (
	KindNone Kind = iota
	KindTouchID
	KindFaceID
	KindOpticID
	// KindPassword is the system asking for the login password, for a Mac
	// without biometrics.
	KindPassword
)

// Available reports whether the platform can authenticate the user now.
func Available() bool { return info().available }

// Check returns the kind of check the platform offers.
func Check() Kind { return info().kind }
