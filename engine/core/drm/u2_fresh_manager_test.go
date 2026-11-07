package drm

import (
	"context"
	"errors"
	"testing"
)

type u213ManagerStub struct {
	crashStub
	start   func(context.Context) error
	stopErr error
}

func (b *u213ManagerStub) Start(ctx context.Context, _ BackendConfig) error { return b.start(ctx) }
func (b *u213ManagerStub) Stop() error                                      { return b.stopErr }
func (b *u213ManagerStub) Running() bool                                    { return true }
func u213Manager(t *testing.T) (*DRMManager, *u213ManagerStub) {
	t.Helper()
	b := &u213ManagerStub{crashStub: crashStub{events: make(chan DRMEvent)}, start: func(context.Context) error { return nil }}
	m := NewDRMManager(b, NewSessionManager(t.TempDir()), nil, BackendConfig{}, DefaultRestartPolicy)
	t.Cleanup(func() { m.shutdownStop(); close(b.events) })
	return m, b
}
func TestU213ManagerCancelledCompletionCannotLogin(t *testing.T) {
	m, b := u213Manager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.start = func(context.Context) error { cancel(); return nil }
	err := m.Authenticate(ctx, Credentials{Email: "synthetic-B", Password: "mock"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Start accepted: %v", err)
	}
	if m.Status().State.Authentication == AuthLoggedIn {
		t.Error("cancelled completion published LoggedIn")
	}
}
func TestU213ManagerFreshAttemptClearsOldAuthority(t *testing.T) {
	m, b := u213Manager(t)
	m.mergeAndEmit(DRMSnapshot{State: DRMState{Authentication: AuthLoggedIn, Session: SessionValid, FairPlay: FairPlayReady}})
	b.start = func(context.Context) error {
		s := m.Status()
		if s.State.Authentication == AuthLoggedIn || s.Capabilities.CBCS || s.State.Session == SessionValid {
			t.Error("A authority retained while B authenticates")
		}
		return errors.New("synthetic rejected B")
	}
	if err := m.Authenticate(context.Background(), Credentials{Email: "synthetic-B", Password: "mock"}); err == nil {
		t.Fatal("B accepted")
	}
	if s := m.Status(); s.State.Authentication == AuthLoggedIn || s.Capabilities.CBCS || s.State.Session == SessionValid {
		t.Error("failed B retained A authority")
	}
}
func TestU213ManagerStopFailurePreventsFreshStart(t *testing.T) {
	m, b := u213Manager(t)
	b.stopErr = errors.New("synthetic drain failure")
	called := false
	b.start = func(context.Context) error { called = true; return nil }
	if err := m.Authenticate(context.Background(), Credentials{Email: "synthetic-B", Password: "mock"}); err == nil {
		t.Error("Stop failure ignored")
	}
	if called {
		t.Error("native Start overlapped failed Stop")
	}
}

func TestU213ManagerSignOutGateRejectsCompletion(t *testing.T) {
	m, b := u213Manager(t)
	b.start = func(context.Context) error { m.gateMu.Lock(); m.gate = gateSigningOut; m.gateMu.Unlock(); return nil }
	if err := m.Authenticate(context.Background(), Credentials{Email: "synthetic-B", Password: "mock"}); !errors.Is(err, ErrLogoutPending) {
		t.Fatalf("closed sign-out gate accepted completion: %v", err)
	}
	if m.Status().State.Authentication == AuthLoggedIn {
		t.Fatal("sign-out superseded by completion")
	}
}

func TestU213ManagerShutdownRejectsLateCompletion(t *testing.T) {
	m, b := u213Manager(t)
	b.start = func(context.Context) error { m.Shutdown(); return nil }
	if err := m.Authenticate(context.Background(), Credentials{Email: "synthetic-B", Password: "mock"}); err == nil {
		t.Fatal("shutdown allowed late authentication")
	}
	if got := m.Status(); got.State.Authentication == AuthLoggedIn || got.State.Manager != ManagerShuttingDown {
		t.Fatal("late completion revived shutdown manager")
	}
}
