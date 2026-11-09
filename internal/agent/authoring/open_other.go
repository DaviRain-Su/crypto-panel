//go:build !unix

package authoring

import "os"

// 非 Unix 平台仍由 os.Root 约束范围，并在读取内容前复核描述符类型与身份。
func openCodeFile(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
