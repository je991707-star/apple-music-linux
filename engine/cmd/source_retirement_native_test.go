//go:build vlc_ordering_test && cgo && linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"engine/core/diskcache"
	"engine/core/media"
	"engine/core/pipeline"
	"engine/core/playback"
	"engine/core/vlc"
	"engine/internal/playbackcmd"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type authorityFixtureProvider struct{}

func (authorityFixtureProvider) Open(_ context.Context, r media.OpenRequest) (*media.Session, error) {
	return &media.Session{Kind: "song", Tracks: []media.Track{{Kind: pipeline.KindAudio, Codec: pipeline.CodecALAC, Open: func(context.Context) (*pipeline.Stream, error) {
		return &pipeline.Stream{Kind: pipeline.KindAudio, Codec: pipeline.CodecALAC}, nil
	}}}}, nil
}

type authorityRead struct {
	vlc.Source
	entered                chan struct{}
	first                  sync.Once
	active, aborts, closes atomic.Int32
}

func (s *authorityRead) Read(b []byte) (int, error) {
	s.active.Add(1)
	defer s.active.Add(-1)
	s.first.Do(func() { close(s.entered) })
	return s.Source.Read(b)
}
func (s *authorityRead) Abort() { s.aborts.Add(1); s.Source.Abort() }
func (s *authorityRead) Close() { s.closes.Add(1); s.Source.Close() }
func authorityChannel(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("authority barrier deadline")
	}
}
func authorityWait(t *testing.T, fn func() bool) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("authority condition deadline")
		case <-tick.C:
		}
	}
}

type authorityRig struct {
	s        *APIServer
	sink     *vlc.IsolatedSink
	id       string
	writer   *diskcache.StreamingPutWriter
	finished atomic.Bool
	readsMu  sync.Mutex
	reads    []*authorityRead
	traceMu  sync.Mutex
	trace    []string
	data     []byte
}

