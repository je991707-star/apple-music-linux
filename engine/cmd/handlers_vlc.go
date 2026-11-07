package main

import (
	"context"
	"encoding/json"
	"engine/internal/playbackcmd"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"engine/core/vlc"
)

// alacCacheVersion is bumped whenever a bug fix changes which bytes are written
// to the ALAC disk cache. On mismatch, all *-alac.m4a files are deleted so VLC
// never replays a fragment-0 that was written by the old, broken key path.
const alacCacheVersion = "cbcs-fairplay-upstream-v9"

// invalidateALACCacheIfStale deletes every committed ALAC cache file when the
// on-disk version marker does not match alacCacheVersion. This is a one-time
// migration on first startup after a decryption fix.
func invalidateALACCacheIfStale(cacheDir string) {
	verFile := filepath.Join(cacheDir, ".alac_ver")
	if data, err := os.ReadFile(verFile); err == nil && strings.TrimSpace(string(data)) == alacCacheVersion {
		return
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	deleted := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "-alac.m4a") {
			if removeErr := os.Remove(filepath.Join(cacheDir, e.Name())); removeErr == nil {
				deleted++
			}
		}
	}
	if deleted > 0 {
		log.Printf("[vlc] invalidated %d stale ALAC cache file(s) (FairPlay key fix)", deleted)
	}
	_ = os.WriteFile(verFile, []byte(alacCacheVersion), 0o644)
}

