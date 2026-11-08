//go:build darwin

package passkey

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// LocalAuthentication's LAPolicyDeviceOwnerAuthentication: Touch ID when it
// is enrolled, the login password otherwise.
const policyDeviceOwnerAuthentication = 2

var (
	once   sync.Once
	cached struct {
		available bool
		kind      Kind
	}

	selAlloc        = objc.RegisterName("alloc")
	selInit         = objc.RegisterName("init")
	selRelease      = objc.RegisterName("release")
	selCanEvaluate  = objc.RegisterName("canEvaluatePolicy:error:")
	selEvaluate     = objc.RegisterName("evaluatePolicy:localizedReason:reply:")
	selBiometryType = objc.RegisterName("biometryType")
	selString       = objc.RegisterName("stringWithUTF8String:")
	selCode         = objc.RegisterName("code")
	selDescription  = objc.RegisterName("localizedDescription")
	selUTF8         = objc.RegisterName("UTF8String")
)

// info loads LocalAuthentication once and asks what it can do. The answer is
// kept: the view asks on every frame, and the check does not change while
// the app runs.
func info() struct {
	available bool
	kind      Kind
} {
	once.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/LocalAuthentication.framework/LocalAuthentication", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return
		}
		cls := objc.GetClass("LAContext")
		if cls == 0 {
			return
		}
		ctx := newContext(objc.Class(cls))
		if ctx == 0 {
			return
		}
		defer ctx.Send(selRelease)
		var errPtr objc.ID
		if !objc.Send[bool](ctx, selCanEvaluate, uintptr(policyDeviceOwnerAuthentication), &errPtr) {
			return
		}
		cached.available = true
		switch objc.Send[uintptr](ctx, selBiometryType) {
		case 1:
			cached.kind = KindTouchID
		case 2:
			cached.kind = KindFaceID
		case 4:
			cached.kind = KindOpticID
		default:
			cached.kind = KindPassword
		}
	})
	return cached
}

func newContext(cls objc.Class) objc.ID {
	return objc.ID(cls).Send(selAlloc).Send(selInit)
}

// Authenticate shows the system's prompt with reason, and returns nil once
// the user confirms. It blocks until the prompt is answered or dismissed.
func Authenticate(reason string) error {
	if !Available() {
		return ErrUnsupported
	}
	cls := objc.GetClass("LAContext")
	if cls == 0 {
		return ErrUnsupported
	}
	ctx := newContext(objc.Class(cls))
	if ctx == 0 {
		return ErrUnsupported
	}
	defer ctx.Send(selRelease)
	reasonStr := objc.ID(objc.GetClass("NSString")).Send(selString, reason)
	if reasonStr == 0 {
		return errors.New("could not build the authentication prompt")
	}
	done := make(chan error, 1)
	block := objc.NewBlock(func(_ objc.Block, success bool, err objc.ID) {
		if success {
			done <- nil
			return
		}
		done <- authError(err)
	})
	ctx.Send(selEvaluate, uintptr(policyDeviceOwnerAuthentication), reasonStr, block)
	err := <-done
	block.Release()
	return err
}

// authError turns LocalAuthentication's NSError into the package's errors.
func authError(err objc.ID) error {
	if err == 0 {
		return errors.New("authentication failed")
	}
	switch objc.Send[int64](err, selCode) {
	case -2, -3, -4, -9: // user cancel, fallback, system cancel, app cancel
		return ErrCanceled
	case -5, -6, -7, -8: // passcode not set, biometry unavailable, not enrolled, locked out
		return ErrUnsupported
	}
	desc := objc.Send[string](objc.Send[objc.ID](err, selDescription), selUTF8)
	if desc == "" {
		return errors.New("authentication failed")
	}
	return fmt.Errorf("authentication failed: %s", desc)
}
