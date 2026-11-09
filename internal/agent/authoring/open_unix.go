//go:build unix

package authoring

import (
	"os"
	"syscall"
)

// 拒绝最后一段链接；非阻塞打开后再检查类型，避免并发换成 FIFO 时卡住。
func openCodeFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
