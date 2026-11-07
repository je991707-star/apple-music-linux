//go:build linux && cgo && adamplay_backend && u2_auth_mock

package drm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests must link only the Lab's offline libdrm_client substitute.
// No installed DRM data, Apple components or account material is used.
func mockNative(t *testing.T) (*adamplayBackend, BackendConfig) {
	t.Helper()
	if os.Getenv("U2_AUTH_MOCK_LIBRARY") != "offline-u2-13" || !u213MockLinked() {
		t.Fatal("offline link substitute required")
	}
	dir := filepath.Join(t.TempDir(), "files")
	if err := os.MkdirAll(filepath.Join(filepath.Dir(dir), "rootfs/system/lib64"), 0700); err != nil {
		t.Fatal(err)
	}
	b := AdamplayBackend("").(*adamplayBackend)
	t.Cleanup(func() { _ = b.Stop() })
	return b, BackendConfig{BaseDir: dir, Credentials: Credentials{Email: "synthetic-A", Password: "mock-ok"}}
}
func nativeCallCount(t *testing.T, cfg BackendConfig) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg.BaseDir, "native-calls"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "fresh\n")
}
func TestU213FreshBInvokesNativeAfterSuccessfulA(t *testing.T) {
	b, cfg := mockNative(t)
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	cfg.Credentials = Credentials{Email: "synthetic-B", Password: "mock-error"}
	err := b.Start(context.Background(), cfg)
	if got := nativeCallCount(t, cfg); got != 2 {
		t.Errorf("B skipped native authentication: fresh calls=%d, want 2", got)
	}
	if err == nil {
		t.Error("failed B accepted old readiness as success")
	}
	b.mu.Lock()
	ready := b.hybrisReady
	b.mu.Unlock()
	if ready {
		t.Error("failed B retained ready A")
	}
}

type u213Challenge func(context.Context, AuthChallenge) (string, error)

func (f u213Challenge) Challenge(ctx context.Context, r AuthChallenge) (string, error) {
	return f(ctx, r)
}
func TestU213CancellationRejectsNativeLateSuccess(t *testing.T) {
	b, cfg := mockNative(t)
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	cfg.Credentials.Email = "synthetic-B"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.SetAuthSource(u213Challenge(func(_ context.Context, _ AuthChallenge) (string, error) { cancel(); return "", context.Canceled }))
	cfg.Credentials.Password = "mock-challenge"
	if err := b.Start(ctx, cfg); !errors.Is(err, context.Canceled) {
		t.Errorf("native returned success after cancel: %v", err)
	}
	b.mu.Lock()
	ready := b.hybrisReady
	b.mu.Unlock()
	if ready {
		t.Error("cancelled attempt published readiness")
	}
	if _, err := os.Stat(filepath.Join(cfg.BaseDir, "session-ready")); !os.IsNotExist(err) {
		t.Error("cancelled attempt persisted session-ready")
	}
}
func TestU213RepeatedFreshAndSessionReuse(t *testing.T) {
	b, cfg := mockNative(t)
	for i := 0; i < 3; i++ {
		if err := b.Start(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		if err := b.Stop(); err != nil {
			t.Fatal(err)
		}
	}
	if got := nativeCallCount(t, cfg); got != 3 {
		t.Fatalf("fresh calls=%d, want 3", got)
	}
	cfg.Credentials = Credentials{}
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	done := b.hybrisInitDone
	b.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("reuse did not complete")
		}
	}
	b.mu.Lock()
	ready := b.hybrisReady
	b.mu.Unlock()
	if !ready {
		t.Fatal("valid reuse lost readiness")
	}
	if got := nativeCallCount(t, cfg); got != 3 {
		t.Fatalf("reuse ran fresh login: %d", got)
	}
}

func TestU213LateChallengeReplyCannotCrossRuns(t *testing.T) {
	b, cfg := mockNative(t)
	entered, release := make(chan struct{}), make(chan struct{})
	b.SetAuthSource(u213Challenge(func(context.Context, AuthChallenge) (string, error) {
		close(entered)
		<-release
		return "synthetic-old-code", nil
	}))
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	reply := make(chan string, 1)
	go func() { reply <- b.answerChallenge("2fa") }()
	<-entered
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	b.SetAuthSource(u213Challenge(func(context.Context, AuthChallenge) (string, error) { return "synthetic-new-code", nil }))
	cfg.Credentials.Email = "synthetic-B"
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got := <-reply; got != "" {
		t.Fatal("late A reply escaped into B")
	}
}
func TestU213LateResultAndQueuedEventCannotAuthorizeB(t *testing.T) {
	b, cfg := mockNative(t)
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	old := b.gen
	b.mu.Unlock()
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	cfg.Credentials = Credentials{Email: "synthetic-B", Password: "mock-error"}
	if err := b.Start(context.Background(), cfg); err == nil {
		t.Fatal("B must fail")
	}
	b.mu.Lock()
	err := b.completeHybrisInitLocked(context.Background(), old, true, cfg.BaseDir)
	b.mu.Unlock()
	if err == nil {
		t.Fatal("late A result accepted")
	}
	if _, err := os.Stat(filepath.Join(cfg.BaseDir, "session-ready")); !os.IsNotExist(err) {
		t.Fatal("late A result restored marker")
	}
	m := NewDRMManager(b, NewSessionManager(cfg.BaseDir), nil, BackendConfig{}, DefaultRestartPolicy)
	t.Cleanup(m.shutdownStop)
	m.consumeBackendEvent(DRMEvent{Generation: old, Snapshot: DRMSnapshot{State: DRMState{Authentication: AuthLoggedIn, FairPlay: FairPlayReady, Session: SessionValid}}})
	if got := m.Status(); got.State.Authentication == AuthLoggedIn || got.Capabilities.CBCS {
		t.Fatal("queued A event authorized B")
	}
}
func TestU213MarkerFailureDoesNotCompleteAuthentication(t *testing.T) {
	b, cfg := mockNative(t)
	// The native return may be successful while marker storage is unavailable.
	b.mu.Lock()
	b.running = true
	b.gen = 7
	if err := b.completeHybrisInitLocked(context.Background(), 7, true, filepath.Join(cfg.BaseDir, "missing")); err == nil {
		t.Error("missing marker storage accepted")
	}
	ready := b.hybrisReady
	b.mu.Unlock()
	if ready {
		t.Fatal("marker failure published readiness")
	}
}

