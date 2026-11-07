//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestU216PrivateFileModes(t *testing.T) {
	if os.Getenv("U216_PERMISSION_CHILD") == "1" {
		if err := restrictScopedFiles(); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "native-style-db")
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		st, _ := os.Stat(p)
		if st.Mode().Perm() != 0600 {
			t.Fatalf("native-style file permissions: %o", st.Mode().Perm())
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestU216PrivateFileModes$")
	cmd.Env = append(os.Environ(), "U216_PERMISSION_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated permission policy: %v %s", err, out)
	}
}
