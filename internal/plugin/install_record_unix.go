//go:build unix

package plugin

import (
	"os"
	"syscall"
)

func openInstallRecord(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
