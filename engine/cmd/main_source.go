package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"engine/internal/playbackcmd"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
)

// All proofs remain Main-only. The transport token is deliberately insufficient.
// Lock order: registry -> Manager -> Lease -> command Gate. Provider and Native
// preparation never run under registry.mu. Grant state is derived from the SAME
// Lease that fences native entry, including expiry/sweep retirement.
type mainSourceGrant struct {
	Principal                  string   `json:"principal"`
	MainGeneration             string   `json:"mainGeneration"`
	DocumentGeneration         uint64   `json:"documentGeneration"`
	Boot                       string   `json:"boot"`
	Epoch                      string   `json:"epoch"`
	Session                    string   `json:"sessionId"`
	Asset                      string   `json:"assetId"`
	SourceGeneration           uint64   `json:"sourceGeneration"`
	LeaseID                    string   `json:"leaseId"`
	CommandGeneration          uint64   `json:"commandGeneration"`
	OwnershipRevision          uint64   `json:"ownershipRevision"`
	Rights                     []string `json:"rights"`
	Secret                     string   `json:"secret,omitempty"` // response to pinned Main only; never stored in registry
	lease                      *playbackcmd.Lease
	proof                      [32]byte
	sequence                   uint64
	complete, failed, finished bool
	aborted                    bool
	codec                      string
	duration                   int
	events                     *ownEventLog
	volumeClock                *atomic.Uint64
}

var sourceUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

var errMainSourceGone = errors.New("owned source retired")

type mainSourceRegistry struct {
	mu        sync.Mutex
	grants    map[string]*mainSourceGrant
	revisions map[string]uint64
}
type mainSourceRequest struct {
	Operation          string `json:"operation"`
	Principal          string `json:"principal"`
	MainGeneration     string `json:"mainGeneration"`
	DocumentGeneration uint64 `json:"documentGeneration"`
	Boot               string `json:"boot"`
	OwnershipRevision  uint64 `json:"ownershipRevision"`
	Intent             string `json:"intent"`
	Session            string `json:"sessionId"`
	Page               int    `json:"page"`
	Limit              int    `json:"limit"`
	Volume             *int   `json:"value"`
	Revision           uint64 `json:"revision"`
}

