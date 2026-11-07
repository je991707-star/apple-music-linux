package main

import (
	"context"
	"encoding/json"
	"engine/core/vlc"
	"engine/internal/playbackcmd"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// OwnMetadata is deliberately not Session, global engine.snapshot, or a map of
// arbitrary Engine output. Unknown source/sample/reader facts remain null.
type ownIdentity struct {
	Boot       string `json:"boot"`
	Epoch      string `json:"epoch"`
	Session    string `json:"sessionId"`
	Asset      string `json:"assetId"`
	Generation uint64 `json:"sourceGeneration"`
	Lease      string `json:"leaseId"`
	Owner      uint64 `json:"ownershipRevision"`
	Document   uint64 `json:"documentGeneration"`
	Load       uint64 `json:"loadGeneration"`
	Sequence   uint64 `json:"commandSequence"`
}
type ownMetadata struct {
	Identity         ownIdentity `json:"identity"`
	Codec            string      `json:"codec"`
	DeclaredDuration int         `json:"declaredDurationMs"`
	SampleDuration   *int64      `json:"sampleDurationMs"`
	Complete         *bool       `json:"sourceComplete"`
	Failed           *bool       `json:"sourceFailed"`
	Aborted          *bool       `json:"sourceAborted"`
	ReaderEOF        *bool       `json:"readerEof"`
	ReaderError      *string     `json:"readerError"`
	State            string      `json:"state"`
	Position         *int64      `json:"positionMs"`
	Length           *int64      `json:"vlcLengthMs"`
}

func (g *mainSourceGrant) ownIdentity() ownIdentity {
	return ownIdentity{g.Boot, g.Epoch, g.Session, g.Asset, g.SourceGeneration, g.LeaseID, g.OwnershipRevision, g.DocumentGeneration, g.CommandGeneration, g.sequence}
}
func (s *APIServer) ownMetadataSnapshot(g *mainSourceGrant) (ownMetadata, error) {
	s.mainSources.mu.Lock()
	if !g.lease.Active() || s.mainSources.grants[g.Session] != g {
		s.mainSources.mu.Unlock()
		return ownMetadata{}, playbackcmd.ErrStale
	}
	v := ownMetadata{Identity: g.ownIdentity(), Codec: g.codec, DeclaredDuration: g.duration, State: "unloaded"}
	if g.finished {
		c, f, a := g.complete, g.failed, g.aborted
		v.Complete = &c
		v.Failed = &f
		v.Aborted = &a
	}
	s.mainSources.mu.Unlock()
	if s.vlcPlayer != nil && s.vlcGate.order.Owns(g.Session) {
		pos, length, state, eof := s.vlcPlayer.OwnStatusForLease(g.lease)
		v.State = state
		if pos >= 0 {
			v.Position = &pos
		}
		if length > 0 {
			v.Length = &length
		}
		v.ReaderEOF = eof
	}
	if !g.lease.Active() {
		return ownMetadata{}, playbackcmd.ErrStale
	}
	return v, nil
}
func (s *APIServer) handleScopedSource(w http.ResponseWriter, r *http.Request, q mainSourceRequest) {
	if q.Principal != "" || q.MainGeneration != "" || q.DocumentGeneration != 0 || q.Boot != "" || q.OwnershipRevision != 0 || q.Intent != "" {
		http.Error(w, "unexpected identity", 400)
		return
	}
	right := q.Operation
	s.mainSources.mu.Lock()
	g, e := s.sourceGrantLocked(r, q.Session, right)
	if e != nil {
		s.mainSources.mu.Unlock()
		http.Error(w, "source authority rejected", 403)
		return
	}
	if q.Operation == "volume" {
		if q.Volume == nil || *q.Volume < 0 || *q.Volume > 200 || q.Revision == 0 || q.Revision > 1<<53 || q.Page != 0 || q.Limit != 0 || q.Revision <= g.volumeClock.Load() {
			s.mainSources.mu.Unlock()
			http.Error(w, "invalid or stale volume", 409)
			return
		}
		g.volumeClock.Store(q.Revision)
		clock := g.volumeClock
		revision := q.Revision
		value := *q.Volume
		identity := g.ownIdentity()
		s.mainSources.mu.Unlock()
		if s.vlcPlayer == nil {
			http.Error(w, "native unavailable", 503)
			return
		}
		if e = s.vlcPlayer.SetVolumeForLease(value, g.lease, func() bool { return clock.Load() == revision }); e != nil {
			if e == playbackcmd.ErrStale {
				http.Error(w, "stale volume", 409)
			} else if errors.Is(e, vlc.ErrVolumeOutputUnsupported) {
				writeJSON(w, 503, map[string]any{"stage": "output-unsupported", "backend": s.vlcPlayer.OutputBackend(), "mayHaveApplied": false})
			} else {
				writeJSON(w, 503, map[string]any{"stage": "native-volume-unconfirmed", "backend": s.vlcPlayer.OutputBackend(), "mayHaveApplied": true})
			}
			return
		}
		nativeValue, _ := s.vlcPlayer.Volume()
		if clock.Load() != revision || !g.lease.Active() {
			http.Error(w, "volume superseded", 409)
			return
		}
		g.events.push(identity, "volume", nil)
		writeJSON(w, 200, map[string]any{"stage": "app-volume-returned", "revision": revision, "value": value, "nativeValue": nativeValue, "backend": s.vlcPlayer.OutputBackend(), "verification": map[string]bool{"nativeSetterAccepted": true, "nativeGetterConfirmed": true, "backendIndependentlyConfirmed": false, "outputSampleGainVerified": false, "physicalDeviceAudibleVerified": false}})
		return
	}
	s.mainSources.mu.Unlock()
	if q.Volume != nil || q.Revision != 0 {
		http.Error(w, "unexpected mutation", 400)
		return
	}
	switch q.Operation {
	case "metadata":
		if q.Page != 0 || q.Limit != 0 {
			http.Error(w, "unexpected metadata fields", 400)
			return
		}
		v, e := s.ownMetadataSnapshot(g)
		if e != nil {
			http.Error(w, "source retired", 409)
			return
		}
		writeJSON(w, 200, v)
	case "library":
		if q.Page < 0 || q.Page > 10 || q.Limit < 1 || q.Limit > 2 {
			http.Error(w, "invalid page", 400)
			return
		}
		v, ok := mainScopedLibrary(q.Page, q.Limit)
		if !ok {
			http.Error(w, "account library unavailable", 403)
			return
		}
		if !g.lease.Active() {
			http.Error(w, "source retired", 409)
			return
		}
		writeJSON(w, 200, v)
	case "artwork":
		if q.Page != 0 || q.Limit != 0 {
			http.Error(w, "unexpected artwork fields", 400)
			return
		}
		v, ok := mainScopedArtwork()
		if !ok {
			http.Error(w, "authorized artwork unavailable", 403)
			return
		}
		if !g.lease.Active() {
			http.Error(w, "source retired", 409)
			return
		}
		writeJSON(w, 200, v)
	}
}

type ownEvent struct {
	Seq      uint64       `json:"seq"`
	Kind     string       `json:"kind"`
	Identity ownIdentity  `json:"identity"`
	Metadata *ownMetadata `json:"metadata"`
}
type ownEventLog struct {
	mu          sync.Mutex
	seq         uint64
	items       []ownEvent
	subscribers int
}

// Ring storage never grows with playback duration or a stalled subscriber.
func (l *ownEventLog) push(identity ownIdentity, kind string, v *ownMetadata) uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	if v != nil {
		identity = v.Identity
	}
	l.items = append(l.items, ownEvent{l.seq, kind, identity, v})
	if len(l.items) > 16 {
		l.items = l.items[len(l.items)-16:]
	}
	return l.seq
}
func (l *ownEventLog) since(cursor uint64) ([]ownEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.items) > 0 && cursor > 0 && cursor+1 < l.items[0].Seq {
		return nil, false
	}
	out := []ownEvent{}
	for _, v := range l.items {
		if v.Seq > cursor {
			out = append(out, v)
		}
	}
	return out, true
}
func (l *ownEventLog) open() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.subscribers >= 2 {
		return false
	}
	l.subscribers++
	return true
}
func (l *ownEventLog) close() { l.mu.Lock(); l.subscribers--; l.mu.Unlock() }
func (s *APIServer) handleOwnSourceEvents(w http.ResponseWriter, r *http.Request) {
	if !orderedVLCAuth(r) {
		http.Error(w, "private source required", 403)
		return
	}
	var q struct {
		Session string `json:"sessionId"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid subscription", 400)
		return
	}
	s.mainSources.mu.Lock()
	g, e := s.sourceGrantLocked(r, q.Session, "events")
	s.mainSources.mu.Unlock()
	if e != nil {
		http.Error(w, "source authority rejected", 403)
		return
	}
	if !g.events.open() {
		http.Error(w, "subscription limit", 429)
		return
	}
	defer g.events.close()
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	rc := http.NewResponseController(w)
	send := func(v ownEvent) bool {
		b, err := json.Marshal(v)
		if err != nil || len(b) > 8192 {
			return false
		}
		if rc.SetWriteDeadline(time.Now().Add(time.Second)) != nil {
			return false
		}
		if _, err = io.WriteString(w, "event: own\ndata: "+string(b)+"\n\n"); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	var cursor uint64
	var previous ownMetadata
	first := true
	lastStatus := time.Time{}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-g.lease.Done():
			return
		default:
		}
		v, err := s.ownMetadataSnapshot(g)
		if err != nil {
			return
		}
		if first {
			cursor = g.events.push(v.Identity, "snapshot", &v) - 1
		} else {
			if v.State != previous.State {
				kind := "playback-state"
				if v.State == "ended" {
					kind = "native-terminal"
				}
				g.events.push(v.Identity, kind, &v)
			}
			if !equalBool(v.Complete, previous.Complete) || !equalBool(v.Failed, previous.Failed) || !equalBool(v.Aborted, previous.Aborted) {
				g.events.push(v.Identity, "source-lifecycle", &v)
			}
		}
		if time.Since(lastStatus) >= time.Second {
			g.events.push(v.Identity, "playback-status", &v)
			lastStatus = time.Now()
		}
		previous = v
		first = false
		batch, ok := g.events.since(cursor)
		if !ok {
			return
		}
		for _, event := range batch {
			if !g.lease.Active() || !send(event) {
				return
			}
			cursor = event.Seq
		}
		select {
		case <-ctx.Done():
			return
		case <-g.lease.Done():
			return
		case <-tick.C:
		}
	}
}
func equalBool(a, b *bool) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
