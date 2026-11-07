//go:build vlc_ordering_test && cgo && linux

package vlc

import (
	"context"
	"encoding/json"
	"engine/internal/playbackcmd"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func pcmPlayer(t *testing.T) (*Player, *IsolatedSink) {
	t.Helper()
	p, s, e := NewIsolatedPlayer()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); s.Close() })
	return p, s
}
func pcmReference(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(filepath.Dir(os.Getenv("ADAM_VLC_TEST_FIXTURE")), name)
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func waitEnded(t *testing.T, p *Player) {
	t.Helper()
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		_, _, s := p.Time()
		if s == "ended" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native Ended deadline (7s)")
}
func pcmEvidence(t *testing.T, s *IsolatedSink) map[string]any {
	t.Helper()
	v := s.Snapshot()
	b, _ := json.Marshal(v)
	t.Logf("PCM evidence=%s", b)
	return v
}
func TestNativePCMFullAndTrailingSilence(t *testing.T) {
	for _, tc := range []struct {
		name, media, ref string
		frames           int
	}{{"four-seconds", "fragmented.mp4", "reference.s16le", 192000}, {"tail-silence", "silence.m4a", "silence.s16le", 216000}} {
		t.Run(tc.name, func(t *testing.T) {
			p, s := pcmPlayer(t)
			ref := pcmReference(t, tc.ref)
			s.Reset(ref)
			t.Setenv("ADAM_VLC_TEST_FIXTURE", filepath.Join(filepath.Dir(os.Getenv("ADAM_VLC_TEST_FIXTURE")), tc.media))
			src := fixtureSource(t, false)
			if e := p.LoadSource(src); e != nil {
				t.Fatal(e)
			}
			waitEnded(t, p)
			v := pcmEvidence(t, s)
			if v["frames"] != tc.frames || v["full_pcm_exact"] != true {
				t.Fatal("full PCM / silence altered", v)
			}
			p.Stop()
			if src.aborts.Load() != 1 || src.closes.Load() != 1 {
				t.Fatal("source lifetime", src.aborts.Load(), src.closes.Load())
			}
		})
	}
}
func TestNativePCMSeekOriginSuffix(t *testing.T) {
	for _, tc := range []struct {
		name            string
		initial, target int64
	}{{"forward", 0, 2200}, {"backward", 2200, 500}} {
		t.Run(tc.name, func(t *testing.T) {
			p, s := pcmPlayer(t)
			ref := pcmReference(t, "reference.s16le")
			s.Reset(ref)
			var g playbackcmd.Gate
			g.SetSession("a")
			load, _ := g.Admit(nil, "load", "a")
			if e := p.LoadSourceCommand(fixtureSource(t, false), load, tc.initial); e != nil {
				t.Fatal(e)
			}
			waitNative(t, func() bool { _, _, state := p.Time(); return state == "playing" })
			time.Sleep(150 * time.Millisecond)
			seek, e := g.Admit(nil, "seek", "a")
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e = p.SetTimeCommand(ctx, tc.target, seek); e != nil {
				t.Fatal(e)
			}
			waitEnded(t, p)
			v := pcmEvidence(t, s)
			origin := v["first_post_flush_pcm_ms"].(float64)
			if math.Abs(origin-float64(tc.target)) > 4096.0/48 || v["post_flush_suffix_exact"] != true {
				t.Fatalf("seek PCM outside original ALAC frame or discontinuous: target=%d snapshot=%v", tc.target, v)
			}
		})
	}
}
func TestNativePCMTerminalRecovery(t *testing.T) {
	p, s := pcmPlayer(t)
	ref := pcmReference(t, "reference.s16le")
	s.Reset(ref)
	var g playbackcmd.Gate
	g.SetSession("a")
	snap := g.Snapshot()
	snap, e := g.Claim(snap.Boot, snap.Epoch, "a")
	if e != nil {
		t.Fatal(e)
	}
	id := func(seq uint64, kind string) *playbackcmd.Identity {
		return &playbackcmd.Identity{Boot: snap.Boot, Epoch: snap.Epoch, Session: "a", Load: 1, Sequence: seq, Type: kind, Request: "pcm-" + kind}
	}
	load, _ := g.Admit(id(1, "load"), "load", "a")
	old := fixtureSource(t, false)
	if e := p.LoadSourceCommand(old, load, 0); e != nil {
		t.Fatal(e)
	}
	g.Loaded(load)
	waitEnded(t, p)
	seek, e := g.Admit(id(2, "seek"), "seek", "a")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := p.SetTimeCommand(ctx, 500, seek); !errors.Is(e, ErrTerminalSeek) {
		t.Fatal("real Ended must request reload", e)
	}
	s.Reset(ref)
	next := fixtureSource(t, false)
	if e := g.Commit(ctx, seek, func() error { return p.ReloadSourceCommand(ctx, next, seek, 500) }); e != nil {
		t.Fatal(e)
	}
	waitEnded(t, p)
	v := pcmEvidence(t, s)
	origin := v["first_post_flush_pcm_ms"].(float64)
	if math.Abs(origin-500) > 4096.0/48 || v["post_flush_suffix_exact"] != true {
		t.Fatal("terminal reload PCM identity/precision", v)
	}
	p.mu.Lock()
	physical, seekgen := p.loadGen, p.seekGen
	p.mu.Unlock()
	t.Logf("ticket=%+v logical=%+v physicalLoad=%d nativeSeek=%d", seek.Identity, g.Snapshot(), physical, seekgen)
	if physical != 2 || g.Snapshot().Load != 1 || g.Snapshot().Sequence != 2 || !seek.Valid() {
		t.Fatal("terminal recovery identity changed")
	}
	p.Stop()
	if old.closes.Load() != 1 || next.closes.Load() != 1 {
		t.Fatal("reload source leaked")
	}
}
func TestNativeCallbackHandleReleased(t *testing.T) {
	p := orderingPlayer(t)
	src := fixtureSource(t, true)
	if e := p.LoadSource(src); e != nil {
		t.Fatal(e)
	}
	channel(t, src.entered)
	p.mu.Lock()
	h := p.cur.handle
	p.mu.Unlock()
	p.Stop()
	deleted := func() (deleted bool) { defer func() { deleted = recover() != nil }(); h.Value(); return false }()
	if !deleted || src.aborts.Load() != 1 || src.closes.Load() != 1 {
		t.Fatal("callback handle/source lifetime", deleted)
	}
	t.Logf("cgo handle deleted=%v aborts=%d closes=%d", deleted, src.aborts.Load(), src.closes.Load())
}
func TestNativeCompletedStopPreservesNewDesiredLoad(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	load, _ := g.Admit(nil, "load", "a")
	if e := p.LoadSourceCommand(fixtureSource(t, false), load, 0); e != nil {
		t.Fatal(e)
	}
	returned, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var trace []string
	p.seekProbe = func(phase string, _ int64) {
		if phase == "transport-stop" {
			close(returned)
			select {
			case <-release:
			case <-time.After(3 * time.Second):
			}
		}
		if phase == "transport-stop" || phase == "native-play" {
			mu.Lock()
			trace = append(trace, phase)
			mu.Unlock()
		}
	}
	stop, _ := g.Admit(nil, "stop", "a")
	done := make(chan error, 1)
	go func() {
		done <- g.Commit(context.Background(), stop, func() error {
			e := p.TransportCommand("stop", stop)
			if e == nil {
				g.Stopped(stop)
			}
			return e
		})
	}()
	channel(t, returned) // Real native Stop has returned; its metadata completion is held.
	next, e := g.Admit(nil, "load", "b")
	if e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !errors.Is(e, playbackcmd.ErrStale) {
		t.Fatal(e)
	}
	if g.Snapshot().Session != "b" || !next.Valid() {
		t.Fatal("old Stop erased new desired Session")
	}
	if e := g.Commit(context.Background(), next, func() error {
		e := p.LoadSourceCommand(fixtureSource(t, false), next, 0)
		if e == nil {
			g.Loaded(next)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	t.Logf("actual native completion sequence=%v", trace)
	mu.Unlock()
	if !g.Owns("b") {
		t.Fatal("new Session missing")
	}
}
