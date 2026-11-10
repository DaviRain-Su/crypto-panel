//go:build unix

package plugin

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestFIFOSentinelIsNotAbsent(t *testing.T) {
	prev := createMigrationSentinelFile
	createMigrationSentinelFile = func(string) error { return nil }
	t.Cleanup(func() { createMigrationSentinelFile = prev })
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	id := "local.personal.fifosentinel"
	plantLoose(t, s.Dir(), id)
	path, err := s.migrationSentinelPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	exists, serr := s.migrationSentinelExists()
	if exists || serr == nil {
		t.Fatalf("管道哨兵被当成不存在: exists=%v err=%v", exists, serr)
	}
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	if _, found, _ := s.getProvenance(id); found {
		t.Fatal("管道哨兵被当成不存在后迁移了 install.json")
	}
}

func TestFIFOSentinelKeepsTheAppConfirmable(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	s := NewStore(filepath.Join(t.TempDir(), "plugins"), sec)
	id := "local.personal.fifoconfirm"
	plantLoose(t, s.Dir(), id)
	path, err := s.migrationSentinelPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	confirmListedApp(t, s, id)
}
