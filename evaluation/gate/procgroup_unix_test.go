//go:build unix

package gate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
)

// TestSuiteCancelKillsGroup shows cancelling a suite's run kills the whole of its process group:
// a fake uv that starts a grandchild, as uv starts the suite's Python and the suite its workers,
// leaves none of them running once the run's context is done.
func TestSuiteCancelKillsGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	exe := filepath.Join(dir, "uv")
	script := "#!/bin/sh\nsleep 300 &\necho $! > " + pidFile + ".tmp && mv " + pidFile + ".tmp " + pidFile + "\nwait\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- gate.UV{Exe: exe, Python: "python", Project: dir, Cache: dir}.Peak(ctx, gate.RoleVision, dir, filepath.Join(dir, "out"))
	}()

	pid := 0
	for deadline := time.Now().Add(10 * time.Second); pid == 0; {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake uv never started its grandchild")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled suite run returned no error")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the cancelled suite run didn't return")
	}
	// The grandchild, reparented once its parent is killed, is reaped by init; until then it is a
	// zombie, which is dead too.
	for deadline := time.Now().Add(5 * time.Second); alive(pid); {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the suite's grandchild %d still runs after the run was cancelled", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// alive reports whether pid is a running process, not gone and not a zombie.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return true
	}
	_, after, _ := strings.Cut(string(stat), ") ")
	return !strings.HasPrefix(after, "Z")
}
