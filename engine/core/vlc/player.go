// Package vlc wraps libvlc to play a single HTTP audio stream.
// It is used for lossless/ALAC tracks that the browser's MSE cannot handle.
package vlc

// #cgo LDFLAGS: -lvlc
// #include <vlc/vlc.h>
// #include <stdlib.h>
import "C"
import (
	"context"
	"engine/internal/playbackcmd"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// Player is a single-track libvlc player. Only one track plays at a time;
// Load replaces the current media. Safe for concurrent use.
type Player struct {
	mu            sync.Mutex
	inst          *C.libvlc_instance_t
	mp            *C.libvlc_media_player_t
	lastURL       string
	outputBackend string         // known only when an output was explicitly attached
	volume        int            // 0-200; default 100
	loadGen       int            // incremented on each Load; goroutines compare to detect staleness
	cur           *callbackMedia // non-nil while a LoadSource media is attached
	sourceBoot    string
	lease         *playbackcmd.Lease
	seekGen       uint64
	// Deterministic observer configured before use by build-tagged native tests.
	seekProbe func(string, int64)
}

// New creates a libvlc instance and media player.
// Returns an error if libvlc cannot be initialised (missing shared library).
func New() (*Player, error) {
	return newPlayer("Adam", "io.github.silentone12725.Adam")
}

func newPlayer(applicationName, applicationID string) (*Player, error) {
	inst := C.libvlc_new(0, nil)
	if inst == nil {
		return nil, fmt.Errorf("vlc: libvlc_new failed — is libvlc.so.5 in LD_LIBRARY_PATH?")
	}
	mp := C.libvlc_media_player_new(inst)
	if mp == nil {
		C.libvlc_release(inst)
		return nil, fmt.Errorf("vlc: libvlc_media_player_new failed")
	}
	// Stable audio identity lets the desktop mixer retain its per-app settings (from PR #1: https://github.com/silentone12725/apple-music-linux/pull/1).
	name := C.CString(applicationName)
	agent := C.CString("Adam/1.4.1")
	appID := C.CString(applicationID)
	version := C.CString("1.4.1")
	icon := C.CString("adam")
	C.libvlc_set_user_agent(inst, name, agent)
	C.libvlc_set_app_id(inst, appID, version, icon)
	for _, value := range []*C.char{name, agent, appID, version, icon} {
		C.free(unsafe.Pointer(value))
	}
	return &Player{inst: inst, mp: mp, volume: 100}, nil
}

// Load stops any current playback and starts playing url from the beginning.
func (p *Player) Load(url string) error { return p.LoadCommand(url, nil, 0) }

// LoadSource stops any current playback and plays src directly through libvlc
// read/seek callbacks — no loopback HTTP. The player takes ownership of src
// and closes it when the media is replaced, stopped or the player is closed.
func (p *Player) LoadSource(src Source) error { return p.LoadSourceCommand(src, nil, 0) }

// detachLocked stops playback and frees the current callback source, if any.
// Abort runs first so a Read blocked on an in-progress download returns and
// libvlc_media_player_stop can join the input thread. The media is detached so
// nothing can re-invoke the open callback on a released source.
func (p *Player) detachLocked() {
	if p.cur == nil {
		return
	}
	p.cur.src.Abort()
	C.libvlc_media_player_stop(p.mp)
	C.libvlc_media_player_set_media(p.mp, nil)
	p.cur.release()
	p.cur = nil
	p.lease = nil
}

func (p *Player) playLocked() error {
	p.loadGen++
	if ret := C.libvlc_media_player_play(p.mp); ret != 0 {
		return fmt.Errorf("vlc: play failed (ret %d)", int(ret))
	}
	if p.seekProbe != nil {
		p.seekProbe("native-play", 0)
	}
	// Volume/mute belong to the existing player and the desktop mixer. Repeated
	// reapplication here overwrote external mixer changes on every new track.
	return nil
}

// Volume reports the live mixer state rather than the last slider request.
// Together with not re-applying volume on every Load, this is from PR #1 (https://github.com/silentone12725/apple-music-linux/pull/1).
func (p *Player) Volume() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil {
		return p.volume, false
	}
	vol := int(C.libvlc_audio_get_volume(p.mp))
	if vol < 0 {
		vol = p.volume
	}
	return vol, C.libvlc_audio_get_mute(p.mp) == 1
}

