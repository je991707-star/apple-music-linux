//go:build windows

package drm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var procLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

const (
	lockfileExclusiveLock   = 0x2
	lockfileFailImmediately = 0x1
)

// SessionLock is an exclusive lock over the shared session/credential directory,
// held for the engine's lifetime so two engines never own the same Apple session.
// On Windows it is a LockFileEx byte-range lock on a dedicated lockfile, released
// by the OS if the process dies.
type SessionLock struct {
	f    *os.File
	path string
}

// AcquireSessionLock takes an exclusive, non-blocking lock on dir. A nil dir is a no-op.
func AcquireSessionLock(dir string) (*SessionLock, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("session lock: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "engine-session.lock")
	// os.OpenFile on Windows shares read/write with other opens, so the owner pid stays readable.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session lock: open %s: %w", path, err)
	}
	// Lock a byte far past EOF: Windows locks are mandatory, and locking byte 0 would stop
	// other processes from reading the owner pid we write there.
	ol := syscall.Overlapped{OffsetHigh: 1}
	r, _, errno := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately,
		0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		ownerSuffix := ""
		if data, rerr := os.ReadFile(path); rerr == nil {
			if pid := strings.TrimSpace(string(data)); pid != "" {
				ownerSuffix = " (held by pid " + pid + ")"
			}
		}
		f.Close()
		return nil, fmt.Errorf("session already owned by another engine instance%s (%s): %w", ownerSuffix, path, errno)
	}
	_ = f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return &SessionLock{f: f, path: path}, nil
}

// Release unlocks and closes the lockfile. Safe to call on a nil lock.
func (l *SessionLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close() // closing the handle drops the lock
	l.f = nil
	return err
}
