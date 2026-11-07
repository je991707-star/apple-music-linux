package drm

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func u218Binding() AuthBinding {
	return AuthBinding{AccountID: "account-A", AccountEpoch: 1, AttemptID: "attempt-A", EngineOwnerID: "owner-A", EngineBoot: strings.Repeat("a", 32), BootNonce: strings.Repeat("b", 64), CredentialRevision: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
}
func u218ProofManager(t *testing.T) (*DRMManager, AuthBinding) {
	m := u218Manager(t)
	b := u218Binding()
	m.mergeAndEmit(DRMSnapshot{State: DRMState{Process: ProcessRunning}})
	if err := m.beginTrustedAttempt(b); err != nil {
		t.Fatal(err)
	}
	return m, b
}
func TestU218ProductionEvidenceUnavailable(t *testing.T) {
	m, b := u218ProofManager(t)
	if err := m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "unverified"}); err == nil {
		t.Fatal("production accepted evidence without verifier")
	}
	if m.Status().NativeAuthenticated {
		t.Fatal("unauthorized Native Ready")
	}
}
func TestU218BindingMismatches(t *testing.T) {
	for _, field := range []string{"account", "epoch", "attempt", "owner", "boot", "nonce", "revision", "generation"} {
		t.Run(field, func(t *testing.T) {
			m, b := u218ProofManager(t)
			m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { return true }
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
				bad.EngineBoot = strings.Repeat("c", 32)
			case "nonce":
				bad.BootNonce = strings.Repeat("c", 64)
			case "revision":
				bad.CredentialRevision++
			case "generation":
				bad.Generation++
			}
			if m.transitionTrusted(context.Background(), bad, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic"}) == nil {
				t.Fatal("mismatch accepted")
			}
			if m.Status().NativeAuthenticated {
				t.Fatal("mismatch authorized")
			}
		})
	}
}
func TestU218TerminalStatesAbsorbCompletion(t *testing.T) {
	for _, state := range []AuthState{AuthFailed, AuthCancelled, AuthExpired, AuthRevoked} {
		t.Run(state.String(), func(t *testing.T) {
			m, b := u218ProofManager(t)
			m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { return true }
			if err := m.transitionTrusted(context.Background(), b, state, NativeEvidence{}); err != nil {
				t.Fatal(err)
			}
			if m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic"}) == nil {
				t.Fatal("terminal revived")
			}
			m.consumeBackendEvent(DRMEvent{Snapshot: DRMSnapshot{State: DRMState{Authentication: AuthLoggedIn}}})
			if s := m.Status(); s.NativeAuthenticated || s.State.Authentication != state {
				t.Fatal("late raw callback revived terminal")
			}
		})
	}
}
func TestU218CompletionCancellationDuringVerifier(t *testing.T) {
	m, b := u218ProofManager(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { close(entered); <-release; return true }
	done := make(chan error, 1)
	go func() {
		done <- m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic"})
	}()
	<-entered
	m.gateMu.Lock()
	m.gate = gateSigningOut
	m.revokeTrustedLocked()
	m.gateMu.Unlock()
	close(release)
	if <-done == nil || m.Status().NativeAuthenticated {
		t.Fatal("logout lost to delayed verification")
	}
}
func TestU218ShutdownDuringVerifier(t *testing.T) {
	m, b := u218ProofManager(t)
	m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { m.Shutdown(); return true }
	if m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic"}) == nil || m.Status().NativeAuthenticated {
		t.Fatal("shutdown revived")
	}
}
func TestU218ExpiryAndDuplicate(t *testing.T) {
	m, b := u218ProofManager(t)
	calls := 0
	m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { calls++; return true }
	e := NativeEvidence{Kind: "native-qualified", ID: "synthetic"}
	if err := m.transitionTrusted(context.Background(), b, AuthLoggedIn, e); err != nil {
		t.Fatal(err)
	}
	if !m.Status().NativeAuthenticated {
		t.Fatal("qualified synthetic proof rejected")
	}
	if m.transitionTrusted(context.Background(), b, AuthLoggedIn, e) == nil || calls != 1 {
		t.Fatal("duplicate applied")
	}
	m.mu.Lock()
	m.authBinding.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	m.mu.Unlock()
	if s := m.Status(); s.NativeAuthenticated || s.State.Authentication != AuthExpired {
		t.Fatal("expired proof stayed ready")
	}
}
func TestU218SnapshotCannotMutateBinding(t *testing.T) {
	m, b := u218ProofManager(t)
	s := m.Status()
	s.AuthBinding.AccountID = "forged"
	if m.Status().AuthBinding.AccountID != b.AccountID {
		t.Fatal("snapshot leaked mutable binding")
	}
}
func TestU218ChallengeIsNotCompletion(t *testing.T) {
	m, b := u218ProofManager(t)
	if err := m.transitionTrusted(context.Background(), b, AuthChallenging, NativeEvidence{}); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); s.NativeAuthenticated || s.State.Authentication != AuthChallenging {
		t.Fatal("challenge granted authority")
	}
}
func TestU218CancelledContextDoesNotComplete(t *testing.T) {
	m, b := u218ProofManager(t)
	m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { return true }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if m.transitionTrusted(ctx, b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic"}) == nil || m.Status().NativeAuthenticated {
		t.Fatal("cancelled ctx authorized")
	}
}
func TestU218WireBridge(t *testing.T) {
	input := os.Getenv("U218_BINDING_FIXTURE")
	if input == "" {
		t.Skip("executed by the cross-layer Node runner with a Main-issued binding")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var b AuthBinding
	if err = json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	m := u218Manager(t)
	m.mergeAndEmit(DRMSnapshot{State: DRMState{Process: ProcessRunning}})
	if err = m.beginTrustedAttempt(b); err != nil {
		t.Fatal(err)
	}
	m.verifyNativeEvidence = func(scope AuthBinding, e NativeEvidence) bool {
		return scope == b && e.ID == "synthetic-qualified-proof"
	}
	if err = m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "synthetic-qualified-proof"}); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(m.Status())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("U218_COMPLETION_FIXTURE"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestU218PostCompletionRevocation(t *testing.T) {
	for _, a := range []AuthState{AuthFailed, AuthLoggedOut, AuthLoggingIn, AuthChallenging} {
		t.Run(a.String(), func(t *testing.T) {
			m, b := u218ProofManager(t)
			m.verifyNativeEvidence = func(AuthBinding, NativeEvidence) bool { return true }
			e := NativeEvidence{Kind: "native-qualified", ID: "synthetic"}
			if err := m.transitionTrusted(context.Background(), b, AuthLoggedIn, e); err != nil {
				t.Fatal(err)
			}
			m.mergeAndEmit(DRMSnapshot{State: DRMState{Authentication: a}})
			if m.Status().NativeAuthenticated {
				t.Fatal("revocation retained authority")
			}
			if m.transitionTrusted(context.Background(), b, AuthLoggedIn, e) == nil {
				t.Fatal("consumed receipt reused")
			}
		})
	}
}
