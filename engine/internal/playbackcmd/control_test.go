package playbackcmd

import (
	"context"
	"testing"
	"time"
)

func claim(t *testing.T, g *Gate) Snapshot {
	t.Helper()
	s := g.Snapshot()
	s, err := g.Claim(s.Boot, s.Epoch, "a")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func identity(s Snapshot, seq, load uint64, kind, session string) *Identity {
	return &Identity{Boot: s.Boot, Epoch: s.Epoch, Session: session, Load: load, Sequence: seq, Type: kind, Request: kind + "-request"}
}
func TestOrderingAndLegacyFence(t *testing.T) {
	var g Gate
	g.SetSession("a")
	s := claim(t, &g)
	a, err := g.Admit(identity(s, 1, 1, "load", "a"), "load", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.Admit(identity(s, 3, 1, "pause", "a"), "pause", "a")
	if err != nil {
		t.Fatal(err)
	}
	if a.Valid() || !b.Valid() {
		t.Fatal("old intent survived")
	}
	for _, x := range []*Identity{identity(s, 2, 1, "seek", "a"), identity(s, 3, 1, "pause", "a")} {
		if _, err := g.Admit(x, x.Type, "a"); err == nil {
			t.Fatal("reordered/duplicate accepted")
		}
	}
	if _, err := g.Admit(nil, "stop", ""); err != ErrLegacy {
		t.Fatal(err)
	}
}
func TestEpochBootSessionAndGeneration(t *testing.T) {
	var g Gate
	g.SetSession("a")
	s := claim(t, &g)
	fresh, err := g.Claim(s.Boot, s.Epoch, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Admit(identity(s, 1, 1, "load", "a"), "load", "a"); err == nil {
		t.Fatal("old renderer accepted")
	}
	var restart Gate
	if _, err := restart.Admit(identity(fresh, 1, 1, "load", "a"), "load", "a"); err == nil {
		t.Fatal("old boot accepted")
	}
	a, _ := g.Admit(identity(fresh, 1, 1, "load", "b"), "load", "b")
	if a == nil {
		t.Fatal("new Session load rejected")
	}
	if _, err := g.Admit(identity(fresh, 2, 1, "seek", "a"), "seek", "a"); err == nil {
		t.Fatal("old Session seek accepted")
	}
	if _, err := g.Admit(identity(fresh, 2, 1, "load", "b"), "load", "b"); err == nil {
		t.Fatal("same load generation accepted")
	}
}
func TestAdmissionDoesNotWaitForNativeCommit(t *testing.T) {
	var g Gate
	g.SetSession("a")
	old, _ := g.Admit(nil, "load", "a")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- g.Commit(context.Background(), old, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	accepted := make(chan *Ticket, 1)
	go func() { n, _ := g.Admit(nil, "stop", ""); accepted <- n }()
	select {
	case n := <-accepted:
		if !n.Valid() {
			t.Fatal("new intent invalid")
		}
	case <-time.After(time.Second):
		t.Fatal("admission blocked behind native commit")
	}
	close(release)
	if err := <-done; err != ErrStale {
		t.Fatal(err)
	}
}

func TestIdentityRejectsRequestReorderingAndBadEnvelope(t *testing.T) {
	var g Gate
	g.SetSession("a")
	s := claim(t, &g)
	newest, _ := g.Admit(identity(s, 4, 2, "load", "a"), "load", "a")
	if newest == nil {
		t.Fatal("newest rejected")
	}
	for _, id := range []*Identity{identity(s, 2, 1, "load", "a"), identity(s, 4, 2, "load", "a"), identity(s, 5, 2, "seek", "b"), identity(s, 5, 1, "pause", "a")} {
		if _, err := g.Admit(id, id.Type, id.Session); err == nil {
			t.Fatal("stale/replayed request accepted", id)
		}
	}
	wrong := identity(s, 5, 2, "seek", "a")
	if _, err := g.Admit(wrong, "pause", "a"); err == nil {
		t.Fatal("type mismatch accepted")
	}
}
func TestRepeatedStopCannotCrossNewLoad(t *testing.T) {
	var g Gate
	g.SetSession("a")
	s := claim(t, &g)
	load, _ := g.Admit(identity(s, 1, 1, "load", "a"), "load", "a")
	g.Loaded(load)
	stop, err := g.Admit(identity(s, 2, 1, "stop", "a"), "stop", "a")
	if err != nil {
		t.Fatal(err)
	}
	g.Stopped(stop)
	if _, err := g.Admit(identity(s, 3, 1, "stop", "a"), "stop", "a"); err != nil {
		t.Fatal("idempotent new Stop rejected", err)
	}
	g.Admit(identity(s, 4, 2, "load", "b"), "load", "b")
	if _, err := g.Admit(identity(s, 5, 2, "stop", "a"), "stop", "a"); err == nil {
		t.Fatal("old owner stopped new Session")
	}
}
func TestRetirementCancelsPendingWorkWithoutStoppingMedia(t *testing.T) {
	var g Gate
	g.SetSession("a")
	s := claim(t, &g)
	a, _ := g.Admit(identity(s, 1, 1, "load", "a"), "load", "a")
	g.Loaded(a)
	cancel, err := g.Admit(identity(s, 2, 1, "cancel", "a"), "cancel", "a")
	if err != nil || !cancel.Valid() || a.Valid() || !g.Owns("a") {
		t.Fatal("retirement stopped or replaced media", err)
	}
	pending, err := g.Admit(identity(s, 3, 2, "load", "b"), "load", "b")
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Admit(identity(s, 4, 2, "cancel", "b"), "cancel", "b")
	if err != nil || pending.Valid() || g.cur != "a" {
		t.Fatal("pending new Session not cancelled without changing media", err)
	}
}

func TestCompletedOldStopPreservesNewLoadIntent(t *testing.T) {
	var g Gate
	g.SetSession("a")
	s := claim(t, &g)
	old, err := g.Admit(identity(s, 1, 0, "stop", "a"), "stop", "a")
	if err != nil {
		t.Fatal(err)
	}
	next, err := g.Admit(identity(s, 2, 1, "load", "b"), "load", "b")
	if err != nil {
		t.Fatal(err)
	}
	// The old C stop already entered before the new Load was admitted.
	g.Stopped(old)
	if got := g.Snapshot(); got.Session != "b" || !next.Valid() {
		t.Fatalf("old native completion erased newer desired session: %+v", got)
	}
	if _, err := g.Admit(identity(s, 3, 1, "pause", "b"), "pause", "b"); err != nil {
		t.Fatal("new Session cannot cancel its pending Load", err)
	}
}

func TestShutdownRevokesAndCannotBeReclaimed(t *testing.T) {
	var g Gate
	s := claim(t, &g)
	old, e := g.Admit(identity(s, 1, 1, "load", "a"), "load", "a")
	if e != nil {
		t.Fatal(e)
	}
	g.Shutdown()
	if old.Valid() {
		t.Fatal("shutdown left pending command alive")
	}
	if _, e := g.Admit(identity(s, 2, 2, "load", "a"), "load", "a"); e == nil {
		t.Fatal("closed Engine accepted command")
	}
	if _, e := g.Claim(s.Boot, s.Epoch, "a"); e == nil {
		t.Fatal("closed Engine reclaimed")
	}
}
