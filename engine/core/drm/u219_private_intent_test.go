package drm

import (
	"context"
	"testing"
	"time"
)

func TestU219IntentRegistersWithoutNative(t *testing.T) {
	m := u218Manager(t)
	calls := 0
	m.backend.(*u213ManagerStub).start = func(context.Context) error { calls++; return nil }
	b := u218Binding()
	s, e := m.BeginPrivateIntent(context.Background(), b)
	if e != nil || s.State.Authentication != AuthAccepted || s.NativeAuthenticated {
		t.Fatalf("begin %+v %v", s, e)
	}
	s, e = m.PrivateIntentStatus(context.Background(), b, true)
	if e != nil || s.State.Authentication != AuthLoggingIn || s.NativeAuthenticated || calls != 0 {
		t.Fatalf("status %+v %v starts=%d", s, e, calls)
	}
	if _, e = m.BeginPrivateIntent(context.Background(), b); e == nil {
		t.Fatal("replay accepted")
	}
}
func TestU219CancelBeforeBeginTombstone(t *testing.T) {
	m := u218Manager(t)
	b := u218Binding()
	s, e := m.EndPrivateIntent(context.Background(), b, AuthCancelled)
	if e != nil || s.State.Authentication != AuthCancelled {
		t.Fatal(s, e)
	}
	if _, e = m.BeginPrivateIntent(context.Background(), b); e == nil {
		t.Fatal("late begin reopened cancel")
	}
	m.consumeBackendEvent(DRMEvent{Snapshot: DRMSnapshot{State: DRMState{Authentication: AuthLoggingIn}}})
	if m.Status().State.Authentication != AuthCancelled {
		t.Fatal("late running revived attempt")
	}
}
func TestU219ScopedStatusRejectsForeignIdentity(t *testing.T) {
	for _, field := range []string{"account", "epoch", "attempt", "owner", "boot", "nonce", "revision", "generation", "deadline"} {
		t.Run(field, func(t *testing.T) {
			m := u218Manager(t)
			b := u218Binding()
			if _, e := m.BeginPrivateIntent(context.Background(), b); e != nil {
				t.Fatal(e)
			}
			bad := b
			switch field {
			case "account":
				bad.AccountID = "B"
			case "epoch":
				bad.AccountEpoch++
			case "attempt":
				bad.AttemptID = "B"
			case "owner":
				bad.EngineOwnerID = "B"
			case "boot":
				bad.EngineBoot = "bad"
			case "nonce":
				bad.BootNonce = "bad"
			case "revision":
				bad.CredentialRevision++
			case "generation":
				bad.Generation++
			case "deadline":
				bad.ExpiresAt++
			}
			if _, e := m.PrivateIntentStatus(context.Background(), bad, true); e == nil {
				t.Fatal("foreign status accepted")
			}
			if _, e := m.EndPrivateIntent(context.Background(), bad, AuthRevoked); e == nil {
				t.Fatal("foreign revoke accepted")
			}
			s, e := m.PrivateIntentStatus(context.Background(), b, false)
			if e != nil || s.State.Authentication != AuthAccepted {
				t.Fatal("foreign reply affected current", s, e)
			}
		})
	}
}
func TestU219IntentExpiresWithoutSuccess(t *testing.T) {
	m := u218Manager(t)
	b := u218Binding()
	b.ExpiresAt = time.Now().Add(15 * time.Millisecond).UnixMilli()
	if _, e := m.BeginPrivateIntent(context.Background(), b); e != nil {
		t.Fatal(e)
	}
	time.Sleep(25 * time.Millisecond)
	s, e := m.PrivateIntentStatus(context.Background(), b, true)
	if e != nil || s.NativeAuthenticated || s.State.Authentication != AuthExpired {
		t.Fatal(s, e)
	}
}
func TestU219RevokeAndShutdownAbsorbLateWork(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		m := u218Manager(t)
		b := u218Binding()
		m.BeginPrivateIntent(context.Background(), b)
		if shutdown {
			m.Shutdown()
		} else {
			m.EndPrivateIntent(context.Background(), b, AuthRevoked)
		}
		s, e := m.PrivateIntentStatus(context.Background(), b, true)
		if e != nil || s.NativeAuthenticated || s.State.Authentication != AuthRevoked {
			t.Fatal(s, e)
		}
	}
}
func TestU219NoProductionCompletionProvider(t *testing.T) {
	m := u218Manager(t)
	b := u218Binding()
	m.BeginPrivateIntent(context.Background(), b)
	if m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic"}) == nil {
		t.Fatal("unqualified producer unlocked account")
	}
}

func TestU219CancelledRequestCannotRegisterIntent(t *testing.T) {
	m := u218Manager(t)
	b := u218Binding()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := m.EndPrivateIntent(ctx, b, AuthCancelled); e == nil {
		t.Fatal("cancelled request admitted")
	}
	if _, e := m.BeginPrivateIntent(context.Background(), b); e != nil {
		t.Fatal("rejected request consumed the scope", e)
	}
}
func TestU219ChallengeFailureRemainUnqualified(t *testing.T) {
	m := u218Manager(t)
	b := u218Binding()
	m.BeginPrivateIntent(context.Background(), b)
	for _, phase := range []AuthState{AuthChallenging, AuthFailed} {
		m.mergeAndEmit(DRMSnapshot{State: DRMState{Authentication: phase}})
		s, e := m.PrivateIntentStatus(context.Background(), b, true)
		if e != nil || s.State.Authentication != phase || s.NativeAuthenticated {
			t.Fatal(s, e)
		}
	}
}
