package diskcache

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFilenameStaysInsideDir(t *testing.T) {
	c, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../../etc/passwd", "..", ".", "a/b", `a\b`, "a\x00b", "../x", "/abs", "", "ünï"} {
		for _, q := range []string{"alac", "../q", ""} {
			name := c.filename(id, q)
			if strings.ContainsAny(name, `/\`) || strings.Contains(name, "\x00") {
				t.Errorf("filename(%q,%q) = %q contains a path separator or NUL", id, q, name)
			}
			p := filepath.Join(c.dir, name)
			if filepath.Dir(p) != filepath.Clean(c.dir) {
				t.Errorf("filename(%q,%q) escapes the cache dir: %s", id, q, p)
			}
		}
	}
}
