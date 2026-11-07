//go:build linux && cgo && u2_native_credential_fixture

package drm

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"unsafe"
)

func manager223(t *testing.T, mode int) (*DRMManager, AuthBinding, *nativeCredentialAdapter, *cCredentialEndpoint, unsafe.Pointer) {
	ep, target := bridge223(mode)
	a := newNativeCredentialAdapter(ep)
	back := &u222NativeBackend{u213ManagerStub: &u213ManagerStub{crashStub: crashStub{events: make(chan DRMEvent)}, start: func(context.Context) error { return nil }}, a: a}
	cfg := BackendConfig{BaseDir: t.TempDir(), RequireTrustedCompletion: true}
	m := NewDRMManager(back, NewSessionManager(cfg.BaseDir), nil, cfg, DefaultRestartPolicy)
	t.Cleanup(func() { m.Shutdown(); close(back.events) })
	b := u218Binding()
	if _, err := m.BeginPrivateIntent(context.Background(), b); err != nil {
		t.Fatal("begin")
	}
	return m, b, a, ep, target
}
func TestU223ResponseBridgeBorrow(t *testing.T) {
	m, b, _, ep, target := manager223(t, 10)
	p := input220()
	if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p); err != nil {
		t.Fatal(err)
	}
	zero220(t, p)
	if calls223(target) != 1 || !valid223(target) || ep.fixtureMetric(2) != 1 {
		t.Fatal("response did not retain owned C++ backing")
	}
	m.EndPrivateIntent(context.Background(), b, AuthCancelled)
	if !valid223(target) || m.Status().NativeAuthenticated {
		t.Fatal("revocation freed borrowed storage or authorized")
	}
}
func TestU223ResponseChallenge(t *testing.T) {
	m, b, _, _, target := manager223(t, 11)
	p := input220()
	bare := len(p.Password)
	st, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", p)
	if err != nil || st.Challenge == nil || calls223(target) != 0 {
		t.Fatal("challenge")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", "wrong", &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("wrong challenge")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); err != nil {
		t.Fatal("reply")
	}
	if !valid223(target) || size223(target) != bare+6 || calls223(target) != 1 || m.Status().NativeAuthenticated {
		t.Fatal("C++ six digit backing")
	}
	if _, err = m.HandoffPrivateInput(context.Background(), b, "challenge", st.Challenge.ID, &PrivateInput{Reply: []byte("000001")}); err == nil {
		t.Fatal("replay")
	}
}
func TestU223InflightCancel(t *testing.T) {
	m, b, a, ep, target := manager223(t, 12)
	done := make(chan error, 1)
	go func() {
		_, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
		done <- err
	}()
	wait222(t, func() bool { return calls223(target) == 1 })
	m.EndPrivateIntent(context.Background(), b, AuthCancelled)
	if !valid223(target) || ep.fixtureReceipt(a.tag, b.Generation) {
		t.Fatal("inflight revocation")
	}
	release223(target)
	if <-done == nil || !valid223(target) || m.Status().NativeAuthenticated {
		t.Fatal("cancel accepted or backing invalid")
	}
}
func TestU223UnknownAndCallOwnershipClosed(t *testing.T) {
	if qualifiedBridge223(13) || qualifiedBridge223(14) || !qualifiedBridge223(10) {
		t.Fatal("bridge ownership qualification")
	}
}
func TestU223WrongBindingNeverConstructs(t *testing.T) {
	for _, field := range []string{"account", "epoch", "boot", "attempt", "owner"} {
		t.Run(field, func(t *testing.T) {
			m, b, _, _, target := manager223(t, 10)
			bad := b
			switch field {
			case "account":
				bad.AccountID = "B"
			case "epoch":
				bad.AccountEpoch++
			case "boot":
				bad.EngineBoot = "wrong"
			case "attempt":
				bad.AttemptID = "B"
			case "owner":
				bad.EngineOwnerID = "B"
			}
			if _, err := m.HandoffPrivateInput(context.Background(), bad, "credentials", "", input220()); err == nil || calls223(target) != 0 {
				t.Fatal("foreign scope constructed response")
			}
		})
	}
}
func TestU223LogoutKeepsBorrowRevokesScope(t *testing.T) {
	m, b, a, ep, target := manager223(t, 10)
	if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ep.fixtureReceipt(a.tag, b.Generation) || !valid223(target) || m.Status().NativeAuthenticated {
		t.Fatal("logout")
	}
}
func TestU223LateAAfterB(t *testing.T) {
	ma, ba, a, ep, ta := manager223(t, 10)
	ma.HandoffPrivateInput(context.Background(), ba, "credentials", "", input220())
	ma.EndPrivateIntent(context.Background(), ba, AuthCancelled)
	mb, bb, _, _, tb := manager223(t, 10)
	mb.HandoffPrivateInput(context.Background(), bb, "credentials", "", input220())
	if ep.fixtureReceipt(a.tag, ba.Generation) || !valid223(ta) || !valid223(tb) || calls223(tb) != 1 || mb.Status().NativeAuthenticated {
		t.Fatal("old original-context receipt contaminated B")
	}
}

func TestU223FreshProcessBridge(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		cmd := exec.Command(exe, "-test.run=^TestU223ResponseBridgeBorrow$", "-test.v")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("owned bridge child failed: %v %s", err, out)
		}
	}
}

func TestU223NativeContextCannotRebindBridge(t *testing.T) {
	m, b, _, ep, target := manager223(t, 10)
	if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); err != nil {
		t.Fatal(err)
	}
	other := fixtureCredentialEndpoint(0)
	other.provider = ep.provider
	other.data = ep.data
	adapter := newNativeCredentialAdapter(other)
	bad := b
	bad.AccountID = "B"
	bad.AttemptID = "B"
	if _, err := adapter.consume(context.Background(), bad, "credentials", input220()); err == nil || calls223(target) != 1 {
		t.Fatal("Native bridge rebound A storage to B context")
	}
}
