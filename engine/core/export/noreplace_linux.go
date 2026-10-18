//go:build linux

package export

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/unix"
)

// renameNoReplace is an atomic rename that fails with fs.ErrExist instead of
// replacing dst (renameat2 RENAME_NOREPLACE). Filesystems or kernels without it,
// and moves across devices, report errNoReplaceUnsupported so the caller copies.
func renameNoReplace(src, dst string) error {
	err := unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST):
		return &fs.PathError{Op: "rename", Path: dst, Err: fs.ErrExist}
	case errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EXDEV),
		errors.Is(err, unix.EPERM), errors.Is(err, unix.EOPNOTSUPP):
		return errNoReplaceUnsupported
	}
	return err
}