// SetTime seeks within the currently loaded media to posMs milliseconds.
// If VLC is already playing the call is synchronous (instant disk-cache seek).
// If VLC is still opening (e.g. right after Load), it polls up to 10 s for the
// playing state before applying — so it is safe to call immediately after Load.
func (p *Player) SetTime(posMs int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if posMs >= 0 && p.mp != nil {
		p.seekLocked(posMs, nil)
	}
}

// Time returns the current position and total length in milliseconds, plus a
// short state string ("playing", "paused", "stopped", "ended", "error").
func (p *Player) Time() (posMs, lengthMs int64, state string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil {
		return 0, 0, "stopped"
	}
	posMs = int64(C.libvlc_media_player_get_time(p.mp))
	lengthMs = int64(C.libvlc_media_player_get_length(p.mp))
	state = vlcStateName(C.libvlc_media_player_get_state(p.mp))
	return
}

// SourceEnded reports whether the attached source delivered all of its data and ended cleanly, as opposed to failing or
// being aborted. libvlc reaching its end while this is false means the read failed, not that the track ended.
func (p *Player) SourceEnded() bool {
	p.mu.Lock()
	cm := p.cur
	p.mu.Unlock()
	return cm != nil && cm.eof.Load()
}

// Pause pauses playback.
func (p *Player) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil || p.sourceBoot != "" {
		return
	}
	p.seekGen++
	C.libvlc_media_player_set_pause(p.mp, 1)
}

// Resume resumes playback.
func (p *Player) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil || p.sourceBoot != "" {
		return
	}
	p.seekGen++
	C.libvlc_media_player_set_pause(p.mp, 0)
}

// SetVolume sets the audio volume. vol is 0–200 (100 = 100%).
func (p *Player) SetVolume(vol int) {
	if vol < 0 {
		vol = 0
	}
	if vol > 200 {
		vol = 200
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = vol
	if p.mp == nil {
		return
	}
	C.libvlc_audio_set_mute(p.mp, 0)
	C.libvlc_audio_set_volume(p.mp, C.int(vol))
}

// Stop halts playback immediately without releasing the player.
// The player remains usable for a subsequent Load call.
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp != nil {
		p.detachLocked()
		C.libvlc_media_player_stop(p.mp)
		p.lease = nil
		p.loadGen++ // cancel delayed seeks even if no new track follows Stop
	}
}

// Close releases all libvlc resources.
func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadGen++ // background pollers see stale and never touch the released player
	if p.mp != nil {
		p.detachLocked()
		C.libvlc_media_player_stop(p.mp)
		C.libvlc_media_player_release(p.mp)
		p.mp = nil
	}
	if p.inst != nil {
		C.libvlc_release(p.inst)
		p.inst = nil
	}
}

func vlcStateName(s C.libvlc_state_t) string {
	switch s {
	case C.libvlc_NothingSpecial:
		return "stopped"
	case C.libvlc_Opening:
		return "opening"
	case C.libvlc_Buffering:
		return "buffering"
	case C.libvlc_Playing:
		return "playing"
	case C.libvlc_Paused:
		return "paused"
	case C.libvlc_Stopped:
		return "stopped"
	case C.libvlc_Ended:
		return "ended"
	case C.libvlc_Error:
		return "error"
	default:
		return "unknown"
	}
}

func (p *Player) LoadSourceCommand(src Source, t *playbackcmd.Ticket, startMs int64) error {
	_, err := p.loadSourceCommand(src, t, startMs)
	return err
}

func (p *Player) ReloadSourceCommand(ctx context.Context, src Source, t *playbackcmd.Ticket, startMs int64) error {
	if t == nil || ctx.Err() != nil {
		src.Abort()
		src.Close()
		if t == nil {
			return playbackcmd.ErrStale
		}
		t.Cancel()
		return ctx.Err()
	}
	stop := context.AfterFunc(ctx, t.Cancel)
	defer stop()
	done, err := p.loadSourceCommand(src, t, startMs)
	if err != nil || done == nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-t.Done():
		return playbackcmd.ErrStale
	case <-ctx.Done():
		t.Cancel()
		return ctx.Err()
	}
}