func sourceNonce() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("system randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}
func sameProof(a, b string) bool {
	return len(a) >= 32 && len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (g *mainSourceGrant) ownerKey() string { return g.Principal + ":" + g.MainGeneration }
func (s *APIServer) sourceGrantLocked(r *http.Request, session, right string) (*mainSourceGrant, error) {
	g := s.mainSources.grants[session]
	if g == nil {
		return nil, playbackcmd.ErrStale
	}
	hash := sha256.Sum256([]byte(r.Header.Get("X-Main-Source")))
	if subtle.ConstantTimeCompare(hash[:], g.proof[:]) != 1 {
		return nil, errors.New("source proof required")
	}
	if g.Boot != s.vlcGate.order.Snapshot().Boot || s.mainSources.revisions[g.ownerKey()] != g.OwnershipRevision {
		return nil, playbackcmd.ErrStale
	}
	if !g.lease.Active() && right != "release" {
		return nil, errMainSourceGone
	}
	for _, v := range g.Rights {
		if v == right {
			return g, nil
		}
	}
	return nil, errors.New("source operation denied")
}
func (s *APIServer) handleMainSource(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" || !orderedVLCAuth(r) {
		http.Error(w, "private Main required", 403)
		return
	}
	var q mainSourceRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid typed source request", 400)
		return
	}
	if q.Operation == "create" || q.Operation == "reopen" {
		s.createMainSource(w, r, q)
		return
	}
	if q.Operation == "metadata" || q.Operation == "volume" || q.Operation == "library" || q.Operation == "artwork" {
		s.handleScopedSource(w, r, q)
		return
	}

	if q.Operation != "read" && q.Operation != "touch" && q.Operation != "release" {
		http.Error(w, "unknown source operation", 400)
		return
	}
	if q.Volume != nil || q.Revision != 0 || q.Page != 0 || q.Limit != 0 || q.Principal != "" || q.MainGeneration != "" || q.DocumentGeneration != 0 || q.Boot != "" || q.OwnershipRevision != 0 || q.Intent != "" {
		http.Error(w, "unexpected source fields", 400)
		return
	}

	s.mainSources.mu.Lock()
	g, e := s.sourceGrantLocked(r, q.Session, q.Operation)
	if e != nil {
		s.mainSources.mu.Unlock()
		status := 403
		if errors.Is(e, errMainSourceGone) && (q.Operation == "read" || q.Operation == "touch") {
			status = 404
		}
		http.Error(w, "source authority rejected", status)
		return
	}
	switch q.Operation {
	case "touch":
		e = s.pm.TouchOwnedSource(g.Session, g.Boot, g.lease)
	case "release":
		g.lease.Retire() // synchronous authority + ticket invalidation before ACK
	}
	complete, failed, finished, aborted := g.complete, g.failed, g.finished, g.aborted
	s.mainSources.mu.Unlock()
	if e != nil {
		http.Error(w, "source retired", 409)
		return
	}
	if q.Operation == "release" {
		s.pm.Release(g.Session)
		writeJSON(w, 200, map[string]any{"stage": "authority-revoked", "nativeRollback": false})
		return
	}
	result := map[string]any{"stage": "engine-accepted", "sessionId": g.Session, "sourceGeneration": g.SourceGeneration, "state": "granted", "sourceComplete": nil, "sourceFailed": nil}
	if finished {
		result["sourceComplete"] = complete
		result["sourceFailed"] = failed
		result["sourceAborted"] = aborted
	}
	if q.Operation == "read" {
		// Reading another session's live Player state is not implied by owning a grant.
		if s.vlcPlayer == nil {
			http.Error(w, "native unavailable", 503)
			return
		}
		if !s.vlcGate.order.Owns(g.Session) {
			snap := s.vlcGate.order.Snapshot()
			writeJSON(w, 200, map[string]any{"state": "nothing", "posMs": 0, "lengthMs": 0, "eof": nil, "control": map[string]any{"boot": snap.Boot, "epoch": snap.Epoch, "versioned": snap.Versioned}})
			return
		}
		pos, length, state := s.vlcPlayer.Time()
		result["posMs"] = pos
		result["lengthMs"] = length
		result["state"] = state
		result["eof"] = s.vlcPlayer.SourceEnded()
		result["control"] = s.vlcGate.order.Snapshot()
		if !g.lease.Active() {
			http.Error(w, "source retired during read", 409)
			return
		}
		if v := mainSourceDiagnostics(s); v != nil {
			result["pcm"] = v
		}
	}
	writeJSON(w, 200, result)
}
func (s *APIServer) createMainSource(w http.ResponseWriter, r *http.Request, q mainSourceRequest) {
	// Issuance is a separate authority from transport and from a retired grant.
	if !sameProof(r.Header.Get("X-Main-Issuer"), os.Getenv("ADAM_MAIN_SOURCE_ISSUER")) {
		http.Error(w, "Main issuer required", 403)
		return
	}
	if mainSourceFactoryKind() == "unavailable" {
		http.Error(w, "qualified factory unavailable", 403)
		return
	}
	if q.Volume != nil || q.Revision != 0 || q.Page != 0 || q.Limit != 0 || !sourceUUID.MatchString(q.Principal) || !sourceUUID.MatchString(q.MainGeneration) || q.DocumentGeneration == 0 || q.DocumentGeneration > 1<<53 || q.OwnershipRevision == 0 || q.OwnershipRevision > 1<<53 || q.Boot != s.vlcGate.order.Snapshot().Boot || q.Session != "" || len(q.Intent) > 64 {
		http.Error(w, "invalid source identity", 409)
		return
	}
	key := q.Principal + ":" + q.MainGeneration
	s.mainSources.mu.Lock()
	if s.mainSources.grants == nil {
		s.mainSources.grants = map[string]*mainSourceGrant{}
		s.mainSources.revisions = map[string]uint64{}
	}
	if q.OwnershipRevision <= s.mainSources.revisions[key] {
		s.mainSources.mu.Unlock()
		http.Error(w, "stale source intent", 409)
		return
	}
	// Reserve the revision BEFORE blocking preparation. It cannot be replayed after failure.
	s.mainSources.revisions[key] = q.OwnershipRevision
	var old []*mainSourceGrant
	for _, g := range s.mainSources.grants {
		if g.ownerKey() == key {
			if g.lease.Active() {
				g.lease.Retire()
				old = append(old, g)
			}
			delete(s.mainSources.grants, g.Session)
		}
	}
	s.mainSources.mu.Unlock()
	for _, g := range old {
		s.pm.Release(g.Session)
	}
	sess, e := openMainTypedSource(r.Context(), s.pm, q.Intent)
	if e != nil {
		http.Error(w, "typed source unavailable", 422)
		return
	}
	l, e := s.pm.ReserveSource(sess.ID, q.Boot)
	if e != nil {
		s.pm.Release(sess.ID)
		disposeMainSourceDiagnostics(s, sess.ID)
		http.Error(w, "source retired", 409)
		return
	}
	secret := sourceNonce()
	g := &mainSourceGrant{Principal: q.Principal, MainGeneration: q.MainGeneration, DocumentGeneration: q.DocumentGeneration, Boot: q.Boot, Session: sess.ID, Asset: sess.AssetID, SourceGeneration: l.Identity.Generation, LeaseID: l.Identity.ID, OwnershipRevision: q.OwnershipRevision, Rights: []string{"control", "load", "seek", "pause", "resume", "stop", "read", "intent", "touch", "release", "metadata", "events", "volume", "library", "artwork"}, lease: l, proof: sha256.Sum256([]byte(secret))}
	s.mainSources.mu.Lock()
	if s.mainSources.revisions[key] != q.OwnershipRevision || r.Context().Err() != nil {
		s.mainSources.mu.Unlock()
		s.pm.Release(sess.ID)
		disposeMainSourceDiagnostics(s, sess.ID)
		http.Error(w, "stale preparation", 409)
		return
	}
	g.codec = sess.Codec
	g.duration = sess.DurationMs
	g.events = &ownEventLog{}
	g.volumeClock = &atomic.Uint64{}
	s.mainSources.grants[sess.ID] = g
	public := *g
	public.Secret = secret
	s.mainSources.mu.Unlock()
	// Observe even unloaded grants so their eventual retirement can drain safely.
	s.watchLease(l)
	writeJSON(w, 201, map[string]any{"grant": public, "session": sess, "stage": "engine-accepted"})
}