func newAuthorityRig(t *testing.T, withData bool) *authorityRig {
	t.Helper()
	p, sink, e := vlc.NewIsolatedPlayer()
	if e != nil {
		t.Fatal(e)
	}
	m := playback.NewWithProvider(authorityFixtureProvider{})
	c, e := diskcache.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	sess, e := m.Open(context.Background(), playback.OpenRequest{AssetID: "authority-synthetic", Private: true})
	if e != nil {
		t.Fatal(e)
	}
	w, e := c.BeginStreamingPut(sess.AssetID, sess.Codec)
	if e != nil || w == nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(os.Getenv("ADAM_VLC_TEST_FIXTURE"))
	if e != nil {
		t.Fatal(e)
	}
	ref, e := os.ReadFile(filepath.Join(filepath.Dir(os.Getenv("ADAM_VLC_TEST_FIXTURE")), "reference.s16le"))
	if e != nil {
		t.Fatal(e)
	}
	sink.Reset(ref)
	r := &authorityRig{s: &APIServer{pm: m, diskCache: c, vlcPlayer: p, alac: newALACGate()}, sink: sink, id: sess.ID, writer: w, data: data}
	if withData {
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	retirementHooks.Store(r.s, retirementTestHooks{wrap: func(src vlc.Source) vlc.Source {
		x := &authorityRead{Source: src, entered: make(chan struct{})}
		r.readsMu.Lock()
		r.reads = append(r.reads, x)
		r.readsMu.Unlock()
		return x
	}})
	p.SetOrderingObserver(func(phase string, _ int64) { r.traceMu.Lock(); r.trace = append(r.trace, phase); r.traceMu.Unlock() })
	t.Cleanup(func() { r.close(t) })
	return r
}
func (r *authorityRig) close(t *testing.T) {
	var receipts []*sourceRetirement
	r.s.sourceRetirements.Range(func(_, v any) bool { receipts = append(receipts, v.(*sourceRetirement)); return true })
	r.s.cancelVLCCommands()
	r.s.vlcPlayer.Close()
	for _, v := range receipts {
		authorityChannel(t, v.done)
		v.mu.Lock()
		e := v.err
		v.mu.Unlock()
		if e != nil {
			t.Error(e)
		}
	}
	r.sink.Close()
	retirementHooks.Delete(r.s)
	if !r.finished.Swap(true) {
		r.writer.Discard()
	}
	r.readsMu.Lock()
	defer r.readsMu.Unlock()
	var readerMetrics []map[string]int32
	for _, x := range r.reads {
		readerMetrics = append(readerMetrics, map[string]int32{"activeReads": x.active.Load(), "abortCount": x.aborts.Load(), "closeCount": x.closes.Load()})
		if x.active.Load() != 0 || x.aborts.Load() != 1 || x.closes.Load() != 1 {
			t.Errorf("reader ownership active=%d abort=%d close=%d", x.active.Load(), x.aborts.Load(), x.closes.Load())
		}
	}
	fd, _ := os.ReadDir("/proc/self/fd")
	t.Logf("after-cleanup FD=%d goroutines=%d", len(fd), runtime.NumGoroutine())
	r.traceMu.Lock()
	defer r.traceMu.Unlock()
	b, _ := json.Marshal(map[string]any{"nativeSequence": r.trace, "readers": len(r.reads), "readerMetrics": readerMetrics, "goroutines": runtime.NumGoroutine()})
	t.Logf("authority evidence=%s", b)
}
func (r *authorityRig) load(id string, start int64) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"sessionId": id, "startMs": start})
	w := httptest.NewRecorder()
	r.s.handleVLCLoad(w, httptest.NewRequest("POST", "/load", bytes.NewReader(b)))
	return w
}
func (r *authorityRig) receipt(t *testing.T) *sourceRetirement {
	t.Helper()
	var v *sourceRetirement
	r.s.sourceRetirements.Range(func(key, x any) bool {
		if key.(*playbackcmd.Lease).Identity.Session != r.id {
			return true
		}
		v = x.(*sourceRetirement)
		return false
	})
	if v == nil {
		t.Fatal("missing lease receipt")
	}
	return v
}
func TestAuthorityNativeReleaseBoundaries(t *testing.T) {
	for _, phase := range []string{"prepare", "authority-before-entry", "native-play"} {
		t.Run(phase, func(t *testing.T) {
			r := newAuthorityRig(t, phase != "native-play")
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			block := func() {
				once.Do(func() {
					close(entered)
					select {
					case <-release:
					case <-time.After(3 * time.Second):
					}
				})
			}
			if phase == "prepare" {
				h, _ := retirementHooks.Load(r.s)
				v := h.(retirementTestHooks)
				v.prepare = func(vlc.Source) { block() }
				retirementHooks.Store(r.s, v)
			} else {
				r.s.vlcPlayer.SetOrderingObserver(func(p string, _ int64) {
					r.traceMu.Lock()
					r.trace = append(r.trace, p)
					r.traceMu.Unlock()
					if p == phase {
						block()
					}
				})
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- r.load(r.id, 0) }()
			authorityChannel(t, entered)
			receipt := r.receipt(t)
			hookEntered, hookRelease := make(chan struct{}), make(chan struct{})
			r.s.pm.SetReleaseHook(func(string) { close(hookEntered); <-hookRelease })
			releaseDone := make(chan struct{})
			go func() { r.s.pm.Release(r.id); close(releaseDone) }()
			authorityChannel(t, hookEntered)
			// Session deletion already revoked authority although the old cleanup hook is blocked.
			close(release)
			w := <-done
			if w.Code != 409 {
				t.Fatalf("retired load accepted: %d %s", w.Code, w.Body.String())
			}
			authorityChannel(t, receipt.done)
			if phase != "native-play" && r.s.vlcPlayer.OrderingOwnership()["physicalLoad"].(int) != 0 {
				t.Fatal("pre-entry release reached native media")
			}
			if phase == "native-play" {
				var ack map[string]any
				json.Unmarshal(w.Body.Bytes(), &ack)
				if ack["nativeCommitEntered"] != true || ack["mayHaveApplied"] != true {
					t.Fatal("C entry reported as rollback", ack)
				}
			}
			close(hookRelease)
			authorityChannel(t, releaseDone)
		})
	}
}
func TestAuthorityNativeAReleaseCannotCleanB(t *testing.T) {
	r := newAuthorityRig(t, true)
	if w := r.load(r.id, 0); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r.readsMu.Lock()
	a := r.reads[0]
	r.readsMu.Unlock()
	authorityChannel(t, a.entered)
	// Same asset, distinct Session and lease, sharing the existing cache producer.
	b, e := r.s.pm.Open(context.Background(), playback.OpenRequest{AssetID: "authority-synthetic", Private: true})
	if e != nil {
		t.Fatal(e)
	}
	ref, err := os.ReadFile(filepath.Join(filepath.Dir(os.Getenv("ADAM_VLC_TEST_FIXTURE")), "reference.s16le"))
	if err != nil {
		t.Fatal(err)
	}
	r.s.vlcPlayer.SetOrderingObserver(func(phase string, _ int64) {
		if phase == "load-before-media" {
			r.sink.Reset(ref)
		}
		r.traceMu.Lock()
		r.trace = append(r.trace, phase)
		r.traceMu.Unlock()
	})
	if w := r.load(b.ID, 0); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := r.s.vlcPlayer.OrderingOwnership()
	oldReceipt := r.receipt(t)
	r.s.pm.Release(r.id)
	authorityChannel(t, oldReceipt.done)
	after := r.s.vlcPlayer.OrderingOwnership()
	x, _ := json.Marshal(before)
	y, _ := json.Marshal(after)
	if !bytes.Equal(x, y) {
		t.Fatalf("A cleanup changed B: before=%s after=%s", x, y)
	}
	// A retirement must not close the writer's FD or the B reader. Complete producer.
	if e := r.writer.Commit(); e != nil {
		t.Fatal(e)
	}
	r.finished.Store(true)
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for r.sink.Snapshot()["full_pcm_exact"] != true {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("B full PCM deadline: %v", r.sink.Snapshot())
		}
	}
	t.Logf("B PCM=%v identity=%v", r.sink.Snapshot(), after)
}
func TestAuthorityNativeBlockedReadAndBoot(t *testing.T) {
	r := newAuthorityRig(t, false)
	if w := r.load(r.id, 0); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r.readsMu.Lock()
	src := r.reads[0]
	r.readsMu.Unlock()
	authorityChannel(t, src.entered)
	authorityWait(t, func() bool { return src.active.Load() == 1 })
	receipt := r.receipt(t)
	handle := r.s.vlcPlayer.OrderingOwnership()["callbackHandle"].(uint64)
	r.s.pm.RevokeSources()
	authorityChannel(t, receipt.done)
	if !vlc.OrderingHandleDeleted(handle) {
		t.Fatal("retired callback handle still live")
	}
	t.Logf("retired cgo handle=%d deleted=true", handle)
	if src.active.Load() != 0 || src.closes.Load() != 1 {
		t.Fatal("retirement could not unblock real StreamingReader")
	}
	snap := r.s.vlcGate.order.Snapshot()
	_, e := r.s.pm.AdmitSource(r.id, snap.Boot, func() (*playbackcmd.Ticket, error) { return r.s.vlcGate.order.Admit(nil, "load", r.id) })
	if e == nil {
		t.Fatal("shutdown lease resurrected")
	}
}
func TestAuthorityNativeTerminalRetirement(t *testing.T) {
	r := newAuthorityRig(t, true)
	if w := r.load(r.id, 0); w.Code != 200 {
		t.Fatal(w.Code)
	}
	r.readsMu.Lock()
	old := r.reads[0]
	r.readsMu.Unlock()
	authorityChannel(t, old.entered)
	if e := r.writer.Commit(); e != nil {
		t.Fatal(e)
	}
	r.finished.Store(true)
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		_, _, state := r.s.vlcPlayer.Time()
		if state == "ended" {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("native Ended deadline")
		}
	}
	t.Setenv("ADAM_VLC_ORDERED", "1")
	t.Setenv("ADAM_API_TOKEN", "synthetic-retirement-token")
	snap := r.s.vlcGate.order.Snapshot()
	snap, e := r.s.vlcGate.order.Claim(snap.Boot, snap.Epoch, r.id)
	if e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	v, _ := retirementHooks.Load(r.s)
	h := v.(retirementTestHooks)
	h.prepare = func(vlc.Source) { close(entered); <-release }
	retirementHooks.Store(r.s, h)
	id := playbackcmd.Identity{Boot: snap.Boot, Epoch: snap.Epoch, Session: r.id, Load: 0, Sequence: 1, Type: "seek", Request: "terminal-retirement"}
	b, _ := json.Marshal(map[string]any{"sessionId": r.id, "posMs": 500, "command": id})
	req := httptest.NewRequest("POST", "/seek", bytes.NewReader(b))
	req.Header.Set("X-Api-Token", "synthetic-retirement-token")
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); r.s.handleVLCSeek(w, req); result <- w }()
	authorityChannel(t, entered)
	receipt := r.receipt(t)
	r.s.pm.Release(r.id)
	close(release)
	w := <-result
	if w.Code != 409 {
		t.Fatal("terminal reload used retired lease", w.Code, w.Body.String())
	}
	authorityChannel(t, receipt.done)
	if r.sink.Snapshot()["full_pcm_exact"] != true {
		t.Fatal("natural PCM corrupted before retirement")
	}
}
func TestAuthorityNativeRepeatedHandoff(t *testing.T) {
	beforeFD, _ := os.ReadDir("/proc/self/fd")
	beforeG := runtime.NumGoroutine()
	r := newAuthorityRig(t, true)
	current := r.id
	for i := 0; i < 8; i++ {
		if w := r.load(current, 0); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if i > 0 {
			old := r.id
			receipt := r.receipt(t)
			r.s.pm.Release(old)
			authorityChannel(t, receipt.done)
			r.id = current
		}
		b, e := r.s.pm.Open(context.Background(), playback.OpenRequest{AssetID: "authority-synthetic", Private: true})
		if e != nil {
			t.Fatal(e)
		}
		current = b.ID
	}
	afterFD, _ := os.ReadDir("/proc/self/fd")
	t.Logf("handoffs=8 FD before=%d during=%d goroutines before=%d during=%d physical=%v", len(beforeFD), len(afterFD), beforeG, runtime.NumGoroutine(), r.s.vlcPlayer.OrderingOwnership())
	// Exact reader counters and zero active Read are asserted during rig cleanup.
}
func TestAuthorityNativeTerminalActiveLeasePCM(t *testing.T) {
	r := newAuthorityRig(t, true)
	if w := r.load(r.id, 0); w.Code != 200 {
		t.Fatal(w.Code)
	}
	r.readsMu.Lock()
	src := r.reads[0]
	r.readsMu.Unlock()
	authorityChannel(t, src.entered)
	if e := r.writer.Commit(); e != nil {
		t.Fatal(e)
	}
	r.finished.Store(true)
	end := time.NewTimer(7 * time.Second)
	defer end.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		_, _, state := r.s.vlcPlayer.Time()
		if state == "ended" {
			break
		}
		select {
		case <-tick.C:
		case <-end.C:
			t.Fatal("initial Ended deadline")
		}
	}
	ref, e := os.ReadFile(filepath.Join(filepath.Dir(os.Getenv("ADAM_VLC_TEST_FIXTURE")), "reference.s16le"))
	if e != nil {
		t.Fatal(e)
	}
	r.s.vlcPlayer.SetOrderingObserver(func(phase string, _ int64) {
		if phase == "load-before-media" {
			r.sink.Reset(ref)
		}
		r.traceMu.Lock()
		r.trace = append(r.trace, phase)
		r.traceMu.Unlock()
	})
	t.Setenv("ADAM_VLC_ORDERED", "1")
	t.Setenv("ADAM_API_TOKEN", "synthetic-retirement-token")
	snap := r.s.vlcGate.order.Snapshot()
	snap, e = r.s.vlcGate.order.Claim(snap.Boot, snap.Epoch, r.id)
	if e != nil {
		t.Fatal(e)
	}
	id := playbackcmd.Identity{Boot: snap.Boot, Epoch: snap.Epoch, Session: r.id, Load: 0, Sequence: 1, Type: "seek", Request: "active-terminal"}
	b, _ := json.Marshal(map[string]any{"sessionId": r.id, "posMs": 500, "command": id})
	req := httptest.NewRequest("POST", "/seek", bytes.NewReader(b))
	req.Header.Set("X-Api-Token", "synthetic-retirement-token")
	w := httptest.NewRecorder()
	r.s.handleVLCSeek(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	end.Reset(7 * time.Second)
	for {
		_, _, state := r.s.vlcPlayer.Time()
		if state == "ended" {
			break
		}
		select {
		case <-tick.C:
		case <-end.C:
			t.Fatal("terminal Ended deadline")
		}
	}
	v := r.sink.Snapshot()
	origin := v["first_post_flush_pcm_ms"].(float64)
	if math.Abs(origin-500) > 4096.0/48 || v["post_flush_suffix_exact"] != true {
		t.Fatal("active lease terminal PCM", v)
	}
	t.Logf("active lease terminal PCM=%v ownership=%v", v, r.s.vlcPlayer.OrderingOwnership())
}
