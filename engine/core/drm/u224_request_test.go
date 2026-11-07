//go:build linux && cgo && u2_native_credential_fixture

package drm

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
	"unsafe"
)

func issue224(t *testing.T, mode int) (*DRMManager, AuthBinding, *cCredentialEndpoint, unsafe.Pointer) {
	t.Helper()
	m, b, _, ep, target := manager223(t, mode)
	if _, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220()); err != nil {
		t.Fatal(err)
	}
	return m, b, ep, target
}
func TestU224QualifiedLocalDuplicateAndUnknown(t *testing.T) {
	m, _, ep, target := issue224(t, 10)
	before := ep.fixtureMetric(3)
	if unknown224(ep) != 1 || ep.fixtureMetric(3) != before {
		t.Fatal("unregistered pointer acquired scope")
	}
	if callback224(target, ep) != 0 || callback224(target, ep) != 4 || ep.fixtureMetric(3) != before+1 || m.Status().NativeAuthenticated {
		t.Fatal("qualified local progress, duplicate or Ready")
	}
}
func TestU224CancelAndLogoutQuarantine(t *testing.T) {
	for _, op := range []string{"cancel", "logout"} {
		t.Run(op, func(t *testing.T) {
			m, b, ep, target := issue224(t, 10)
			if op == "cancel" {
				m.EndPrivateIntent(context.Background(), b, AuthCancelled)
			} else {
				if _, err := m.Logout(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if callback224(target, ep) != 2 || m.Status().NativeAuthenticated {
				t.Fatal("late request retained authority")
			}
		})
	}
}
func TestU224ShutdownQuarantine(t *testing.T) {
	m, _, ep, target := issue224(t, 10)
	m.Shutdown()
	if callback224(target, ep) != 6 || m.Status().NativeAuthenticated {
		t.Fatal("shutdown scope")
	}
}
func TestU224FirstDelayedAAfterB(t *testing.T) {
	ma, ba, ea, ta := issue224(t, 10)
	ma.EndPrivateIntent(context.Background(), ba, AuthCancelled)
	mb, _, eb, _ := issue224(t, 10)
	before := eb.fixtureMetric(3)
	if callback224(ta, eb) != 3 || callback224(ta, ea) != 2 || eb.fixtureMetric(3) != before || mb.Status().NativeAuthenticated {
		t.Fatal("first A callback contaminated B")
	}
}
func TestU224SameEngineCannotBindB(t *testing.T) {
	m, b, ea, _ := issue224(t, 10)
	m.EndPrivateIntent(context.Background(), b, AuthCancelled)
	eb, _ := bridge223(10)
	sameProcess224(eb, ea)
	adapter := newNativeCredentialAdapter(eb)
	b.AccountID = "B"
	b.AttemptID = "B"
	if _, err := adapter.consume(context.Background(), b, "credentials", input220()); err == nil || eb.fixtureMetric(0) != 0 {
		t.Fatal("same Engine reassigned Native authority")
	}
}
func TestU224RequestPointerCannotAcquireIdentity(t *testing.T) {
	m, _, ep, _ := issue224(t, 10)
	before := diagnostic224(1, 0)
	for range 3 {
		if unknown224(ep) != 1 {
			t.Fatal("unknown pointer mapped")
		}
	}
	if diagnostic224(1, 0) != before+3 || m.Status().NativeAuthenticated {
		t.Fatal("quarantine diagnostic or Ready")
	}
}
func TestU224ExpiredRequest(t *testing.T) {
	ep, target := bridge223(10)
	adapter := newNativeCredentialAdapter(ep)
	b := u218Binding()
	b.ExpiresAt = time.Now().Add(250 * time.Millisecond).UnixMilli()
	if _, err := adapter.consume(context.Background(), b, "credentials", input220()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(time.UnixMilli(b.ExpiresAt)) + 10*time.Millisecond)
	if callback224(target, ep) != 5 {
		t.Fatal("expired callback")
	}
}
func TestU224InflightShutdownBorrow(t *testing.T) {
	m, b, _, ep, target := manager223(t, 12)
	done := make(chan error, 1)
	go func() {
		_, err := m.HandoffPrivateInput(context.Background(), b, "credentials", "", input220())
		done <- err
	}()
	wait222(t, func() bool { return calls223(target) == 1 })
	m.Shutdown()
	if callback224(target, ep) != 6 || !valid223(target) {
		t.Fatal("shutdown callback or borrowed buffer")
	}
	release223(target)
	if <-done == nil || !valid223(target) || m.Status().NativeAuthenticated {
		t.Fatal("late C++ delivery accepted")
	}
}
func TestU224GlobalProcessBarrierFreshExec(t *testing.T) {
	if os.Getenv("U224_BARRIER_CHILD") == "1" {
		if !globalBarrier224() {
			t.Fatal("production process barrier")
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		cmd := exec.Command(exe, "-test.run=^TestU224GlobalProcessBarrierFreshExec$", "-test.v")
		cmd.Env = append(os.Environ(), "U224_BARRIER_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("owned child: %v %s", err, out)
		}
	}
}

func TestU224PointerReuseNeedsOriginalProof(t *testing.T) {
	_, _, _, old := issue224(t, 10)
	m, _, ep, current := issue224(t, 10)
	before := ep.fixtureMetric(3)
	if wrongNonce224(current, ep) != 1 || pointerReuse224(old, current, ep) != 1 || ep.fixtureMetric(3) != before || m.Status().NativeAuthenticated {
		t.Fatal("pointer equality replaced original proof")
	}
	if callback224(current, ep) != 0 {
		t.Fatal("correct original proof lost")
	}
}
