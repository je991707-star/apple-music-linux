package corefp

import (
	"os"
	"path/filepath"
)

func defaultDLLPath() string {
	base := os.Getenv("PROGRAMFILES")
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, "Adam", "LibCoreFP", "CoreFP.dll")
}