func (s *APIServer) handleVLCLoad(w http.ResponseWriter, r *http.Request) {
	if s.vlcPlayer == nil {
		http.Error(w, "libvlc not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		SessionID string                `json:"sessionId"`
		AssetID   string                `json:"assetId"`
		StartMs   int64                 `json:"startMs"`
		Command   *playbackcmd.Identity `json:"command"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, "sessionId required", http.StatusBadRequest)
		return
	}
	ok := true
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" {
		_, ok = s.pm.GetSession(req.SessionID)
	}
	if !ok {
		http.Error(w, "session not found or expired", http.StatusNotFound)
		return
	}
	t, ok := s.admitVLC(w, r, req.Command, "load", req.SessionID)
	if !ok {
		return
	}
	stopCancel := context.AfterFunc(r.Context(), t.Cancel)
	defer stopCancel()
	if err := s.loadVLCCommand(r.Context(), t, req.SessionID, req.StartMs, false); err != nil {
		s.vlcCommandError(w, err, t)
		return
	}
	s.vlcACK(w, t, map[string]any{"ack": "media-committed"})
}

func (s *APIServer) handleVLCPause(w http.ResponseWriter, r *http.Request) {
	s.handleVLCTransport(w, r, "pause")
}

func (s *APIServer) handleVLCResume(w http.ResponseWriter, r *http.Request) {
	s.handleVLCTransport(w, r, "resume")
}

func (s *APIServer) handleVLCTime(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		snap := s.vlcGate.order.Snapshot()
		writeJSON(w, 200, map[string]any{"state": "unowned", "control": map[string]any{"boot": snap.Boot, "epoch": snap.Epoch, "versioned": snap.Versioned}})
		return
	}

	if s.vlcPlayer == nil {
		http.Error(w, "libvlc not available", http.StatusServiceUnavailable)
		return
	}
	posMs, lengthMs, state := s.vlcPlayer.Time()
	volume, muted := s.vlcPlayer.Volume()
	writeJSON(w, http.StatusOK, map[string]any{
		"posMs":    posMs,
		"lengthMs": lengthMs,
		"state":    state,
		"volume":   volume, // live mixer state, from PR #1 (https://github.com/silentone12725/apple-music-linux/pull/1)
		"muted":    muted,
		// eof: the source ended cleanly, so an "ended" state is the real end of the data, not a failed read.
		"eof":     s.vlcPlayer.SourceEnded(),
		"control": s.vlcGate.order.Snapshot(),
	})
}

func (s *APIServer) handleVLCSeek(w http.ResponseWriter, r *http.Request) {
	if s.vlcPlayer == nil {
		http.Error(w, "libvlc not available", 503)
		return
	}
	var req struct {
		PosMs     int64                 `json:"posMs"`
		SessionID string                `json:"sessionId"`
		Command   *playbackcmd.Identity `json:"command"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.PosMs < 0 {
		http.Error(w, "invalid seek", 400)
		return
	}
	t, ok := s.admitVLC(w, r, req.Command, "seek", req.SessionID)
	if !ok {
		return
	}
	stopCancel := context.AfterFunc(r.Context(), t.Cancel)
	defer stopCancel()
	err := s.vlcGate.order.Commit(r.Context(), t, func() error {
		if req.Command == nil {
			return s.vlcPlayer.QueueTimeCommand(req.PosMs, t)
		}
		return s.vlcPlayer.SetTimeCommand(r.Context(), req.PosMs, t)
	})
	reloaded := req.Command != nil && errors.Is(err, vlc.ErrTerminalSeek)
	if reloaded {
		err = s.loadVLCCommand(r.Context(), t, req.SessionID, req.PosMs, true)
	}
	if err != nil {
		t.Cancel()
		s.vlcCommandError(w, err, t)
		return
	}
	stage := "seek-dispatched"
	if req.Command == nil {
		stage = "seek-queued"
	}
	s.vlcACK(w, t, map[string]any{"actualStartMs": req.PosMs, "ack": stage, "reloaded": reloaded})
}

func (s *APIServer) handleVLCStop(w http.ResponseWriter, r *http.Request) {
	s.handleVLCTransport(w, r, "stop")
}

func (s *APIServer) handleVLCVolume(w http.ResponseWriter, r *http.Request) {
	if s.vlcPlayer == nil {
		http.Error(w, "libvlc not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Volume int `json:"volume"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.vlcPlayer.SetVolume(req.Volume)
	w.WriteHeader(http.StatusOK)
}

// openVLCSource returns a libvlc callback source for the session's audio:
// the committed cache file, a reader on the in-progress download, or a
// freshly started download. Returns (nil, nil) when no cache reader can be
// had, in which case the caller falls back to the loopback HTTP endpoint.
func (s *APIServer) openVLCSource(id, assetID, codec string) (vlc.Source, error) {
	if s.diskCache == nil {
		return nil, nil
	}
	// A download can commit between the checks below; retry once to pick up
	// the committed file.
	for range 2 {
		if f, ok := s.diskCache.Get(assetID, codec); ok {
			fi, err := f.Stat()
			if err != nil {
				f.Close()
				return nil, err
			}
			return &fileSource{File: f, size: fi.Size()}, nil
		}
		if spw := s.diskCache.GetStreaming(assetID, codec); spw != nil {
			if r := spw.NewReaderIfActive(); r != nil {
				return r, nil
			}
			continue
		}
		spw, err := s.diskCache.BeginStreamingPut(assetID, codec)
		if err != nil {
			return nil, err
		}
		if spw != nil {
			r := spw.NewReader()
			s.startCacheDownload(id, spw)
			return r, nil
		}
	}
	// A non-streaming precache holds the key; the HTTP endpoint streams uncached.
	return nil, nil
}

// fileSource adapts a committed cache file to vlc.Source. Reads never block,
// so Abort is a no-op.
type fileSource struct {
	*os.File
	size int64
}

func (f *fileSource) Size() int64 { return f.size }
func (f *fileSource) Abort()      {}
func (f *fileSource) Close()      { f.File.Close() }

var _ vlc.Source = (*fileSource)(nil)

func (s *APIServer) vlcACK(w http.ResponseWriter, t *playbackcmd.Ticket, extra map[string]any) {
	if extra == nil {
		extra = map[string]any{}
	}
	if _, ok := extra["ack"]; !ok {
		extra["ack"] = "native-committed"
	}
	extra["command"] = t.Identity
	if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		s.mainSources.mu.Lock()
		if g := s.mainSources.grants[t.Identity.Session]; g != nil && g.events != nil && g.lease.Active() {
			g.events.push(g.ownIdentity(), "command-"+t.Identity.Type, nil)
		}
		s.mainSources.mu.Unlock()
	}

	writeJSON(w, http.StatusOK, extra)
}

func (s *APIServer) vlcCommandError(w http.ResponseWriter, err error, tickets ...*playbackcmd.Ticket) {
	status := http.StatusInternalServerError
	if errors.Is(err, playbackcmd.ErrStale) || errors.Is(err, playbackcmd.ErrLegacy) {
		status = http.StatusConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusRequestTimeout
	}
	if len(tickets) > 0 && tickets[0] != nil {
		t := tickets[0]
		writeJSON(w, status, map[string]any{"command": t.Identity, "error": "playback command rejected", "mayHaveApplied": t.Entered(), "nativeCommitEntered": t.NativeEntered()})
		return
	}
	http.Error(w, err.Error(), status)
}

func (s *APIServer) handleVLCTransport(w http.ResponseWriter, r *http.Request, kind string) {
	if s.vlcPlayer == nil {
		http.Error(w, "libvlc not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		SessionID string                `json:"sessionId"`
		Command   *playbackcmd.Identity `json:"command"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
	}
	t, ok := s.admitVLC(w, r, req.Command, kind, req.SessionID)
	if !ok {
		return
	}
	stopCancel := context.AfterFunc(r.Context(), t.Cancel)
	defer stopCancel()
	err := s.vlcGate.order.Commit(r.Context(), t, func() error {

		if err := s.vlcPlayer.TransportCommand(kind, t); err != nil {
			return err
		}
		if kind == "stop" {
			s.vlcGate.order.Stopped(t)
		}
		return nil
	})
	if err != nil {
		s.vlcCommandError(w, err, t)
		return
	}
	if kind == "stop" && os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		if l := t.SourceLease(); l != nil {
			l.Retire()
			s.pm.Release(req.SessionID)
		}
	}
	s.vlcACK(w, t, nil)
}

// Prepared ownership ends at transfer to Player, including native error paths.
type ownedVLCSource struct {
	vlc.Source
	release      func()
	abort, close sync.Once
}

func (s *ownedVLCSource) Abort() { s.abort.Do(s.Source.Abort) }
func (s *ownedVLCSource) Close() { s.close.Do(func() { s.Source.Close(); s.release() }) }

func (s *APIServer) loadVLCCommand(ctx context.Context, t *playbackcmd.Ticket, id string, startMs int64, terminal bool) error {
	if !t.Valid() {
		return playbackcmd.ErrStale
	}
	sess, ok := s.pm.GetSession(id)
	if !ok {
		return playbackcmd.ErrStale
	}
	var resourceDone func()
	if l := t.SourceLease(); l != nil {
		var allowed bool
		resourceDone, allowed = l.Resource()
		if !allowed {
			return playbackcmd.ErrStale
		}
	}
	resourceTransferred := false
	defer func() {
		if resourceDone != nil && !resourceTransferred {
			resourceDone()
		}
	}()
	cacheKey := sess.AssetID
	if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		cacheKey = "main-private-" + sess.ID
	}
	hold := true
	if s.diskCache != nil {
		if _, cached := s.diskCache.Path(cacheKey, sess.Codec); cached {
			hold = false
		} else if spw := s.diskCache.GetStreaming(cacheKey, sess.Codec); spw != nil && spw.Written() >= alacStartBytes {
			hold = false
		}
	}
	var release func()
	if err := s.vlcGate.order.Current(t, func() { release = s.alac.foregroundLease(id, hold) }); err != nil {
		return err
	}
	stopRelease := context.AfterFunc(t.Context(), release)
	defer stopRelease()
	src, err := s.openVLCSource(id, cacheKey, sess.Codec)
	if err != nil {
		release()
		return err
	}
	if src != nil {
		src = &ownedVLCSource{Source: src, release: func() {
			release()
			if resourceDone != nil {
				resourceDone()
			}
		}}
		resourceTransferred = true
	} else {
		release()
	}
	// The qualified Main path has no authorized HTTP transport. A cache/reader
	// failure must not silently fall through to the Legacy API URL or emit a
	// misleading successful Native Load ACK. Legacy behavior remains unchanged.
	if src == nil && os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		return errors.New("authorized callback source unavailable")
	}

	src = observeRetirementSource(s, src)
	transferred := false
	defer func() {
		if src != nil && !transferred {
			src.Abort()
			src.Close()
		}
	}()
	return s.vlcGate.order.Commit(ctx, t, func() error {
		// Reject a Session that expired during preparation. This existence check
		// is not an atomic Source Authority lease (reserved for U2-2).
		if _, ok := s.pm.GetSession(id); !ok {
			return playbackcmd.ErrStale
		}
		if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
			prepareMainSourceDiagnostics(s, id)
		}
		var err error
		if src != nil {
			transferred = true
			if terminal {
				err = s.vlcPlayer.ReloadSourceCommand(ctx, src, t, startMs)
			} else {
				err = s.vlcPlayer.LoadSourceCommand(src, t, startMs)
			}
		} else {
			// U2-1 does not authorize HTTP audio or terminal recovery without a reader.
			if terminal {
				return errors.New("terminal seek requires callback source")
			}
			url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/playback/%s/audio", s.port, id)
			err = s.vlcPlayer.LoadCommand(url, t, startMs)
		}
		if err == nil {
			s.vlcGate.order.Loaded(t)
		}
		return err
	})
}
