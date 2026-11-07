package drm

import (
	"context"
	"testing"
)

type nativeWiringProbe struct {
	*u213ManagerStub
	calls int
}

func (b *nativeWiringProbe) nativeCredentialConsumer() (privateInputConsumer, func()) {
	return func(context.Context, AuthBinding, string, *PrivateInput) (*PrivateInputChallenge, error) {
		b.calls++
		return nil, nil
	}, func() {}
}
func TestU222BackendConsumerWiring(t *testing.T) {
	b := &nativeWiringProbe{u213ManagerStub: &u213ManagerStub{crashStub: crashStub{events: make(chan DRMEvent)}, start: func(context.Context) error { return nil }}}
	cfg := BackendConfig{BaseDir: t.TempDir(), RequireTrustedCompletion: true}
	m := NewDRMManager(b, NewSessionManager(cfg.BaseDir), nil, cfg, DefaultRestartPolicy)
	t.Cleanup(func() { m.Shutdown(); close(b.events) })
	scope := u218Binding()
	if _, e := m.BeginPrivateIntent(context.Background(), scope); e != nil {
		t.Fatal("begin")
	}
	if _, e := m.HandoffPrivateInput(context.Background(), scope, "credentials", "", input220()); e != nil || b.calls != 1 {
		t.Fatal("existing private-input path did not reach qualified backend consumer")
	}
	if m.Status().NativeAuthenticated {
		t.Fatal("delivery became Native Ready")
	}
}
