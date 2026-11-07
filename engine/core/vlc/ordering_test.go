//go:build vlc_ordering_test

package vlc

import (
	"bytes"
	"context"
	"engine/core/diskcache"
	"engine/internal/playbackcmd"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type orderedSource struct {
	mu                      sync.Mutex
	r                       *bytes.Reader
	entered, release, abort chan struct{}
	first, abortOnce        sync.Once
	aborts, closes          atomic.Int32
}

func fixtureSource(t *testing.T, held bool) *orderedSource {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("ADAM_VLC_TEST_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	s := &orderedSource{r: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{}), abort: make(chan struct{})}
	if !held {
		close(s.release)
	}
	return s
}
func (s *orderedSource) Read(b []byte) (int, error) {
	s.first.Do(func() {
		close(s.entered)
		select {
		case <-s.release:
		case <-s.abort:
		}
	})
	select {
	case <-s.abort:
		return 0, io.ErrClosedPipe
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.Read(b)
}
func (s *orderedSource) Seek(n int64, w int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.Seek(n, w)
}
func (s *orderedSource) Size() int64 { return s.r.Size() }
func (s *orderedSource) Abort()      { s.aborts.Add(1); s.abortOnce.Do(func() { close(s.abort) }) }
func (s *orderedSource) Close()      { s.closes.Add(1) }
func waitNative(t *testing.T, fn func() bool) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("native bounded condition timed out")
}
func channel(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
		t.Fatal("native barrier deadline")
	}
}
func orderingPlayer(t *testing.T) *Player {
	t.Helper()
	p, err := newOrderingPlayer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func TestNativeSameLoadLastSeekWins(t *testing.T) {
	p := orderingPlayer(t)
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var traceMu sync.Mutex
	var trace []int64
	var once sync.Once
	p.seekProbe = func(phase string, ms int64) {
		if phase == "deferred-done" && ms == 400 {
			close(done)
		}
		if phase == "deferred-before-lock" && ms == 400 {
			once.Do(func() {
				close(held)
				select {
				case <-release:
				case <-time.After(3 * time.Second):
				}
			})
		}
		if phase == "applied" {
			traceMu.Lock()
			trace = append(trace, ms)
			traceMu.Unlock()
		}
	}
	src := fixtureSource(t, true)
	if err := p.LoadSource(src); err != nil {
		t.Fatal(err)
	}
	channel(t, src.entered)
	p.SetTime(400)
	channel(t, held)
	close(src.release)
	waitNative(t, func() bool { _, _, s := p.Time(); return s == "playing" })
	p.SetTime(2200)
	p.Pause()
	close(release)
	channel(t, done)
	traceMu.Lock()
	observed := append([]int64{}, trace...)
	traceMu.Unlock()
 t.Logf("actual C set_time sequence=%v",observed)
	if len(observed) != 1 || observed[0] != 2200 {
		t.Fatalf("old delayed seek applied after new seek: %v", observed)
	}
	waitNative(t, func() bool { _, _, state := p.Time(); return state == "paused" })
	pos, _, state := p.Time()
	if state != "paused" || pos < 2000 {
		t.Fatalf("native state mismatch pos=%d state=%s", pos, state)
	}
}
func TestNativeDeferredSeekCancelledByTransport(t *testing.T) {
	for _, kind := range []string{"pause", "resume", "stop"} {
		t.Run(kind, func(t *testing.T) {
			p := orderingPlayer(t)
			var g playbackcmd.Gate
			g.SetSession("a")
			load, _ := g.Admit(nil, "load", "a")
			src := fixtureSource(t, true)
			if err := p.LoadSourceCommand(src, load, 0); err != nil {
				t.Fatal(err)
			}
			channel(t, src.entered)
			seek, _ := g.Admit(nil, "seek", "a")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- p.SetTimeCommand(ctx, 400, seek) }()
			time.Sleep(120 * time.Millisecond)
			transport, _ := g.Admit(nil, kind, "a")
			if err := p.TransportCommand(kind, transport); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, playbackcmd.ErrStale) {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("deferred seek failed to cancel")
			}
			if kind != "stop" {
				close(src.release)
			}
			p.Stop()
			if src.closes.Load() != 1 || src.aborts.Load() != 1 {
				t.Fatalf("source abort=%d close=%d", src.aborts.Load(), src.closes.Load())
			}
		})
	}
}
func TestNativeStaleCommandAtLockBoundary(t *testing.T) {
	p := orderingPlayer(t)
	src := fixtureSource(t, false)
	if err := p.LoadSource(src); err != nil {
		t.Fatal(err)
	}
	waitNative(t, func() bool { _, _, s := p.Time(); return s == "playing" })
	var g playbackcmd.Gate
	g.SetSession("a")
	old, _ := g.Admit(nil, "seek", "a")
	held, release := make(chan struct{}), make(chan struct{})
	p.seekProbe = func(phase string, ms int64) {
		if phase == "command-before-lock" {
			close(held)
			select {
			case <-release:
			case <-time.After(3 * time.Second):
			}
		}
	}
	result := make(chan error, 1)
	go func() { result <- p.SetTimeCommand(context.Background(), 400, old) }()
	channel(t, held)
	next, _ := g.Admit(nil, "pause", "a")
	if err := p.TransportCommand("pause", next); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, playbackcmd.ErrStale) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale lock-boundary command blocked")
	}
}
func TestNativeOldSessionAndBlockedReadStop(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	a, _ := g.Admit(nil, "load", "a")
	first := fixtureSource(t, true)
	if err := p.LoadSourceCommand(first, a, 1200); err != nil {
		t.Fatal(err)
	}
	channel(t, first.entered)
	stop, _ := g.Admit(nil, "stop", "")
	done := make(chan error, 1)
	go func() { done <- p.TransportCommand("stop", stop) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop blocked behind callback Read")
	}
	b, _ := g.Admit(nil, "load", "b")
	second := fixtureSource(t, false)
	if err := p.LoadSourceCommand(second, b, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.SetTimeCommand(context.Background(), 1200, a); !errors.Is(err, playbackcmd.ErrStale) {
		t.Fatal("old Session seek accepted", err)
	}
	p.Stop()
	if first.aborts.Load() != 1 || first.closes.Load() != 1 || second.closes.Load() != 1 {
		t.Fatal("source ownership not released once")
	}
}

func TestNativeExpiredContextCannotDispatch(t *testing.T) {
	p := orderingPlayer(t)
	src := fixtureSource(t, false)
	if err := p.LoadSource(src); err != nil {
		t.Fatal(err)
	}
	var g playbackcmd.Gate
	g.SetSession("a")
	ticket, _ := g.Admit(nil, "seek", "a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.SetTimeCommand(ctx, 1200, ticket); !errors.Is(err, context.Canceled) {
		t.Fatal("expired command accepted", err)
	}
}
func TestNativeDirectPauseCancelsUnversionedDeferredSeek(t *testing.T) {
	for _, kind := range []string{"pause", "resume"} {
		t.Run(kind, func(t *testing.T) {
			p := orderingPlayer(t)
			var applied atomic.Int32
			finished := make(chan struct{})
			p.seekProbe = func(phase string, ms int64) {
				if phase == "deferred-done" {
					close(finished)
				}
				if phase == "applied" {
					applied.Add(1)
				}
			}
			src := fixtureSource(t, true)
			if err := p.LoadSource(src); err != nil {
				t.Fatal(err)
			}
			channel(t, src.entered)
			p.SetTime(1200)
			if kind == "pause" {
				p.Pause()
			} else {
				p.Resume()
			}
			close(src.release)
			channel(t, finished)
			if applied.Load() != 0 {
				t.Fatal("old deferred seek survived direct transport")
			}
			p.Stop()
		})
	}
}

func TestNativeLoadCommitOvertakenByStop(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	old, _ := g.Admit(nil, "load", "a")
	entered, release := make(chan struct{}), make(chan struct{})
	p.seekProbe = func(phase string, _ int64) {
		if phase == "load-before-media" {
			close(entered)
			select {
			case <-release:
			case <-time.After(3 * time.Second):
			}
		}
	}
	src := fixtureSource(t, false)
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- g.Commit(context.Background(), old, func() error { return p.LoadSourceCommand(src, old, 0) })
	}()
	channel(t, entered)
	stop, _ := g.Admit(nil, "stop", "")
	stopDone := make(chan error, 1)
	go func() {
		stopDone <- g.Commit(context.Background(), stop, func() error { return p.TransportCommand("stop", stop) })
	}()
	close(release)
	select {
	case err := <-oldDone:
		if !errors.Is(err, playbackcmd.ErrStale) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Load did not cancel")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop commit blocked")
	}
	_, _, state := p.Time()
	if state != "stopped" || src.aborts.Load() != 1 || src.closes.Load() != 1 {
		t.Fatal("obsolete native media committed or source leaked", state)
	}
}
func TestNativeVersionedSameLoadSeekOrdering(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	snap := g.Snapshot()
	snap, err := g.Claim(snap.Boot, snap.Epoch, "a")
	if err != nil {
		t.Fatal(err)
	}
	admit := func(kind string, seq uint64) *playbackcmd.Ticket {
		id := &playbackcmd.Identity{Boot: snap.Boot, Epoch: snap.Epoch, Session: "a", Load: 1, Sequence: seq, Type: kind, Request: "native-command"}
		ticket, err := g.Admit(id, kind, "a")
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	load := admit("load", 1)
	src := fixtureSource(t, true)
	if err := p.LoadSourceCommand(src, load, 0); err != nil {
		t.Fatal(err)
	}
	channel(t, src.entered)
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var trace []int64
	p.seekProbe = func(phase string, ms int64) {
		if phase == "deferred-before-lock" && ms == 400 {
			once.Do(func() {
				close(held)
				select {
				case <-release:
				case <-time.After(3 * time.Second):
				}
			})
		}
		if phase == "applied" {
			mu.Lock()
			trace = append(trace, ms)
			mu.Unlock()
		}
	}
	old := admit("seek", 2)
	result := make(chan error, 1)
	go func() { result <- p.SetTimeCommand(context.Background(), 400, old) }()
	channel(t, held)
	close(src.release)
	waitNative(t, func() bool { _, _, state := p.Time(); return state == "playing" })
	next := admit("seek", 3)
	if err := p.SetTimeCommand(context.Background(), 2200, next); err != nil {
		t.Fatal(err)
	}
	p.Pause()
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, playbackcmd.ErrStale) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("old native seek remained live")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(trace) != 1 || trace[0] != 2200 {
		t.Fatal("old versioned seek applied", trace)
	}
}

