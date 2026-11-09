package app

import (
	"errors"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/passkey"
)

// The wallet's second factor is the operating system's own check: Touch ID
// on a Mac. It guards unlocking an account, revealing a key and every
// signature. The check is not a passkey — an EVM key is secp256k1, which a
// passkey cannot hold — but it is the part of a passkey that protects a
// wallet: the system confirms the person at the computer before a key is
// used.

// presenceRequired reports whether the system must confirm the user before
// a key is unlocked, revealed or used.
func (a *App) presenceRequired() bool {
	return a.passkeyKind != passkey.KindNone && !a.settings.DisablePresence
}

// authName is the check's name in the user's language.
func (a *App) authName() string {
	switch a.passkeyKind {
	case passkey.KindTouchID:
		return a.t(i18n.AuthTouchID)
	case passkey.KindFaceID:
		return a.t(i18n.AuthFaceID)
	case passkey.KindOpticID:
		return a.t(i18n.AuthOpticID)
	default:
		return a.t(i18n.AuthSystem)
	}
}

// authFailure turns an authentication error into the sentence the page
// shows.
func (a *App) authFailure(err error) string {
	switch {
	case errors.Is(err, passkey.ErrCanceled):
		return a.t(i18n.AuthCanceled)
	case errors.Is(err, passkey.ErrUnsupported):
		return a.t(i18n.AuthUnavailable)
	default:
		return a.t(i18n.AuthFailed, err)
	}
}

// setPresenceRequired turns the system check on or off.
func (a *App) setPresenceRequired(on bool) {
	if a.settings.DisablePresence == !on {
		return
	}
	a.settings.DisablePresence = !on
	if !on {
		a.authBusy = ""
	}
	a.save()
	a.invalidate()
}

// lockIfIdle forgets unlocked keys after five minutes without input. The
// refresh loop calls it on the main thread.
func (a *App) lockIfIdle() {
	if a.walletUnlocked() == 0 {
		return
	}
	if mygo.Power.IdleTime() < 5*time.Minute {
		return
	}
	a.lockAll()
}

// authGate asks the system to confirm the user, then runs fn on the main
// thread. Without a required check fn runs at once. The prompt runs on its
// own goroutine, so the window keeps drawing; busyKey marks the account
// whose prompt is up, and a lock or another action clears it, which drops
// the answer.
func (a *App) authGate(reason, busyKey string, onFail func(string), fn func()) {
	if !a.presenceRequired() || a.authenticate == nil {
		fn()
		return
	}
	a.authBusy = busyKey
	a.invalidate()
	authenticate := a.authenticate
	a.goAsync(func() {
		err := authenticate(reason)
		a.runOnMain(func() {
			if a.authBusy != busyKey {
				return
			}
			a.authBusy = ""
			if err != nil {
				onFail(a.authFailure(err))
				a.invalidate()
				return
			}
			fn()
		})
	})
}
