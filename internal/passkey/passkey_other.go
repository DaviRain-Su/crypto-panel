//go:build !darwin

package passkey

// info is the answer on platforms whose system authentication this app does
// not call yet.
func info() struct {
	available bool
	kind      Kind
} {
	return struct {
		available bool
		kind      Kind
	}{}
}

// Authenticate is unavailable where the app has no system check to call.
func Authenticate(string) error { return ErrUnsupported }
