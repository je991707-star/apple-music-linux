package playbackcmd

import (
	"context"
	"testing"
	"time"
)

func TestLeaseEntryAndDrainFence(t *testing.T) {
	var g Gate
	g.SetSession("a")
	l := NewLease(g.Snapshot().Boot, "a", 1)
	x, e := l.Admit(func() (*Ticket, error) { return g.Admit(nil, "load", "a") })
	if e != nil {
		t.Fatal(e)
	}
	done, ok := l.Resource()
	if !ok {
		t.Fatal("resource denied")
	}
	if !x.EnterNative() {
		t.Fatal("live entry denied")
	}
	g.Loaded(x)
	l.Retire()
	if x.Valid() || x.EnterNative() {
		t.Fatal("retired ticket entered")
	}
	if _, ok := l.Resource(); ok {
		t.Fatal("retired resource reacquired")
	}
	if _, e := l.Admit(func() (*Ticket, error) { t.Fatal("retired lease invoked admit"); return nil, nil }); e == nil {
		t.Fatal("retired admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := l.WaitResources(ctx); e == nil {
		t.Fatal("cleanup lied before reference closed")
	}
	done()
	done()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := l.WaitResources(ctx); e != nil {
		t.Fatal(e)
	}
	g.RetiredStopped(l)
	if g.Snapshot().Session != "" || g.Owns("a") {
		t.Fatal("retired metadata owner remains")
	}
}
func TestOldLeaseCleanupDoesNotClearNewDesired(t *testing.T) {
	var g Gate
	l := NewLease(g.Snapshot().Boot, "a", 1)
	a, _ := l.Admit(func() (*Ticket, error) { return g.Admit(nil, "load", "a") })
	g.Loaded(a)
	b := NewLease(g.Snapshot().Boot, "b", 2)
	x, e := b.Admit(func() (*Ticket, error) { return g.Admit(nil, "load", "b") })
	if e != nil {
		t.Fatal(e)
	}
	g.Loaded(x)
	l.Retire()
	g.RetiredStopped(l)
	if !g.Owns("b") || !x.Valid() {
		t.Fatal("old cleanup erased new source")
	}
}