func (p *Player) loadSourceCommand(src Source, t *playbackcmd.Ticket, startMs int64) (<-chan error, error) {
	src = &onceSource{Source: src}
	if t != nil && t.SourceLease() != nil {
		src = watchLeaseSource(src, t.SourceLease())
	}
	if !validCommand(t) {
		src.Abort()
		src.Close()
		return nil, playbackcmd.ErrStale
	}
	// A cancelled preparation may have entered a native operation waiting on Read.
	// Abort has no Player/control lock dependency; join it before transferring ownership.
	var stop func() bool
	var aborted chan struct{}
	if t != nil {
		aborted = make(chan struct{})
		stop = context.AfterFunc(t.Context(), func() { src.Abort(); close(aborted) })
	}
	defer func() {
		if stop != nil && !stop() {
			<-aborted
		}
	}()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil || !validCommand(t) {
		src.Abort()
		src.Close()
		if p.mp == nil {
			return nil, errors.New("vlc: player closed")
		}
		return nil, playbackcmd.ErrStale
	}
	if !p.enterSource(t, startMs) {
		src.Abort()
		src.Close()
		return nil, playbackcmd.ErrStale
	}
	p.detachLocked()
	if p.seekProbe != nil {
		p.seekProbe("load-before-media", startMs)
	}
	if !validCommand(t) {
		src.Abort()
		src.Close()
		return nil, playbackcmd.ErrStale
	}
	cm := newCallbackMedia(src)
	media := cm.newMedia(p.inst)
	if media == nil {
		cm.release()
		return nil, errors.New("vlc: libvlc_media_new_callbacks failed")
	}
	defer C.libvlc_media_release(media)
	C.libvlc_media_player_set_media(p.mp, media)
	p.cur = cm
	p.lease = nil
	if t != nil {
		p.lease = t.SourceLease()
	}
	p.lastURL = ""
	if err := p.playLocked(); err != nil {
		p.detachLocked()
		return nil, err
	}
	if startMs > 0 {
		return p.seekLocked(startMs, t), nil
	}
	return nil, nil
}

func validCommand(t *playbackcmd.Ticket) bool { return t == nil || t.Valid() }

func (p *Player) QueueTimeCommand(posMs int64, t *playbackcmd.Ticket) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if posMs < 0 || p.mp == nil || !validCommand(t) {
		return playbackcmd.ErrStale
	}
	p.seekLocked(posMs, t)
	return nil
}

func (p *Player) SetTimeCommand(ctx context.Context, posMs int64, t *playbackcmd.Ticket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t != nil {
		stop := context.AfterFunc(ctx, t.Cancel)
		defer stop()
	}
	if p.seekProbe != nil {
		p.seekProbe("command-before-lock", posMs)
	}
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	if posMs < 0 || p.mp == nil || !validCommand(t) {
		p.mu.Unlock()
		return playbackcmd.ErrStale
	}
	done := p.seekLocked(posMs, t)
	p.mu.Unlock()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if t != nil {
			t.Cancel()
		}
		return ctx.Err()
	}
}

func (p *Player) seekLocked(posMs int64, t *playbackcmd.Ticket) <-chan error {
	p.seekGen++
	seekGen, loadGen := p.seekGen, p.loadGen
	done := make(chan error, 1)
	state := C.libvlc_media_player_get_state(p.mp)
	if t != nil && state == C.libvlc_Ended {
		done <- ErrTerminalSeek
		return done
	}
	if state == C.libvlc_Playing || state == C.libvlc_Paused {
		if !validCommand(t) {
			done <- playbackcmd.ErrStale
			return done
		}
		if !p.enterSource(t, posMs) {
			done <- playbackcmd.ErrStale
			return done
		}
		C.libvlc_media_player_set_time(p.mp, C.libvlc_time_t(posMs))
		if p.seekProbe != nil {
			p.seekProbe("applied", posMs)
		}
		done <- nil
		return done
	}
	go func() {
		defer func() {
			if p.seekProbe != nil {
				p.seekProbe("deferred-done", posMs)
			}
		}()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		var cancelled <-chan struct{}
		if t != nil {
			cancelled = t.Done()
		}
		for {
			select {
			case <-cancelled:
				done <- playbackcmd.ErrStale
				return
			case <-timer.C:
				done <- context.DeadlineExceeded
				return
			case <-tick.C:
				if p.seekProbe != nil {
					p.seekProbe("deferred-before-lock", posMs)
				}
				p.mu.Lock()
				if p.mp == nil || p.loadGen != loadGen || p.seekGen != seekGen || !validCommand(t) {
					p.mu.Unlock()
					done <- playbackcmd.ErrStale
					return
				}
				state := C.libvlc_media_player_get_state(p.mp)
				if t != nil && state == C.libvlc_Ended {
					p.mu.Unlock()
					done <- ErrTerminalSeek
					return
				}
				if state == C.libvlc_Playing || state == C.libvlc_Paused {
					if !p.enterSource(t, posMs) {
						p.mu.Unlock()
						done <- playbackcmd.ErrStale
						return
					}
					C.libvlc_media_player_set_time(p.mp, C.libvlc_time_t(posMs))
					if p.seekProbe != nil {
						p.seekProbe("applied", posMs)
					}
					p.mu.Unlock()
					done <- nil
					return
				}
				p.mu.Unlock()
			}
		}
	}()
	return done
}

