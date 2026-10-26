//go:build windows

package diskcache

import (
	"os"
	"syscall"
)

// openShared opens a cache file the way POSIX does: any handle to it, ours included, may still be renamed or
// deleted. Go's os.OpenFile on Windows omits FILE_SHARE_DELETE, so renaming a file that still has an open
// handle (the streaming writer's own, or a reader's) fails with a sharing violation, and no track streamed to
// a client could ever be committed to the cache.
func openShared(name string, flag int, _ os.FileMode) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	var access uint32
	switch flag & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR) {
	case os.O_WRONLY:
		access = syscall.GENERIC_WRITE
	case os.O_RDWR:
		access = syscall.GENERIC_READ | syscall.GENERIC_WRITE
	default:
		access = syscall.GENERIC_READ
	}
	var disposition uint32
	switch {
	case flag&os.O_CREATE != 0 && flag&os.O_TRUNC != 0:
		disposition = syscall.CREATE_ALWAYS
	case flag&os.O_CREATE != 0:
		disposition = syscall.OPEN_ALWAYS
	case flag&os.O_TRUNC != 0:
		disposition = syscall.TRUNCATE_EXISTING
	default:
		disposition = syscall.OPEN_EXISTING
	}
	share := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, err := syscall.CreateFile(p, access, share, nil, disposition, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}
