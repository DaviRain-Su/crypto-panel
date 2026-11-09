//go:build !unix

package acp

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}

func terminateProcess(cmd *exec.Cmd) {
	// 此平台仅保证回收直接子进程，不声称能够终止独立后代。
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
