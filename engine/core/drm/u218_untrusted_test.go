package drm

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func u218Manager(t *testing.T) *DRMManager {
	t.Helper()
	b := &u213ManagerStub{crashStub: crashStub{events: make(chan DRMEvent)}, start: func(context.Context) error { return nil }}
	cfg := BackendConfig{BaseDir: t.TempDir()}
	// Reflection lets these behavior tests run unchanged on the original source.
	if f := reflect.ValueOf(&cfg).Elem().FieldByName("RequireTrustedCompletion"); f.IsValid() {
		f.SetBool(true)
	}
	m := NewDRMManager(b, NewSessionManager(cfg.BaseDir), nil, cfg, DefaultRestartPolicy)
	t.Cleanup(func() { m.shutdownStop(); close(b.events) })
	return m
}
func TestU218NilStartIsNotNativeCompletion(t *testing.T) {
	m := u218Manager(t)
	if err := m.Authenticate(context.Background(), Credentials{Email: "synthetic-account", Password: "synthetic-password"}); err != nil {
		t.Fatal(err)
	}
	if m.Status().State.Authentication == AuthLoggedIn {
		t.Fatal("nil Start result published unqualified LoggedIn")
	}
}
func TestU218BackendStartedIsNotNativeCompletion(t *testing.T) {
	m := u218Manager(t)
	m.consumeBackendEvent(DRMEvent{Snapshot: DRMSnapshot{State: DRMState{Process: ProcessRunning, Authentication: AuthLoggedIn, FairPlay: FairPlayReady, Session: SessionValid}}})
	if s := m.Status(); s.State.Authentication == AuthLoggedIn || s.Capabilities.CBCS {
		t.Fatal("backend lifecycle event published unqualified account authority")
	}
}
func TestU218DiskMarkerIsNotNativeCompletion(t *testing.T) {
	m := u218Manager(t)
	dir := m.cfg.BaseDir
	if err := os.MkdirAll(filepath.Join(dir, "mpl_db"), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "mpl_db/accounts.sqlitedb"), []byte("synthetic-empty-marker"), 0600)
	os.WriteFile(filepath.Join(dir, "session-ready"), []byte("1"), 0600)
	b := &crashStub{events: make(chan DRMEvent)}
	again := NewDRMManager(b, NewSessionManager(dir), nil, m.cfg, DefaultRestartPolicy)
	t.Cleanup(func() { again.shutdownStop(); close(b.events) })
	if again.Status().State.Authentication == AuthLoggedIn {
		t.Fatal("disk marker was promoted to Native authentication")
	}
}
