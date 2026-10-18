//go:build !windows

package export

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"engine/internal/pq"
)

func needTool(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available", name)
	}
	return p
}

// A cancelled job ends its tool at once (SIGTERM).
func TestToolCommandStopsOnCancel(t *testing.T) {
	sleep := needTool(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := toolCommand(ctx, sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_ = cmd.Wait()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("tool kept running %v after cancel", d)
	}
}

// A tool that ignores SIGTERM is killed after the grace period.
func TestToolCommandKillsAToolIgnoringTerm(t *testing.T) {
	sh := needTool(t, "sh")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := toolCommand(ctx, sh, "-c", "trap '' TERM; exec sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the trap take effect
	cancel()
	start := time.Now()
	_ = cmd.Wait()
	if d := time.Since(start); d > toolStopGrace+time.Second {
		t.Fatalf("tool survived %v after cancel", d)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("tool process still alive")
	}
}

// Stop cancels running work, which ends its tool, and waits for the work to finish.
func TestStopJoinsRunningWorkAndItsTool(t *testing.T) {
	sleep := needTool(t, "sleep")
	m := &Manager{
		jobs:     make(map[string]*ExportJob),
		requests: make(map[string]ExportRequest),
		queue:    pq.New[*workItem](),
		bw:       newBWController(nil, 0),
	}
	started, finished := make(chan struct{}), make(chan struct{})
	m.run = func(item *workItem) {
		cmd := toolCommand(item.ctx, sleep, "30")
		if err := cmd.Start(); err != nil {
			t.Error(err)
			return
		}
		close(started)
		_ = cmd.Wait()
		close(finished)
	}
	m.active.Add(1)
	go func() { defer m.active.Done(); m.worker() }()
	if _, err := m.Enqueue(ExportRequest{AssetID: "1", OutputDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	<-started
	start := time.Now()
	m.Stop()
	select {
	case <-finished:
	default:
		t.Fatal("Stop returned while the export's tool was still running")
	}
	if d := time.Since(start); d > stopWait {
		t.Fatalf("Stop took %v", d)
	}
}

func TestMoveNoReplaceNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "a.m4a")
	os.WriteFile(dst, []byte("existing"), 0o644)
	src := filepath.Join(dir, ".tmp")
	os.WriteFile(src, []byte("new"), 0o644)
	if err := moveNoReplace(src, dst); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("want ErrExist, got %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "existing" {
		t.Fatalf("existing file replaced: %q", b)
	}
	free := filepath.Join(dir, "b.m4a")
	if err := moveNoReplace(src, free); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(free); string(b) != "new" {
		t.Fatalf("moved content %q", b)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source must be gone after the move")
	}
}

// The copy fallback (no atomic rename on this filesystem) claims the name too.
func TestCopyIntoNewNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "a.m4a")
	os.WriteFile(dst, []byte("existing"), 0o644)
	src := filepath.Join(dir, ".tmp")
	os.WriteFile(src, []byte("new"), 0o644)
	if err := copyIntoNew(src, dst); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("want ErrExist, got %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "existing" {
		t.Fatalf("existing file replaced: %q", b)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("source must be kept when the name is taken")
	}
}

func TestToolTrustRejectsGroupWritableDir(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "ffmpeg")
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)
	info, _ := os.Stat(tool)
	if err := checkToolTrust(tool, info); err != nil {
		t.Fatalf("a private directory must be trusted: %v", err)
	}
	if err := os.Chmod(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := checkToolTrust(tool, info); err == nil {
		t.Fatal("a group-writable directory must not be trusted")
	}
}
