package export

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTmp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMoveNoClobber_SkipKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	final := writeTmp(t, dir, "a.m4a", "old")
	tmp := writeTmp(t, dir, ".tmp", "new")
	got, err := moveNoClobber(tmp, final, "skip")
	if err != nil || got != final {
		t.Fatalf("got %q err=%v", got, err)
	}
	if read(t, final) != "old" {
		t.Fatal("skip policy overwrote an existing file")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestMoveNoClobber_RenamePicksFreeName(t *testing.T) {
	dir := t.TempDir()
	final := writeTmp(t, dir, "a.m4a", "one")
	writeTmp(t, dir, "a (1).m4a", "two")
	tmp := writeTmp(t, dir, ".tmp", "three")
	got, err := moveNoClobber(tmp, final, "rename")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "a (2).m4a" || read(t, got) != "three" {
		t.Fatalf("got %q", got)
	}
	if read(t, final) != "one" {
		t.Fatal("existing file clobbered")
	}
}

func TestMoveNoClobber_OverwriteReplaces(t *testing.T) {
	dir := t.TempDir()
	final := writeTmp(t, dir, "a.m4a", "old")
	tmp := writeTmp(t, dir, ".tmp", "new")
	if _, err := moveNoClobber(tmp, final, "overwrite"); err != nil {
		t.Fatal(err)
	}
	if read(t, final) != "new" {
		t.Fatal("overwrite did not replace")
	}
}

func TestMoveNoClobber_NewFile(t *testing.T) {
	dir := t.TempDir()
	tmp := writeTmp(t, dir, ".tmp", "x")
	final := filepath.Join(dir, "fresh.m4a")
	got, err := moveNoClobber(tmp, final, "")
	if err != nil || got != final || read(t, final) != "x" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestCopyFileFailureLeavesDestinationIntact(t *testing.T) {
	dir := t.TempDir()
	dst := writeTmp(t, dir, "keep.m4a", "precious")
	if err := copyFile(filepath.Join(dir, "does-not-exist"), dst); err == nil {
		t.Fatal("expected error")
	}
	if read(t, dst) != "precious" {
		t.Fatal("failed copy damaged the destination")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
}

func TestMoveNoClobber_RenameGivesUpAfterTheAttemptBound(t *testing.T) {
	old := maxRenameAttempts
	maxRenameAttempts = 3
	defer func() { maxRenameAttempts = old }()
	dir := t.TempDir()
	final := writeTmp(t, dir, "a.m4a", "0")
	writeTmp(t, dir, "a (1).m4a", "1")
	writeTmp(t, dir, "a (2).m4a", "2")
	tmp := writeTmp(t, dir, ".tmp", "new")
	if _, err := moveNoClobber(tmp, final, "rename"); err == nil {
		t.Fatal("expected an error once every candidate name is taken")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("the temp file must not be left behind on failure")
	}
	for name, want := range map[string]string{"a.m4a": "0", "a (1).m4a": "1", "a (2).m4a": "2"} {
		if read(t, filepath.Join(dir, name)) != want {
			t.Errorf("%s was modified", name)
		}
	}
}
