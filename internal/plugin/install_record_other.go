//go:build !unix

package plugin

import "os"

func openInstallRecord(path string) (*os.File, error) {
	return os.Open(path)
}

func installIdentity(info os.FileInfo) (dev, ino, nlink uint64, ok bool) {
	return 0, 0, 1, true
}