func TestU213RawRunningCallbackNeverAuthenticates(t *testing.T) {
	b := &adamplayBackend{running: true, gen: 2, stateCh: make(chan string, 1), eventCh: make(chan DRMEvent, 1)}
	b.stateCh <- "RUNNING"
	close(b.stateCh)
	b.stateLoop()
	ev := <-b.eventCh
	if ev.Snapshot.State.Authentication == AuthLoggedIn || ev.Snapshot.State.FairPlay == FairPlayReady || ev.Snapshot.State.Session == SessionValid {
		t.Fatal("unbound C callback granted authentication")
	}
}

func TestU213BackgroundInitMustDrainBeforeFreshB(t *testing.T) {
	b, cfg := mockNative(t)
	cfg.Credentials = Credentials{}
	if err := os.MkdirAll(cfg.BaseDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"session-ready", "hold-init"} {
		if err := os.WriteFile(filepath.Join(cfg.BaseDir, name), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(cfg.BaseDir, "release-init"), nil, 0600) })
	deadline := time.After(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(cfg.BaseDir, "init-entered")); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("mock init did not enter")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	stopped := make(chan error, 1)
	go func() { stopped <- b.Stop() }()
	for {
		b.mu.Lock()
		running := b.running
		b.mu.Unlock()
		if !running {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Stop did not revoke")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cfg.Credentials = Credentials{Email: "synthetic-B", Password: "mock-error"}
	if err := b.Start(context.Background(), cfg); err == nil {
		t.Fatal("B started under active A native init")
	}
	if err := os.WriteFile(filepath.Join(cfg.BaseDir, "release-init"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	ready := b.hybrisReady
	b.mu.Unlock()
	if ready {
		t.Fatal("late reuse A resurrected readiness after Stop")
	}
	if err := b.Start(context.Background(), cfg); err == nil {
		t.Fatal("failed B accepted after A drained")
	}
	if got := nativeCallCount(t, cfg); got != 1 {
		t.Fatalf("B did not reach native once after drain: %d", got)
	}
}

type u213CancelAfterNative struct {
	*adamplayBackend
	cancel context.CancelFunc
}

func (b *u213CancelAfterNative) Start(ctx context.Context, cfg BackendConfig) error {
	err := b.adamplayBackend.Start(ctx, cfg)
	b.cancel()
	return err
}
func TestU213ManagerCancellationAfterNativeReturnRevokesResult(t *testing.T) {
	b, cfg := mockNative(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wrapped := &u213CancelAfterNative{adamplayBackend: b, cancel: cancel}
	m := NewDRMManager(wrapped, NewSessionManager(cfg.BaseDir), nil, cfg, DefaultRestartPolicy)
	t.Cleanup(m.shutdownStop)
	if err := m.Authenticate(ctx, cfg.Credentials); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel after native return: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.BaseDir, "session-ready")); !os.IsNotExist(err) {
		t.Fatal("Manager rejected result left reusable marker")
	}
	b.mu.Lock()
	ready, running := b.hybrisReady, b.running
	b.mu.Unlock()
	if ready || running {
		t.Fatal("rejected completion retained backend authority")
	}
	m.consumeBackendEvent(DRMEvent{Generation: 1, Snapshot: DRMSnapshot{State: DRMState{FairPlay: FairPlayReady, Authentication: AuthLoggedIn}}})
	if got := m.Status(); got.State.Authentication == AuthLoggedIn || got.Capabilities.CBCS {
		t.Fatal("rejected completion was revived by queued event")
	}
}

func TestU213ExpiredAuthenticationCannotComplete(t *testing.T) {
	b, cfg := mockNative(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	b.SetAuthSource(u213Challenge(func(ctx context.Context, _ AuthChallenge) (string, error) { <-ctx.Done(); return "synthetic-late", nil }))
	cfg.Credentials.Password = "mock-challenge"
	if err := b.Start(ctx, cfg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired attempt accepted native success: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.BaseDir, "session-ready")); !os.IsNotExist(err) {
		t.Fatal("expired attempt persisted readiness")
	}
}

func TestU213RawFailureStillRevokesReadiness(t *testing.T) {
	b := &adamplayBackend{running: true, hybrisReady: true, gen: 2, stateCh: make(chan string, 1), eventCh: make(chan DRMEvent, 1)}
	b.stateCh <- "FAILED"
	close(b.stateCh)
	b.stateLoop()
	ev := <-b.eventCh
	if b.hybrisReady || ev.Snapshot.State.FairPlay != FairPlayFailed || ev.Snapshot.State.Authentication != AuthFailed {
		t.Fatal("failure notification no longer revokes readiness")
	}
}
