//go:build !windows

package export

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// checkToolTrust requires a regular, executable file owned by root or the current user,
// not writable by anyone else, in a directory nobody else can write to.
func checkToolTrust(resolved string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not an executable file", resolved)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by other users", resolved)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != 0 && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is owned by another user", resolved)
	}
	dir, err := os.Stat(filepath.Dir(resolved))
	if err != nil {
		return err
	}
	if dir.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("%s is in a directory writable by other users", resolved)
	}
	return nil
}
