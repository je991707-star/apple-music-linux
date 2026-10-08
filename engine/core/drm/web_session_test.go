package drm

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionFallsBackToTheWebToken(t *testing.T) {
	dir := t.TempDir()
	s := NewSessionManager(dir)
	if s.HasSession() || s.ReadMusicToken() != "" {
		t.Fatal("an empty profile has no session")
	}

	tok := ""
	s.SetWebSession(func() (string, string) { return tok, "us" })
	if s.HasSession() {
		t.Fatal("no token yet, so still no session")
	}
	tok = "web-token"
	if !s.HasSession() || s.ReadMusicToken() != "web-token" || s.ReadStorefrontID() != "us" {
		t.Fatalf("web token must count as a session: has=%v token=%q sf=%q", s.HasSession(), s.ReadMusicToken(), s.ReadStorefrontID())
	}

	// Credential files, when present, still win over the web token.
	os.WriteFile(filepath.Join(dir, "MUSIC_TOKEN"), []byte("file-token\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "STOREFRONT_ID"), []byte("143467-2,31\n"), 0o600)
	if s.ReadMusicToken() != "file-token" || s.ReadStorefrontID() != "143467-2,31" {
		t.Fatalf("files must take precedence: %q %q", s.ReadMusicToken(), s.ReadStorefrontID())
	}
}

func TestLegacySessionDatabaseStillCounts(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "mpl_db"), 0o700)
	os.WriteFile(filepath.Join(dir, "mpl_db", "accounts.sqlitedb"), []byte("x"), 0o600)
	if !NewSessionManager(dir).HasSession() {
		t.Fatal("an existing login database must keep working exactly as before")
	}
}

type startCounter struct {
	crashStub
	starts atomic.Int32
}

func (c *startCounter) Start(context.Context, BackendConfig) error { c.starts.Add(1); return nil }

func newWebManager(t *testing.T, tok *atomic.Pointer[string]) (*DRMManager, *startCounter) {
	t.Helper()
	stub := &startCounter{crashStub: crashStub{events: make(chan DRMEvent)}}
	m := NewDRMManager(stub, NewSessionManager(t.TempDir()), nil, BackendConfig{}, RestartPolicy{StartupTimeout: time.Second})
	m.SetWebSession(func() (string, string) {
		if p := tok.Load(); p != nil {
			return *p, ""
		}
		return "", ""
	})
	return m, stub
}

func TestInitialStatusIsStoppedNotUnknown(t *testing.T) {
	var tok atomic.Pointer[string]
	m, _ := newWebManager(t, &tok)
	st := m.Status().State
	if st.Process != ProcessStopped {
		t.Errorf("an idle, lazily started backend is stopped, got %v", st.Process)
	}
	if st.Session != SessionEmpty || st.Authentication != AuthLoggedOut {
		t.Errorf("no token yet: want empty/logged_out, got %v/%v", st.Session, st.Authentication)
	}
}

func TestWebTokenStartsTheBackendAndReportsTheSession(t *testing.T) {
	var tok atomic.Pointer[string]
	m, stub := newWebManager(t, &tok)

	m.NoteWebSession() // no token: nothing to report and nothing to start
	time.Sleep(50 * time.Millisecond)
	if stub.starts.Load() != 0 || m.Status().State.Session != SessionEmpty {
		t.Fatal("without a token the backend must stay stopped and the session empty")
	}

	v := "web-token"
	tok.Store(&v)
	m.NoteWebSession()
	deadline := time.Now().Add(2 * time.Second)
	for stub.starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if stub.starts.Load() != 1 {
		t.Fatalf("the backend must start once the web token appears, starts=%d", stub.starts.Load())
	}
	st := m.Status().State
	if st.Session != SessionValid || st.Authentication != AuthLoggedIn {
		t.Errorf("want valid/logged_in, got %v/%v", st.Session, st.Authentication)
	}
}

func TestNoteWebSessionAfterShutdownDoesNothing(t *testing.T) {
	var tok atomic.Pointer[string]
	m, stub := newWebManager(t, &tok)
	v := "t"
	tok.Store(&v)
	m.Shutdown()
	m.NoteWebSession()
	time.Sleep(50 * time.Millisecond)
	if stub.starts.Load() != 0 {
		t.Fatal("a closed manager must not start the backend")
	}
}
