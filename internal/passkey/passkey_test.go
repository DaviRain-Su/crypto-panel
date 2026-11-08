package passkey

import "testing"

// TestAvailable asks the platform what it offers. It never prompts: the
// check is the non-interactive canEvaluatePolicy.
func TestAvailable(t *testing.T) {
	t.Logf("available=%v kind=%v", Available(), Check())
	if Available() && Check() == KindNone {
		t.Fatal("authentication is available but the check is unknown")
	}
	if !Available() && Check() != KindNone {
		t.Fatal("authentication is unavailable but a check was named")
	}
}
