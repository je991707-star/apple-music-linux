//go:build windows

package export

import (
	"fmt"
	"os"
	"strings"
)

// checkToolTrust on Windows has no POSIX mode bits or uid to inspect; require a regular
// .exe file. Directory ACLs are the OS's concern.
func checkToolTrust(resolved string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || !strings.EqualFold(resolved[max(0, len(resolved)-4):], ".exe") {
		return fmt.Errorf("%s is not an executable file", resolved)
	}
	return nil
}
