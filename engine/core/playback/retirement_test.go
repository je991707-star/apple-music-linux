package playback

import (
	"context"
	"engine/internal/playbackcmd"
	"testing"
	"time"
)

func retirementTicket(t *testing.T, m *Manager, id string) *playbackcmd.Ticket {
	t.Helper()
	var g playbackcmd.Gate
	g.SetSession(id)
	x, e := g.Admit(nil, "load", id)
	if e != nil {
		t.Fatal(e)
	}
	x, e = m.AdmitSource(id, x.Identity.Boot, func() (*playbackcmd.Ticket, error) { return x, nil })
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func TestRetirementBeforeDelayedReleaseHook(t *testing.T) {
	for _, kind := range []string{"release", "lookup", "sweep"} {
		t.Run(kind, func(t *testing.T) {
			m := newTestManager(&stubProvider{})
			s, e := m.Open(context.Background(), OpenRequest{AssetID: "same"})
			if e != nil {
				t.Fatal(e)
			}
			x := retirementTicket(t, m, s.ID)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			m.SetReleaseHook(func(string) { close(entered); <-release })
			defer func() {
				close(release)
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("hook completion deadline")
				}
			}()
			go func() {
				defer close(done)
				switch kind {
				case "release":
					m.Release(s.ID)
				case "lookup":
					m.mu.Lock()
					m.contexts[s.ID].expiry = time.Now().Add(-time.Second)
					m.mu.Unlock()
					m.GetSession(s.ID)
				case "sweep":
					m.sweepExpired(time.Now().Add(sessionTTL + time.Minute))
				}
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("hook deadline")
			}
			if x.Valid() {
				t.Error("Session removed but ticket survived until delayed hook")
			}
		})
	}
}
func TestRetiredLeaseCannotBeRevivedByTouchOrBoot(t *testing.T) {
	m := newTestManager(&stubProvider{})
	s, _ := m.Open(context.Background(), OpenRequest{AssetID: "asset"})
	x := retirementTicket(t, m, s.ID)
	l := x.SourceLease()
	if !x.EnterNative() {
		t.Fatal("live entry rejected")
	}
	m.RevokeSources()
	m.GetSession(s.ID)
	if l.Active() || x.Valid() || x.EnterNative() {
		t.Fatal("retired authority revived")
	}
	var g playbackcmd.Gate
	if _, e := m.AdmitSource(s.ID, g.Snapshot().Boot, func() (*playbackcmd.Ticket, error) { return g.Admit(nil, "load", s.ID) }); e == nil {
		t.Fatal("old Manager accepted new Boot")
	}
}
func TestLateOldHookCannotRetireReplacementSameAsset(t *testing.T) {
	m := newTestManager(&stubProvider{})
	a, _ := m.Open(context.Background(), OpenRequest{AssetID: "same", Private: true})
	var g playbackcmd.Gate
	g.SetSession(a.ID)
	bind := func(id string) *playbackcmd.Ticket {
		x, e := m.AdmitSource(id, g.Snapshot().Boot, func() (*playbackcmd.Ticket, error) { return g.Admit(nil, "load", id) })
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	old := bind(a.ID)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.SetReleaseHook(func(id string) {
		if id == a.ID {
			close(entered)
			<-release
		}
	})
	go func() { m.Release(a.ID); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("old hook deadline")
	}
	b, e := m.Open(context.Background(), OpenRequest{AssetID: "same", Private: true})
	if e != nil {
		t.Fatal(e)
	}
	next := bind(b.ID)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old hook exit deadline")
	}
	if old.Valid() || !next.Valid() || old.SourceLease() == next.SourceLease() || old.SourceLease().Identity.Generation >= next.SourceLease().Identity.Generation {
		t.Fatal("replacement lease corrupted")
	}
	m.Release(a.ID)
	if !next.Valid() {
		t.Fatal("duplicate old release cancelled replacement")
	}
	m.Release(b.ID)
}
