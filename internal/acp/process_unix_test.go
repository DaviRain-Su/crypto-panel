//go:build unix

package acp

import (
	"context"
	"errors"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestOwnedProcessGroupCleanup(t *testing.T) {
	c := startFake(t, "descendant-parent", nil)
	init, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(init.AgentInfo.Version)
	if err != nil || childPID <= 0 {
		t.Fatalf("invalid descendant pid: %v", err)
	}
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("descendant never started: %v", err)
	}
	parentPID := c.cmd.Process.Pid
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		parentGone := errors.Is(syscall.Kill(parentPID, 0), syscall.ESRCH)
		childGone := errors.Is(syscall.Kill(childPID, 0), syscall.ESRCH)
		if parentGone && childGone {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("owned processes survived: parent gone=%v child gone=%v", parentGone, childGone)
		case <-tick.C:
		}
	}
}
