package drm

import (
	"encoding/json"
	"testing"
	"time"
)

func TestManagerStateStringIsTotal(t *testing.T) {
	for _, s := range []ManagerState{ManagerUnknown, ManagerDisabled, ManagerInitializing, ManagerReady, ManagerShuttingDown, ManagerFailed} {
		if s.String() == "" {
			t.Errorf("state %d has no name", int(s))
		}
	}
	for _, bad := range []ManagerState{-1, 6, 99, -1 << 31} {
		if bad.String() != "unknown" {
			t.Errorf("ManagerState(%d).String() = %q, want unknown", int(bad), bad.String())
		}
		if _, err := json.Marshal(bad); err != nil {
			t.Errorf("marshalling ManagerState(%d) failed: %v", int(bad), err)
		}
	}
}

func newFSMManager(t *testing.T) *DRMManager {
	t.Helper()
	stub := &crashStub{events: make(chan DRMEvent)}
	return NewDRMManager(stub, NewSessionManager(t.TempDir()), nil, BackendConfig{},
		RestartPolicy{MaxCrashRestarts: 2, RestartBackoff: []time.Duration{time.Millisecond}, StartupTimeout: time.Second})
}

// Before shutdown the manager moves freely (Logout, for one, passes through shutting_down on its way
// back to ready). After Shutdown it stays at shutting_down whatever late callbacks say.
func TestShutdownIsAbsorbing(t *testing.T) {
	m := newFSMManager(t)
	m.setManagerState(ManagerShuttingDown)
	m.setManagerState(ManagerReady)
	if got := m.Status().State.Manager; got != ManagerReady {
		t.Fatalf("a logout-style transient must be able to return to ready, got %v", got)
	}

	m.Shutdown()
	for _, late := range []ManagerState{ManagerReady, ManagerFailed, ManagerInitializing, ManagerDisabled} {
		m.setManagerState(late)
		m.mergeAndEmit(DRMSnapshot{State: DRMState{Manager: late}})
		if got := m.Status().State.Manager; got != ManagerShuttingDown {
			t.Fatalf("after Shutdown a late %v flipped the state to %v", late, got)
		}
	}
	// Other dimensions still update; only the manager lifecycle is frozen.
	m.mergeAndEmit(DRMSnapshot{State: DRMState{Process: ProcessStopped}})
	if got := m.Status().State.Process; got != ProcessStopped {
		t.Errorf("process state should still update, got %v", got)
	}
}
