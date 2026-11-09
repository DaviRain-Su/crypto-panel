//go:build !unix

package plugin

import "os"

func openInstallRecord(path string) (*os.File, error) {
	return os.Open(path)
}
