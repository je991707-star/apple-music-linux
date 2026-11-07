package drm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func input220() *PrivateInput {
	return &PrivateInput{Email: []byte("u220-user@example.invalid"), Password: []byte("u220-synthetic-sentinel")}
}
func setup220(t *testing.T) (*DRMManager, AuthBinding) {
	m := u218Manager(t)
	b := u218Binding()
	if _, e := m.BeginPrivateIntent(context.Background(), b); e != nil {
		t.Fatal("begin")
	}
	m.privateInputConsumer = func(_ context.Context, b AuthBinding, k string, p *PrivateInput) (*PrivateInputChallenge, error) {
		if k == "credentials" {
			return &PrivateInputChallenge{ID: strings.Repeat("a", 32), Type: "two_factor", ExpiresAt: b.ExpiresAt}, nil
		}
		return nil, nil
	}
	return m, b
}
func zero220(t *testing.T, p *PrivateInput) {
	t.Helper()
	for _, v := range [][]byte{p.Email, p.Password, p.Reply} {
		for _, c := range v {
			if c != 0 {
				t.Fatal("owned input not cleared")
			}
		}
	}
}
func TestU220OneShotCredentialAndChallenge(t *testing.T) {
	m, b := setup220(t)
	starts := 0
	m.backend.(*u213ManagerStub).start = func(context.Context) error { starts++; return nil }
	p := input220()
	st, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p)
	if e != nil || st.Challenge == nil {
		t.Fatal("credential not consumed")
	}
	zero220(t, p)
	if m.Status().NativeAuthenticated {
		t.Fatal("intent authorized")
	}
	if _, e = m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); e == nil {
		t.Fatal("credential replay")
	}
	if _, e = m.HandoffPrivateInput(context.Background(), b, "challenge", "wrong", &PrivateInput{Reply: []byte("000001")}); e == nil {
		t.Fatal("wrong challenge")
	}
	if _, e = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); e != nil {
		t.Fatal("challenge rejected")
	}
	if _, e = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); e == nil {
		t.Fatal("challenge replay")
	}
	if m.Status().NativeAuthenticated {
		t.Fatal("challenge authorized")
	}
	if starts != 0 {
		t.Fatal("private input started Native backend")
	}
}
func TestU220NoProductionConsumer(t *testing.T) {
	m := u218Manager(t)
	b := u218Binding()
	m.BeginPrivateIntent(context.Background(), b)
	p := input220()
	_, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p)
	if !errors.Is(e, ErrPrivateInputUnavailable) {
		t.Fatal("production consumer open")
	}
	zero220(t, p)
}
func TestU220ForeignScopeNeverReachesConsumer(t *testing.T) {
	for _, field := range []string{"account", "epoch", "attempt", "owner", "boot", "nonce", "revision", "generation", "deadline"} {
		t.Run(field, func(t *testing.T) {
			m, b := setup220(t)
			calls := 0
			m.privateInputConsumer = func(context.Context, AuthBinding, string, *PrivateInput) (*PrivateInputChallenge, error) {
				calls++
				return nil, nil
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
				bad.EngineBoot = strings.Repeat("b", 32)
			case "nonce":
				bad.BootNonce = "foreign"
			case "revision":
				bad.CredentialRevision++
			case "generation":
				bad.Generation++
			case "deadline":
				bad.ExpiresAt++
			}
			p := input220()
			if _, e := m.HandoffPrivateInput(context.Background(), bad, "credentials", "", p); e == nil || calls != 0 {
				t.Fatal("foreign input delivered")
			}
			zero220(t, p)
		})
	}
}
func TestU220UnsolicitedChallenge(t *testing.T) {
	m, b := setup220(t)
	if _, e := m.HandoffPrivateInput(context.Background(), b, "challenge", strings.Repeat("a", 32), &PrivateInput{Reply: []byte("000001")}); e == nil {
		t.Fatal("unsolicited challenge")
	}
}
func TestU220TerminalRejectsInput(t *testing.T) {
	for _, state := range []AuthState{AuthCancelled, AuthRevoked, AuthFailed, AuthExpired} {
		t.Run(state.String(), func(t *testing.T) {
			m, b := setup220(t)
			m.transitionTrusted(context.Background(), b, state, NativeEvidence{})
			p := input220()
			if _, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p); e == nil {
				t.Fatal("terminal accepted")
			}
			zero220(t, p)
		})
	}
}
func TestU220DeadlineRejectsCode(t *testing.T) {
	m, b := setup220(t)
	st, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
	if e != nil {
		t.Fatal("consume")
	}
	m.mu.Lock()
	m.privateInput.challenge.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	m.mu.Unlock()
	if _, e = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); e == nil {
		t.Fatal("expired code")
	}
}
func TestU220CancelWhileBorrowedDoesNotFreeEarly(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			m, b := setup220(t)
			entered := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			p := input220()
			m.privateInputConsumer = func(ctx context.Context, _ AuthBinding, _ string, v *PrivateInput) (*PrivateInputChallenge, error) {
				close(entered)
				<-ctx.Done()
				if !bytes.HasPrefix(v.Password, []byte("u220-")) {
					t.Error("borrow invalidated before unwind")
				}
				<-release
				return nil, nil
			}
			go func() { _, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p); done <- e }()
			<-entered
			if shutdown {
				m.Shutdown()
			} else {
				m.EndPrivateIntent(context.Background(), b, AuthCancelled)
			}
			close(release)
			if <-done == nil {
				t.Fatal("late consumer committed")
			}
			zero220(t, p)
			if m.Status().NativeAuthenticated {
				t.Fatal("late Ready")
			}
		})
	}
}
func TestU220ConcurrentReplay(t *testing.T) {
	m, b := setup220(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	m.privateInputConsumer = func(context.Context, AuthBinding, string, *PrivateInput) (*PrivateInputChallenge, error) {
		close(entered)
		<-release
		return nil, nil
	}
	go func() {
		defer close(done)
		m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
	}()
	<-entered
	if _, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); e == nil {
		t.Fatal("concurrent replay")
	}
	close(release)
	<-done
}
func TestU220InputBounds(t *testing.T) {
	for _, p := range []*PrivateInput{{Email: []byte("x"), Password: bytes.Repeat([]byte("x"), 256)}, {Email: bytes.Repeat([]byte("x"), 257), Password: []byte("x")}, {Email: []byte("x"), Password: []byte{0}}, {Email: []byte("x"), Password: []byte{255}}, {Reply: []byte("1234567")}, {Reply: []byte("ab3456")}, {Email: []byte("x"), Password: []byte("x"), Reply: []byte("123456")}} {
		m, b := setup220(t)
		kind := "credentials"
		if len(p.Reply) > 0 && len(p.Email) == 0 {
			kind = "challenge"
		}
		if _, e := m.HandoffPrivateInput(context.Background(), b, kind, "", p); e == nil {
			t.Fatal("bad input accepted")
		}
		zero220(t, p)
	}
}
func TestU220BoundaryLengthAccepted(t *testing.T) {
	m, b := setup220(t)
	p := &PrivateInput{Email: bytes.Repeat([]byte("x"), 256), Password: bytes.Repeat([]byte("x"), 255)}
	if _, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p); e != nil {
		t.Fatal("valid byte boundary")
	}
	zero220(t, p)
}
func TestU220ConsumerFailureIsRedacted(t *testing.T) {
	m, b := setup220(t)
	p := input220()
	secret := string(p.Password)
	m.privateInputConsumer = func(context.Context, AuthBinding, string, *PrivateInput) (*PrivateInputChallenge, error) {
		return nil, errors.New(secret)
	}
	_, e := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p)
	if e == nil || strings.Contains(e.Error(), secret) {
		t.Fatal("unsafe consumer error")
	}
	zero220(t, p)
	if m.Status().NativeAuthenticated {
		t.Fatal("failed Ready")
	}
}
func TestU220StatusAndDiagnosticsContainNoInput(t *testing.T) {
	m, b := setup220(t)
	p := input220()
	secret := append([]byte(nil), p.Password...)
	if strings.Contains(fmt.Sprintf("%+v %#v", p, p), string(secret)) {
		t.Fatal("diagnostic leak")
	}
	redacted, _ := json.Marshal(p)
	if bytes.Contains(redacted, secret) {
		t.Fatal("JSON diagnostic leak")
	}
	m.HandoffPrivateInput(context.Background(), b, "credentials", "", p)
	raw, _ := json.Marshal(m.Status())
	if bytes.Contains(raw, secret) {
		t.Fatal("snapshot leak")
	}
	raw, _ = json.Marshal(m.privateInput)
	if bytes.Contains(raw, secret) {
		t.Fatal("manager retained input")
	}
	zero220(t, p)
}
func TestU220CancelledRequestRejected(t *testing.T) {
	m, b := setup220(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := input220()
	if _, e := m.HandoffPrivateInput(ctx, b, "credentials", "", p); e == nil {
		t.Fatal("cancelled request accepted")
	}
	zero220(t, p)
}
