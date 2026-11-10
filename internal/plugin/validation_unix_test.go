//go:build unix

package plugin

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestPackageHashRejectsFIFONamedLikeShellFile(t *testing.T) {
	s, m := newTestManager(t, nil)
	installed := installTest(t, m, personalPackage(t, "local.personal.gas", "1.0.0"))
	if err := syscall.Mkfifo(filepath.Join(s.Dir(), installed.PluginID, "kv.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PackageHash(installed.PluginID); err == nil {
		t.Fatal("名为壳文件的 FIFO 通过了摘要")
	}
}
