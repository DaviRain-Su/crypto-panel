//go:build unix

package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyRejectsSpecialFilesWithoutOpening(t *testing.T) {
	for _, name := range []string{"pipe", "state.json"} {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			if err := syscall.Mkfifo(filepath.Join(src, name), 0600); err != nil {
				t.Fatal(err)
			}
			if err := copyExisting(context.Background(), src, t.TempDir()); !errors.Is(err, ErrSource) {
				t.Fatalf("接受了特殊文件: %v", err)
			}
		})
	}
}

func TestOpenCodeFileDoesNotFollowLinksOrBlockOnFIFO(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("not copied"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if f, err := openCodeFile(root, "link"); err == nil {
		f.Close()
		t.Fatal("读取时跟随链接")
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := root.Lstat("pipe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readCodeFile(context.Background(), root, "pipe", info); !errors.Is(err, ErrSource) {
		t.Fatalf("读取时未拒绝FIFO: %v", err)
	}
}