func (p *Player) TransportCommand(kind string, t *playbackcmd.Ticket) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil || !validCommand(t) {
		return playbackcmd.ErrStale
	}
	if !p.enterSource(t, 0) {
		return playbackcmd.ErrStale
	}
	p.seekGen++
	switch kind {
	case "pause":
		C.libvlc_media_player_set_pause(p.mp, 1)
	case "resume":
		C.libvlc_media_player_set_pause(p.mp, 0)
	case "stop":
		p.detachLocked()
		p.lease = nil
		C.libvlc_media_player_stop(p.mp)
		p.lease = nil
		p.loadGen++
	default:
		return errors.New("invalid native transport")
	}
	if p.seekProbe != nil {
		p.seekProbe("transport-"+kind, 0)
	}
	return nil
}

// Each native media owns one source reference; this does not alter StreamingReader.Close.
type onceSource struct {
	Source
	abort, close sync.Once
}

func (s *onceSource) Abort() { s.abort.Do(s.Source.Abort) }
func (s *onceSource) Close() { s.close.Do(s.Source.Close) }

var ErrTerminalSeek = errors.New("vlc: ended media requires reload for seek")

func (p *Player) LoadCommand(url string, t *playbackcmd.Ticket, startMs int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !validCommand(t) {
		return playbackcmd.ErrStale
	}
	if p.mp == nil {
		return fmt.Errorf("vlc: player closed")
	}

	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))

	media := C.libvlc_media_new_location(p.inst, cURL)
	if media == nil {
		return fmt.Errorf("vlc: libvlc_media_new_location failed for %s", url)
	}
	defer C.libvlc_media_release(media)

	if !p.enterSource(t, startMs) {
		return playbackcmd.ErrStale
	}
	p.detachLocked()
	if !validCommand(t) {
		return playbackcmd.ErrStale
	}
	C.libvlc_media_player_set_media(p.mp, media)
	p.lease = nil
	if t != nil {
		p.lease = t.SourceLease()
	}
	p.lastURL = url
	if err := p.playLocked(); err != nil {
		return err
	}
	if startMs > 0 {
		p.seekLocked(startMs, t)
	}
	return nil
}

// enterSource is called only under Player.mu. Its lease lock is never held
// through C. Entry is a logical irrevocable point, not a claim that C returned.
func (p *Player) enterSource(t *playbackcmd.Ticket, ms int64) bool {
	if p.seekProbe != nil {
		p.seekProbe("authority-before-entry", ms)
	}
	if t == nil {
		return p.lease == nil && p.sourceBoot == ""
	}
	if p.lease != nil && (t.SourceLease() == nil || (t.Identity.Type != "load" && t.SourceLease() != p.lease)) {
		return false
	}
	if l := t.SourceLease(); l != nil {
		if p.sourceBoot != "" && p.sourceBoot != l.Identity.Boot {
			return false
		}
		if !t.EnterNative() {
			return false
		}
		p.sourceBoot = l.Identity.Boot
		return true
	}
	return p.sourceBoot == "" && t.EnterNative()
}
func (p *Player) StopRetiredLease(l *playbackcmd.Lease) (bool, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	physical := p.loadGen
	if p.mp == nil || p.lease != l || l.Active() {
		return false, physical
	}
	p.detachLocked()
	C.libvlc_media_player_stop(p.mp)
	p.lease = nil
	p.loadGen++
	p.seekGen++
	if p.seekProbe != nil {
		p.seekProbe("retirement-stop", int64(physical))
	}
	return true, physical
}

