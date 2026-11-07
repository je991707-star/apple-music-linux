//go:build vlc_ordering_test && cgo

package vlc

import (
	"context"
	"engine/internal/playbackcmd"
	"errors"
	"testing"
	"time"
)

func TestNativeLeaseRejectsForeignAndUnboundTicket(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	l := playbackcmd.NewLease(g.Snapshot().Boot, "a", 1)
	load, e := l.Admit(func() (*playbackcmd.Ticket, error) { return g.Admit(nil, "load", "a") })
	if e != nil {
		t.Fatal(e)
	}
	if e := p.LoadSourceCommand(fixtureSource(t, false), load, 0); e != nil {
		t.Fatal(e)
	}
	waitNative(t, func() bool { _, _, s := p.Time(); return s == "playing" })
	var other playbackcmd.Gate
	other.SetSession("b")
	foreign := playbackcmd.NewLease(other.Snapshot().Boot, "b", 1)
	seek, _ := foreign.Admit(func() (*playbackcmd.Ticket, error) { return other.Admit(nil, "seek", "b") })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := p.SetTimeCommand(ctx, 400, seek); !errors.Is(e, playbackcmd.ErrStale) {
		t.Errorf("foreign lease sought current media: %v", e)
	}
	unbound, _ := other.Admit(nil, "resume", "b")
	if e := p.TransportCommand("resume", unbound); !errors.Is(e, playbackcmd.ErrStale) {
		t.Errorf("unbound command drove owned media: %v", e)
	}
	otherLoad, _ := foreign.Admit(func() (*playbackcmd.Ticket, error) { return other.Admit(nil, "load", "b") })
	if e := p.LoadSourceCommand(fixtureSource(t, false), otherLoad, 0); !errors.Is(e, playbackcmd.ErrStale) {
		t.Errorf("different Engine Boot replaced owned Player: %v", e)
	}

}
