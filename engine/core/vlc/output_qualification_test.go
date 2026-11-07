//go:build vlc_ordering_test && cgo && linux

package vlc

import (
	"engine/internal/playbackcmd"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestQualifiedPulseVolume(t *testing.T) {
	if os.Getenv("U2_PRIVATE_PULSE_QUALIFICATION") != "1" {
		t.Skip("requires private no-host-wallet/no-host-audio capsule")
	}
	p, e := NewPrivatePulseTestPlayer()
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if e = p.Load("file://" + os.Getenv("ADAM_VLC_TEST_FIXTURE")); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, _, s := p.Time()
		if s == "playing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("playing deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	l := playbackcmd.NewLease("private-boot", "private-source", 1)
	p.mu.Lock()
	p.lease = l
	p.mu.Unlock()
	for _, v := range []int{80, 100, 0, 200} {
		start := time.Now()
		e = p.SetVolumeForLease(v, l, func() bool { return true })
		actual, _ := p.Volume()
		t.Logf("request=%d actual=%d elapsed=%s error=%v", v, actual, time.Since(start), e)
		if e != nil || actual != v {
			t.Fatalf("requested %d must be Native confirmed %d", v, v)
		}
	}
}
func TestVolumeWaitRevalidatesOwnershipWithoutHoldingPlayer(t *testing.T) {
	for _, action := range []string{"revision", "lease", "new-source", "stop", "close", "engine-restart"} {
		t.Run(action, func(t *testing.T) {
			p, s, e := NewIsolatedPlayer()
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			defer p.Close()
			if e = p.Load("file://" + os.Getenv("ADAM_VLC_TEST_FIXTURE")); e != nil {
				t.Fatal(e)
			}
			end := time.Now().Add(time.Second)
			for {
				_, _, state := p.Time()
				if state == "playing" {
					break
				}
				if time.Now().After(end) {
					t.Fatal("playing deadline")
				}
				time.Sleep(5 * time.Millisecond)
			}
			p.outputBackend = "" // unknown/unreporting output exercises bounded confirmation, not known capability refusal
			l := playbackcmd.NewLease("private", "A", 1)
			p.mu.Lock()
			p.lease = l
			p.mu.Unlock()
			var current atomic.Bool
			current.Store(true)
			done := make(chan error, 1)
			go func() { done <- p.SetVolumeForLease(80, l, current.Load) }()
			time.Sleep(20 * time.Millisecond)
			select {
			case e := <-done:
				t.Fatalf("readback wait ended prematurely: %v", e)
			default:
			}
			begin := time.Now()
			p.Time()
			if time.Since(begin) > 50*time.Millisecond {
				t.Fatal("readback held Player lock")
			}
			if action == "stop" {
				p.Stop()
			}
			if action == "close" {
				p.Close()
			}
			if action == "engine-restart" {
				l.Retire()
			}
			if action == "revision" {
				current.Store(false)
			}
			if action == "lease" {
				l.Retire()
			}
			if action == "new-source" {
				p.mu.Lock()
				p.lease = playbackcmd.NewLease("private", "B", 2)
				p.mu.Unlock()
			}
			select {
			case e := <-done:
				if !errors.Is(e, playbackcmd.ErrStale) {
					t.Fatal("stale wait result", e)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("ownership cancellation deadline")
			}
		})
	}
}
func TestVolumeRejectsInvalidAndForeignBeforeSetter(t *testing.T) {
	p, s, e := NewIsolatedPlayer()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	defer p.Close()
	l := playbackcmd.NewLease("b", "A", 1)
	p.lease = l
	for _, v := range []int{-1, 201} {
		if e = p.SetVolumeForLease(v, l, func() bool { return true }); !errors.Is(e, playbackcmd.ErrStale) {
			t.Fatal(v, e)
		}
	}
	if e = p.SetVolumeForLease(80, playbackcmd.NewLease("b", "B", 2), func() bool { return true }); !errors.Is(e, playbackcmd.ErrStale) {
		t.Fatal(e)
	}
}
func TestKnownMemoryOutputCannotAcknowledgeIncidentalZero(t *testing.T) {
	p, s, e := NewIsolatedPlayer()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	defer p.Close()
	l := playbackcmd.NewLease("b", "A", 1)
	p.lease = l
	for _, v := range []int{0, 80, 100, 200} {
		if e = p.SetVolumeForLease(v, l, func() bool { return true }); !errors.Is(e, ErrVolumeOutputUnsupported) {
			t.Fatal("unsupported output falsely confirmed", v, e)
		}
	}
}
