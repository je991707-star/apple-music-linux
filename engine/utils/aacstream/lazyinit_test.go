package aacstream

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Importing the package must not create directories or read the user's files. The
// check runs this test binary again with an empty cache/config home: its init functions
// run before any test, so anything they create shows up there.
func TestImportHasNoFilesystemSideEffects(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperDoesNothing$")
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"), "AACSTREAM_HELPER=noop")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("importing aacstream created %v under the user's home", names)
	}
}

func TestHelperDoesNothing(t *testing.T) {
	if os.Getenv("AACSTREAM_HELPER") == "" {
		t.Skip("helper for TestImportHasNoFilesystemSideEffects")
	}
}
