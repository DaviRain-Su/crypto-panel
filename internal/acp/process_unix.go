//go:build unix

package acp

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	// 新建进程组；后续只终止本模块拥有的组，不按名称查杀其他 agent。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
