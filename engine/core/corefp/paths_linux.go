package corefp

import (
	"os"
	"path/filepath"
)

func defaultDLLPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/root", ".config", "Adam", "LibCoreFP", "CoreFP.dll")
	}
	return filepath.Join(home, ".config", "Adam", "LibCoreFP", "CoreFP.dll")
}
