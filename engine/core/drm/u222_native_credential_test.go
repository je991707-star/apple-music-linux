//go:build linux && cgo && u2_native_credential_fixture

package drm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type u222NativeBackend struct {
	*u213ManagerStub
	a *nativeCredentialAdapter
}

func (b *u222NativeBackend) nativeCredentialConsumer() (privateInputConsumer, func()) {
	return b.a.consumer()
}
func (b *u222NativeBackend) nativeCredentialShutdown() func() { return b.a.shutdown }
func native222(t *testing.T, mode int) (*DRMManager, AuthBinding, *nativeCredentialAdapter, *cCredentialEndpoint) {
	t.Helper()
	ep := fixtureCredentialEndpoint(mode)
	a := newNativeCredentialAdapter(ep)
	back := &u222NativeBackend{u213ManagerStub: &u213ManagerStub{crashStub: crashStub{events: make(chan DRMEvent)}, start: func(context.Context) error { return nil }}, a: a}
	cfg := BackendConfig{BaseDir: t.TempDir(), RequireTrustedCompletion: true}
	m := NewDRMManager(back, NewSessionManager(cfg.BaseDir), nil, cfg, DefaultRestartPolicy)
	t.Cleanup(func() { m.Shutdown(); close(back.events) })
	b := u218Binding()
	if _, err := m.BeginPrivateIntent(context.Background(), b); err != nil {
		t.Fatal("begin")
	}
	return m, b, a, ep
}
func wait222(t *testing.T, fn func() bool) {
	t.Helper()
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("controlled Native did not enter")
}
func TestU222NativeCredentialChallenge(t *testing.T) {
	m, b, _, ep := native222(t, 1)
	p := input220()
	st, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p)
	if err != nil || st.Challenge == nil {
		t.Fatal("Native challenge unavailable")
	}
	zero220(t, p)
	if ep.fixtureMetric(0) != 1 {
		t.Fatal("did not cross C")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); err == nil {
		t.Fatal("replayed credentials")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", "wrong", &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("wrong challenge")
	}
	if ep.fixtureMetric(0) != 1 {
		t.Fatal("rejected input entered Native")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); err != nil {
		t.Fatal("Native challenge response failed")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("replayed response")
	}
	if ep.fixtureMetric(0) != 2 || m.Status().NativeAuthenticated || m.Status().State.Authentication == AuthLoggedIn {
		t.Fatal("unsafe Native consumption")
	}
	if err = m.transitionTrusted(context.Background(), b, AuthLoggedIn, NativeEvidence{Kind: "native-qualified", ID: "untrusted-native-return"}); err == nil {
		t.Fatal("consumer bypassed verifier")
	}
}
func TestU222NativeForeignBindings(t *testing.T) {
	for _, field := range []string{"account", "epoch", "attempt", "owner", "boot", "nonce", "revision", "generation", "deadline"} {
		t.Run(field, func(t *testing.T) {
			m, b, _, ep := native222(t, 0)
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
			case "deadline":
				bad.ExpiresAt++
			}
			if _, err := m.HandoffPrivateInput(context.Background(), bad, "credentials", "", input220()); err == nil || ep.fixtureMetric(0) != 0 {
				t.Fatal("foreign scope reached C")
			}
		})
	}
}
func TestU222AdapterCannotRebind(t *testing.T) {
	_, b, a, ep := native222(t, 0)
	if _, err := a.consume(context.Background(), b, "credentials", input220()); err != nil {
		t.Fatal("first")
	}
	bad := b
	bad.AccountID = "B"
	bad.AttemptID = "B"
	if _, err := a.consume(context.Background(), bad, "credentials", input220()); err == nil || ep.fixtureMetric(0) != 1 {
		t.Fatal("A adapter reused as B")
	}
}
func TestU222CancelAfterConsumptionRevokesNative(t *testing.T) {
	m, b, a, ep := native222(t, 1)
	st, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
	if err != nil {
		t.Fatal("credential")
	}
	if _, err = m.EndPrivateIntent(context.Background(), b, AuthCancelled); err != nil {
		t.Fatal("cancel")
	}
	if ep.fixtureReceipt(a.tag, b.Generation) {
		t.Fatal("late callback accepted")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("late challenge")
	}
	if ep.fixtureMetric(0) != 1 || m.Status().NativeAuthenticated {
		t.Fatal("revocation failed")
	}
}
func TestU222TimeoutNeverEntersNative(t *testing.T) {
	m, b, _, ep := native222(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.HandoffPrivateInput(ctx, b, "credentials", "", input220()); err == nil || ep.fixtureMetric(0) != 0 {
		t.Fatal("cancelled request entered C")
	}
	m.mu.Lock()
	m.authBinding.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	expired := *m.authBinding
	m.mu.Unlock()
	if _, err := m.HandoffPrivateInput(context.Background(), expired, "credentials", "", input220()); err == nil || ep.fixtureMetric(0) != 0 {
		t.Fatal("expired request entered C")
	}
}
func TestU222CancelInflightDoesNotFreeBorrow(t *testing.T) {
	m, b, _, ep := native222(t, 4)
	input := input220()
	done := make(chan error, 1)
	go func() {
		_, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input)
		done <- err
	}()
	wait222(t, ep.fixtureStarted)
	if _, err := m.EndPrivateIntent(context.Background(), b, AuthCancelled); err != nil {
		t.Fatal("cancel")
	}
	if ep.fixtureMetric(1) != 1 || !ep.fixtureHeld() {
		t.Fatal("borrow freed under in-flight user")
	}
	ep.fixtureRelease()
	if err := <-done; err == nil {
		t.Fatal("cancelled Native result committed")
	}
	zero220(t, input)
}
func TestU222ShutdownInflightDoesNotFreeBorrow(t *testing.T) {
	m, b, _, ep := native222(t, 4)
	done := make(chan error, 1)
	go func() {
		_, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
		done <- e
	}()
	wait222(t, ep.fixtureStarted)
	m.Shutdown()
	if !ep.fixtureHeld() {
		t.Fatal("shutdown freed live C payload")
	}
	ep.fixtureRelease()
	if <-done == nil || m.Status().NativeAuthenticated {
		t.Fatal("shutdown authorized")
	}
}
func TestU222BorrowRetainedToProcessExit(t *testing.T) {
	m, b, _, ep := native222(t, 3)
	p := input220()
	if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p); err != nil {
		t.Fatal("borrow")
	}
	zero220(t, p)
	m.EndPrivateIntent(context.Background(), b, AuthRevoked)
	if ep.fixtureMetric(2) != 1 || !ep.fixtureHeld() {
		t.Fatal("C process borrow was cleared/freed early")
	}
}
func TestU222NativeFailureAndAllocation(t *testing.T) {
	for _, mode := range []int{2, 5} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			m, b, _, ep := native222(t, mode)
			if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); err == nil {
				t.Fatal("failure ignored")
			}
			if m.Status().NativeAuthenticated || m.Status().State.Authentication != AuthFailed {
				t.Fatal("failure became Ready")
			}
			if mode == 5 && ep.fixtureStarted() {
				t.Fatal("allocation error invoked provider")
			}
		})
	}
}
func TestU222NativeLengthsAndIdentity(t *testing.T) {
	_, b, a, ep := native222(t, 1)
	if _, err := a.consume(context.Background(), b, "credentials", input220()); err != nil {
		t.Fatal("first")
	}
	bad := a.tag
	bad[0] ^= 1
	if ep.fixtureReceipt(bad, b.Generation) || ep.fixtureReceipt(a.tag, b.Generation+1) {
		t.Fatal("foreign Native callback")
	}
	p := &PrivateInput{Email: []byte("u220-user@example.invalid"), Password: make([]byte, 256)}
	if _, err := ep.submit(a.tag, b.Generation, "credentials", strings.Repeat("a", 32), p); err == nil {
		t.Fatal("Native length accepted")
	}
	if _, err := ep.submit(a.tag, b.Generation, "challenge", strings.Repeat("f", 32), &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("C challenge mismatch accepted")
	}
}
func TestU222OldCallbackDoesNotAffectB(t *testing.T) {
	a, ba, aa, ea := native222(t, 1)
	a.HandoffPrivateInput(context.Background(), ba, "credentials", "", input220())
	a.EndPrivateIntent(context.Background(), ba, AuthRevoked)
	b, bb, _, eb := native222(t, 1)
	b.HandoffPrivateInput(context.Background(), bb, "credentials", "", input220())
	before := eb.fixtureMetric(3)
	if ea.fixtureReceipt(aa.tag, ba.Generation) || eb.fixtureMetric(3) != before || b.Status().NativeAuthenticated {
		t.Fatal("old callback contaminated B")
	}
}
func TestU222RealProviderRemainsClosed(t *testing.T) {
	ep := newNativeCredentialEndpoint()
	if ep != nil && ep.available() {
		t.Fatal("unknown Vendor qualified")
	}
	a := newNativeCredentialAdapter(ep)
	if consume, _ := a.consumer(); consume != nil {
		t.Fatal("production open")
	}
}
func TestU222PrivateInputRedacted(t *testing.T) {
	p := input220()
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), string(p.Password)) || strings.Contains(fmt.Sprint(p), string(p.Password)) {
		t.Fatal("secret formatted")
	}
	p.Clear()
}
func TestU222FreshProcessBoundary(t *testing.T) {
	if os.Getenv("U222_CHILD_ONLY") == "1" {
		m, b, _, ep := native222(t, 0)
		if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); err != nil || ep.fixtureMetric(0) != 1 || m.Status().NativeAuthenticated {
			t.Fatal("child boundary")
		}
		return
	}
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestU222FreshProcessBoundary$", "-test.v")
		cmd.Env = append(os.Environ(), "U222_CHILD_ONLY=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal("owned test child failed", err, len(out))
		}
	}
}

func TestU222LogoutRejectsOldChallenge(t *testing.T) {
	m, b, a, ep := native222(t, 1)
	st, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
	if err != nil {
		t.Fatal("credential")
	}
	if _, err = m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ep.fixtureReceipt(a.tag, b.Generation) {
		t.Fatal("logout left Native scope valid")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("old challenge accepted after logout")
	}
	if ep.fixtureMetric(0) != 1 || m.Status().NativeAuthenticated {
		t.Fatal("logout gate")
	}
}
func TestU222CRequiresIssuedChallenge(t *testing.T) {
	_, b, _, ep := native222(t, 0)
	var tag [32]byte
	if err := ep.bind(tag, b.Generation, b.ExpiresAt); err != nil {
		t.Fatal("bind")
	}
	if _, err := ep.submit(tag, b.Generation, "credentials", strings.Repeat("a", 32), input220()); err != nil {
		t.Fatal("credential")
	}
	if _, err := ep.submit(tag, b.Generation, "challenge", strings.Repeat("\x00", 32), &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("unissued C challenge")
	}
	if ep.fixtureMetric(0) != 1 {
		t.Fatal("unissued challenge entered provider")
	}
}
