//go:build !windows

package diskcache

import "os"

// openShared is os.OpenFile: POSIX already lets an open file be renamed or deleted.
func openShared(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}