// The new private source's producer owns a separate Lease resource. Retirement
// cancels its context; cleanup is not complete until the producer actually exits.
func (s *APIServer) bindMainSourceDownload(ctx context.Context, cancel context.CancelFunc, id string) func() {
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" {
		return func() {}
	}
	s.mainSources.mu.Lock()
	g := s.mainSources.grants[id]
	if g == nil {
		s.mainSources.mu.Unlock()
		cancel()
		return func() {}
	}
	l := g.lease
	release, ok := l.Resource()
	s.mainSources.mu.Unlock()
	if !ok {
		cancel()
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-l.Done():
			cancel()
		case <-ctx.Done():
		case <-done:
		}
	}()
	return func() { once.Do(func() { close(done); release() }) }
}

// Facts describe the actual Pipeline outcome, never an inference from VLC Ended.
func (s *APIServer) recordMainSourceOutcome(id string, err error) {
	s.mainSources.mu.Lock()
	defer s.mainSources.mu.Unlock()
	if g := s.mainSources.grants[id]; g != nil {
		g.complete = err == nil
		g.aborted = errors.Is(err, context.Canceled)
		g.failed = err != nil && !g.aborted
		g.finished = true
	}
}
func (s *APIServer) admitMainVLC(r *http.Request, id *playbackcmd.Identity, kind, session string) (*playbackcmd.Ticket, error) {
	if !orderedVLCAuth(r) || id == nil {
		return nil, errors.New("versioned Main source command required")
	}
	s.mainSources.mu.Lock()
	defer s.mainSources.mu.Unlock()
	g, e := s.sourceGrantLocked(r, session, kind)
	if e != nil {
		return nil, e
	}
	if id.Session != g.Session || id.Boot != g.Boot || id.Epoch != g.Epoch || id.Sequence <= g.sequence {
		return nil, playbackcmd.ErrStale
	}
	t, e := s.pm.AdmitOwnedSource(session, g.Boot, g.lease, func() (*playbackcmd.Ticket, error) { return s.vlcGate.order.Admit(id, kind, session) })
	if e == nil {
		g.sequence = id.Sequence
		g.CommandGeneration = id.Load
	}
	return t, e
}

// No preparation or Native calls occur while holding the registry lock.
func (s *APIServer) claimMainVLC(r *http.Request, boot, epoch, session string) (playbackcmd.Snapshot, error) {
	if !orderedVLCAuth(r) {
		return playbackcmd.Snapshot{}, errors.New("Main transport required")
	}
	s.mainSources.mu.Lock()
	defer s.mainSources.mu.Unlock()
	g, e := s.sourceGrantLocked(r, session, "control")
	if e != nil {
		return playbackcmd.Snapshot{}, e
	}
	if g.Epoch != "" {
		return playbackcmd.Snapshot{}, playbackcmd.ErrStale
	}
	var snap playbackcmd.Snapshot
	e = s.pm.OwnedSourceOperation(session, g.Boot, g.lease, func() error {
		var err error
		snap, err = s.vlcGate.order.ClaimSource(boot, epoch, session, g.lease)
		return err
	})
	if e == nil {
		g.Epoch = snap.Epoch
		g.sequence = 0
	}
	return snap, e
}
