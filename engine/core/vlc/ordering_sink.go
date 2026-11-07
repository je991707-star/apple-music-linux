//go:build vlc_ordering_test && cgo && linux

package vlc

/*
#include <stdint.h>
#include <stdlib.h>
#include <vlc/vlc.h>
extern void isolatedPCM(void *, void *, unsigned, int64_t);
extern void isolatedFlush(void *, int64_t);
extern void isolatedEvent(libvlc_event_t *, void *);
static int isolatedEventType(const libvlc_event_t *e) { return e->type; }
*/
import "C"

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"
)

// IsolatedSink is compiled only for the account-free synthetic API executable.
// Close must follow Player.Close: libVLC has then joined all audio callbacks.
type isolatedSegment struct {
	pcm    []byte
	origin float64
}

type IsolatedSink struct {
	diagnosticOrigin          time.Time
	lastPCMNS, completedPCMNS int64

	segments []isolatedSegment

	mu              sync.Mutex
	opaque          unsafe.Pointer
	handle          cgo.Handle
	pcm, reference  []byte
	firstAfterFlush float64
	flushes         int
	resetSerial     uint64
	binding         map[string]any
	history         []map[string]any
	events          []map[string]any
}

func NewIsolatedPlayer() (*Player, *IsolatedSink, error) {
	p, err := newOrderingPlayer()
	if err != nil {
		return nil, nil, err
	}
	p.outputBackend = "callback-memory"
	s := &IsolatedSink{firstAfterFlush: -1}
	s.handle = cgo.NewHandle(s)
	s.opaque = C.malloc(C.size_t(unsafe.Sizeof(C.uintptr_t(0))))
	*(*C.uintptr_t)(s.opaque) = C.uintptr_t(s.handle)
	C.libvlc_audio_set_callbacks(p.mp, C.libvlc_audio_play_cb(C.isolatedPCM), nil, nil, C.libvlc_audio_flush_cb(C.isolatedFlush), nil, s.opaque)
	format := C.CString("S16N")
	C.libvlc_audio_set_format(p.mp, format, 48000, 2)
	C.free(unsafe.Pointer(format))
	em := C.libvlc_media_player_event_manager(p.mp)
	for _, e := range []C.libvlc_event_type_t{C.libvlc_MediaPlayerPlaying, C.libvlc_MediaPlayerPaused, C.libvlc_MediaPlayerStopped, C.libvlc_MediaPlayerEndReached, C.libvlc_MediaPlayerEncounteredError} {
		C.libvlc_event_attach(em, e, C.libvlc_callback_t(C.isolatedEvent), s.opaque)
	}
	return p, s, nil
}
func (s *IsolatedSink) Reset(reference []byte) { s.ResetForSource(reference, nil) }

// Test-only intent binding; actual Native identity is independently observed.
func (s *IsolatedSink) ResetForSource(reference []byte, binding map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resetSerial > 0 && (len(s.pcm) > 0 || s.binding != nil) {
		old := s.snapshotLocked()
		old["sessionId"] = s.binding["sessionId"]
		s.history = append(s.history, old)
		if len(s.history) > 4 {
			s.history = s.history[len(s.history)-4:]
		}
	}
	s.resetSerial++
	s.binding = binding
	s.pcm = nil
	s.lastPCMNS = 0
	s.completedPCMNS = 0
	s.segments = nil
	s.reference = reference
	s.firstAfterFlush = -1
	s.flushes = 0
	s.events = nil
}
func (s *IsolatedSink) SetDiagnosticOrigin(origin time.Time) {
	s.mu.Lock()
	s.diagnosticOrigin = origin
	s.mu.Unlock()
}
func (s *IsolatedSink) Close() { s.handle.Delete(); C.free(s.opaque) }
func (s *IsolatedSink) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.snapshotLocked()
	v["history"] = append([]map[string]any(nil), s.history...)
	return v
}
func (s *IsolatedSink) snapshotLocked() map[string]any {
	origin := float64(-1)
	suffixExact, contiguous := false, false
	for i := len(s.segments) - 1; i >= 0; i-- {
		segment := s.segments[i]
		if len(segment.pcm) == 0 {
			continue
		}
		origin = segment.origin
		if origin >= 0 {
			offset := int(origin*48+0.5) * 4
			if offset <= len(s.reference) {
				suffixExact = bytes.Equal(segment.pcm, s.reference[offset:])
				contiguous = len(segment.pcm) <= len(s.reference)-offset && bytes.Equal(segment.pcm, s.reference[offset:offset+len(segment.pcm)])
			}
		}
		break
	}
	sum := sha256.Sum256(s.pcm)
	return map[string]any{"resetSerial": s.resetSerial, "binding": s.binding, "referenceSHA256": func() string { h := sha256.Sum256(s.reference); return hex.EncodeToString(h[:]) }(), "last_pcm_monotonic_ns": s.lastPCMNS, "completed_pcm_monotonic_ns": s.completedPCMNS, "frames": len(s.pcm) / 4, "sha256": hex.EncodeToString(sum[:]), "full_pcm_exact": len(s.pcm) > 0 && bytes.Equal(s.pcm, s.reference), "first_post_flush_pcm_ms": origin, "post_flush_suffix_exact": suffixExact, "post_flush_contiguous": contiguous, "flushes": s.flushes, "events": append([]map[string]any(nil), s.events...)}
}
func isolatedSink(p unsafe.Pointer) *IsolatedSink {
	return cgo.Handle(*(*C.uintptr_t)(p)).Value().(*IsolatedSink)
}

//export isolatedPCM
func isolatedPCM(o unsafe.Pointer, b unsafe.Pointer, n C.uint, pts C.int64_t) {
	s := isolatedSink(o)
	raw := C.GoBytes(b, C.int(n)*4)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcm = append(s.pcm, raw...)
	if !s.diagnosticOrigin.IsZero() {
		s.lastPCMNS = time.Since(s.diagnosticOrigin).Nanoseconds()
		if len(s.pcm) == len(s.reference) && bytes.Equal(s.pcm, s.reference) {
			s.completedPCMNS = s.lastPCMNS
		}
	}
	if len(s.segments) > 0 {
		seg := &s.segments[len(s.segments)-1]
		if len(seg.pcm) == 0 && len(raw) >= 64 {
			i := bytes.Index(s.reference, raw[:64])
			if i >= 0 && i%4 == 0 {
				seg.origin = float64(i) / 4 / 48
			}
		}
		seg.pcm = append(seg.pcm, raw...)
	}
	if s.firstAfterFlush < 0 && len(raw) >= 64 {
		i := bytes.Index(s.reference, raw[:64])
		if i >= 0 && i%4 == 0 {
			s.firstAfterFlush = float64(i) / 4 / 48
		}
	}
}

//export isolatedFlush
func isolatedFlush(o unsafe.Pointer, pts C.int64_t) {
	s := isolatedSink(o)
	s.mu.Lock()
	s.flushes++
	s.segments = append(s.segments, isolatedSegment{origin: -1})
	s.firstAfterFlush = -1
	s.mu.Unlock()
}

//export isolatedEvent
func isolatedEvent(e *C.libvlc_event_t, o unsafe.Pointer) {
	s := isolatedSink(o)
	s.mu.Lock()
	s.events = append(s.events, map[string]any{"resetSerial": s.resetSerial, "event": C.GoString(C.libvlc_event_type_name(C.libvlc_event_type_t(C.isolatedEventType(e)))), "wall": time.Now().UTC().Format(time.RFC3339Nano)})
	s.mu.Unlock()
}
