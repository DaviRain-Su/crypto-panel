package app

import (
	"os"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func TestMain(m *testing.M) {
	newPluginSecrets = func() plugin.Secrets {
		panic("test reached the real plugin keychain")
	}
	os.Exit(m.Run())
}