// The watcher can Abort while Native holds Player.mu and is waiting for Read.
type leaseSource struct {
	close sync.Once
	Source
	stop func() bool
	done chan struct{}
}

func watchLeaseSource(src Source, l *playbackcmd.Lease) Source {
	w := &leaseSource{Source: src, done: make(chan struct{})}
	w.stop = context.AfterFunc(leaseContext{l}, func() { src.Abort(); close(w.done) })
	return w
}
func (w *leaseSource) Close() {
	w.close.Do(func() {
		if !w.stop() {
			<-w.done
		}
		w.Source.Close()
	})
}

type leaseContext struct{ l *playbackcmd.Lease }

func (c leaseContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c leaseContext) Done() <-chan struct{}       { return c.l.Done() }
func (c leaseContext) Err() error {
	if !c.l.Active() {
		return context.Canceled
	}
	return nil
}
func (c leaseContext) Value(any) any { return nil }

// SetVolumeForLease changes only this app Player. It does not invalidate Seek/
// Transport tickets. Current media, source lifetime and volume revision are
// rechecked at the actual Native entry under the same Player -> Lease order.
var ErrVolumeOutputUnsupported = errors.New("vlc: output volume capability unavailable")

var ErrVolumeUnconfirmed = errors.New("vlc: native volume unconfirmed; may have applied")

func (p *Player) OutputBackend() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.outputBackend == "" {
		return "native-unidentified"
	}
	return p.outputBackend
}

func (p *Player) SetVolumeForLease(value int, l *playbackcmd.Lease, current func() bool) error {
	check := func() bool {
		return p.mp != nil && l != nil && p.lease == l && l.Active() && value >= 0 && value <= 200 && current()
	}
	p.mu.Lock()
	if !check() {
		p.mu.Unlock()
		return playbackcmd.ErrStale
	}
	if p.outputBackend == "callback-memory" {
		p.mu.Unlock()
		return ErrVolumeOutputUnsupported
	}
	err := l.WhileActive(func() error {
		if !current() {
			return playbackcmd.ErrStale
		}
		if C.libvlc_audio_set_volume(p.mp, C.int(value)) != 0 {
			return ErrVolumeUnconfirmed
		}
		return nil
	})
	p.mu.Unlock()
	if err != nil {
		return err
	}
	// Pulse volume acknowledgments are asynchronous. Never hold Player/Lease
	// locks while waiting, and never reissue a setter or mask unsupported output.
	deadline := time.NewTimer(250 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		confirmed := false
		p.mu.Lock()
		if !check() {
			p.mu.Unlock()
			return playbackcmd.ErrStale
		}
		err = l.WhileActive(func() error {
			if !current() {
				return playbackcmd.ErrStale
			}
			confirmed = int(C.libvlc_audio_get_volume(p.mp)) == value
			if confirmed {
				p.volume = value
			}
			return nil
		})
		p.mu.Unlock()
		if err != nil {
			return err
		}
		if confirmed {
			return nil
		}
		select {
		case <-l.Done():
			return playbackcmd.ErrStale
		case <-deadline.C:
			return ErrVolumeUnconfirmed
		case <-tick.C:
		}
	}
}

// OwnStatusForLease avoids attributing a previous physical media's timeline to
// an accepted source whose Native Load has not yet committed.
func (p *Player) OwnStatusForLease(l *playbackcmd.Lease) (int64, int64, string, *bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil || l == nil || p.lease != l || !l.Active() {
		return -1, -1, "unloaded", nil
	}
	eof := false
	if p.cur != nil {
		eof = p.cur.eof.Load()
	}
	return int64(C.libvlc_media_player_get_time(p.mp)), int64(C.libvlc_media_player_get_length(p.mp)), vlcStateName(C.libvlc_media_player_get_state(p.mp)), &eof
}