type nativeStreamingSource struct {
	*diskcache.StreamingReader
	entered                chan struct{}
	first                  sync.Once
	aborts, closes, active atomic.Int32
}

func (s *nativeStreamingSource) Read(b []byte) (int, error) {
	s.active.Add(1)
	defer s.active.Add(-1)
	s.first.Do(func() { close(s.entered) })
	return s.StreamingReader.Read(b)
}
func (s *nativeStreamingSource) Abort() { s.aborts.Add(1); s.StreamingReader.Abort() }
func (s *nativeStreamingSource) Close() { s.closes.Add(1); s.StreamingReader.Close() }
func TestNativeRealStreamingReaderBlockedReadAndIndependentOwner(t *testing.T) {
	p := orderingPlayer(t)
	cache, err := diskcache.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := cache.BeginStreamingPut("native-streaming", "alac")
	if err != nil {
		t.Fatal(err)
	}
	src := &nativeStreamingSource{StreamingReader: writer.NewReader(), entered: make(chan struct{})}
	other := writer.NewReader()
	var g playbackcmd.Gate
	g.SetSession("a")
	load, _ := g.Admit(nil, "load", "a")
	if err := p.LoadSourceCommand(src, load, 0); err != nil {
		t.Fatal(err)
	}
	channel(t, src.entered)
	if writer.Written() != 0 || src.active.Load() != 1 {
		t.Fatal("real Reader was not waiting for data")
	}
	stop, _ := g.Admit(nil, "stop", "")
	finished := make(chan error, 1)
	go func() { finished <- p.TransportCommand("stop", stop) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("real StreamingReader blocked Stop")
	}
	p.Stop()
	if src.aborts.Load() != 1 || src.closes.Load() != 1 || src.active.Load() != 0 {
		t.Fatal("Reader not joined/released once")
	}
	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatal("native Close released another Reader's writer reference", err)
	}
	data := make([]byte, 3)
	if n, err := other.Read(data); n != 3 || err != nil {
		t.Fatal("other Reader closed prematurely", n, err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Read(data); !errors.Is(err, io.EOF) {
		t.Fatal("other Reader EOF incorrect", err)
	}
	other.Close()
	t.Logf("real reader abort=%d close=%d active=%d; independent Reader survived Commit", src.aborts.Load(), src.closes.Load(), src.active.Load())
}

// Terminal recovery must replace the source under the Seek ticket. The native
// Ended state itself does not make set_time restart playback.
func TestNativeTerminalSeekOwnedReload(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	snap := g.Snapshot()
	snap, err := g.Claim(snap.Boot, snap.Epoch, "a")
	if err != nil {
		t.Fatal(err)
	}
	admit := func(kind string, seq uint64) *playbackcmd.Ticket {
		t.Helper()
		x, e := g.Admit(&playbackcmd.Identity{Boot: snap.Boot, Epoch: snap.Epoch, Session: "a", Load: 1, Sequence: seq, Type: kind, Request: "terminal-native"}, kind, "a")
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	old := fixtureSource(t, false)
	if err := p.LoadSourceCommand(old, admit("load", 1), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(7 * time.Second)
	for {
		_, _, state := p.Time()
		if state == "ended" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("true native Ended deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	seek := admit("seek", 2)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.SetTimeCommand(ctx, 500, seek); !errors.Is(err, ErrTerminalSeek) {
		t.Fatal("Ended must request reload", err)
	}
	next := fixtureSource(t, false)
	if err := p.ReloadSourceCommand(ctx, next, seek, 500); err != nil {
		t.Fatal(err)
	}
	waitNative(t, func() bool { _, _, state := p.Time(); return state == "playing" })
	if old.aborts.Load() != 1 || old.closes.Load() != 1 {
		t.Fatal("old callback source not released exactly once")
	}
	if g.Snapshot().Sequence != 2 || g.Snapshot().Load != 1 || seek.Identity.Type != "seek" {
		t.Fatal("reload invented command identity")
	}
	p.Stop()
	if next.aborts.Load() != 1 || next.closes.Load() != 1 {
		t.Fatal("reload source leaked")
	}
}

func TestNativeTerminalReloadOvertakenBeforeMedia(t *testing.T) {
	for _, kind := range []string{"pause", "stop", "seek"} {
		t.Run(kind, func(t *testing.T) {
			p := orderingPlayer(t)
			var g playbackcmd.Gate
			g.SetSession("a")
			old, _ := g.Admit(nil, "seek", "a")
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			p.seekProbe = func(phase string, _ int64) {
				if phase == "load-before-media" {
					once.Do(func() {
						close(held)
						select {
						case <-release:
						case <-time.After(3 * time.Second):
						}
					})
				}
			}
			src := fixtureSource(t, false)
			done := make(chan error, 1)
			go func() { done <- p.ReloadSourceCommand(context.Background(), src, old, 500) }()
			channel(t, held)
			newer, err := g.Admit(nil, kind, "a")
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			select {
			case err := <-done:
				if !errors.Is(err, playbackcmd.ErrStale) {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("reload cancellation deadline")
			}
			if kind != "seek" {
				if err := p.TransportCommand(kind, newer); err != nil {
					t.Fatal(err)
				}
			}
			if src.aborts.Load() != 1 || src.closes.Load() != 1 {
				t.Fatal("stale reload double released or leaked")
			}
			_, _, state := p.Time()
			if state == "playing" {
				t.Fatal("stale reload played")
			}
		})
	}
}
func TestNativeTerminalReloadCancelledContextOwnsCleanup(t *testing.T) {
	p := orderingPlayer(t)
	var g playbackcmd.Gate
	g.SetSession("a")
	ticket, _ := g.Admit(nil, "seek", "a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := fixtureSource(t, false)
	if err := p.ReloadSourceCommand(ctx, src, ticket, 500); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if ticket.Valid() || src.aborts.Load() != 1 || src.closes.Load() != 1 {
		t.Fatal("cancelled reload source ownership")
	}
}

func TestNativeTerminalReloadOpeningNewerTransport(t *testing.T) {
	for _, kind := range []string{"pause", "stop"} {
		t.Run(kind, func(t *testing.T) {
			p := orderingPlayer(t)
			var g playbackcmd.Gate
			g.SetSession("a")
			old, _ := g.Admit(nil, "seek", "a")
			src := fixtureSource(t, true)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- p.ReloadSourceCommand(ctx, src, old, 500) }()
			channel(t, src.entered)
			newer, err := g.Admit(nil, kind, "a")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, playbackcmd.ErrStale) {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("opening reload ignored newer intent")
			}
			ack := make(chan error, 1)
			go func() { ack <- p.TransportCommand(kind, newer) }()
			select {
			case err := <-ack:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				close(src.release)
				t.Fatal("transport blocked on opening Read")
			}
			close(src.release)
			want := "paused"
			if kind == "stop" {
				want = "stopped"
			}
			waitNative(t, func() bool { _, _, state := p.Time(); return state == want })
			p.Stop()
			if src.aborts.Load() != 1 || src.closes.Load() != 1 {
				t.Fatal("opening reload source leaked or double closed")
			}
		})
	}
}
