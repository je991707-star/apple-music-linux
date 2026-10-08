package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func setRoots(t *testing.T) (config, cache string) {
	t.Helper()
	config, cache = t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("APPDATA", config)
	t.Setenv("LOCALAPPDATA", cache)
	resolved = map[bool]string{}
	return
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFreshInstallJustReturnsTheNewDirectory(t *testing.T) {
	config, cache := setRoots(t)
	if got, want := Config(), filepath.Join(config, "adam"); got != want {
		t.Fatalf("Config() = %q, want %q", got, want)
	}
	if got, want := Cache(), filepath.Join(cache, "adam"); got != want {
		t.Fatalf("Cache() = %q, want %q", got, want)
	}
}

func TestLegacyConfigDirectoriesAreMergedIntoTheNewOne(t *testing.T) {
	config, _ := setRoots(t)
	write(t, filepath.Join(config, "apple-music-linux", "electron-prefs.json"), "prefs")
	write(t, filepath.Join(config, "apple-music-linux", "widevine", "device_private_key"), "key")
	write(t, filepath.Join(config, "aml", "mv-dec.key"), "mvkey")
	write(t, filepath.Join(config, "aml", "eq.json"), "eq")

	dir := Config()
	for rel, want := range map[string]string{
		"electron-prefs.json":         "prefs",
		"widevine/device_private_key": "key",
		"mv-dec.key":                  "mvkey",
		"eq.json":                     "eq",
	} {
		if got := read(t, filepath.Join(dir, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	for _, old := range []string{"apple-music-linux", "aml"} {
		if _, err := os.Stat(filepath.Join(config, old)); !os.IsNotExist(err) {
			t.Errorf("legacy directory %s should be gone after a full merge", old)
		}
	}
}

func TestMergeNeverOverwritesAndKeepsWhatItCannotMove(t *testing.T) {
	config, _ := setRoots(t)
	write(t, filepath.Join(config, "adam", "eq.json"), "new")
	write(t, filepath.Join(config, "aml", "eq.json"), "old")
	write(t, filepath.Join(config, "aml", "hrtf.wav"), "wav")

	dir := Config()
	if got := read(t, filepath.Join(dir, "eq.json")); got != "new" {
		t.Errorf("existing file overwritten: %q", got)
	}
	if got := read(t, filepath.Join(dir, "hrtf.wav")); got != "wav" {
		t.Errorf("missing file not moved: %q", got)
	}
	if got := read(t, filepath.Join(config, "aml", "eq.json")); got != "old" {
		t.Errorf("conflicting legacy file must be left in place, got %q", got)
	}
}

func TestMergeRecursesIntoDirectoriesPresentOnBothSides(t *testing.T) {
	_, cache := setRoots(t)
	write(t, filepath.Join(cache, "aml", "engine", "mv-dec", "1.enc"), "a")
	write(t, filepath.Join(cache, "apple-music-linux", "engine", "mv-prefs.json"), "p")
	write(t, filepath.Join(cache, "apple-music-linux", "playback", "x.m4a"), "m")

	dir := Cache()
	for _, rel := range []string{"engine/mv-dec/1.enc", "engine/mv-prefs.json", "playback/x.m4a"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing after merge: %v", rel, err)
		}
	}
}

func TestResultIsCachedAndStable(t *testing.T) {
	setRoots(t)
	if a, b := Config(), Config(); a != b {
		t.Fatalf("Config() not stable: %q vs %q", a, b)
	}
}

func TestMoveInto(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "old", "f"), "1")
	MoveInto(filepath.Join(d, "old"), filepath.Join(d, "new"))
	if read(t, filepath.Join(d, "new", "f")) != "1" {
		t.Fatal("not moved")
	}
	write(t, filepath.Join(d, "old2", "f"), "2")
	write(t, filepath.Join(d, "new2", "f"), "3")
	MoveInto(filepath.Join(d, "old2"), filepath.Join(d, "new2"))
	if read(t, filepath.Join(d, "new2", "f")) != "3" {
		t.Fatal("must not overwrite an existing destination")
	}
}
