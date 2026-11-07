package main

import (
	"crypto/subtle"
	"encoding/json"
	"engine/internal/playbackcmd"
	"net/http"
	"os"
)

// Ordered control is opt-in and requires the existing Engine API credential.
// This is an Engine-wide private-client authority, NOT per-source authorization.
// Session capabilities / Main Source Authorization remain a U2-2 prerequisite.
func orderedVLCAuth(r *http.Request) bool {
	token := os.Getenv("ADAM_API_TOKEN")
	return os.Getenv("ADAM_VLC_ORDERED") == "1" && token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Api-Token")), []byte(token)) == 1
}

func (s *APIServer) admitVLC(w http.ResponseWriter, r *http.Request, id *playbackcmd.Identity, kind, session string) (*playbackcmd.Ticket, bool) {
	// Normal Main source factory is not yet qualified. A global API credential
	// must never grant permission to adopt an arbitrary existing Session.
	if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		if id == nil || r.Header.Get("X-Main-Source") == "" {
			http.Error(w, "Main source proof required", 403)
			return nil, false
		}
		t, e := s.admitMainVLC(r, id, kind, session)
		if e != nil {
			if e == errMainSourceGone {
				http.Error(w, "owned source retired", 404)
				return nil, false
			}
			s.vlcCommandError(w, e)
			return nil, false
		}
		s.watchSourceLease(t)
		return t, true
	}
	if err := r.Context().Err(); err != nil {
		s.vlcCommandError(w, err)
		return nil, false
	}
	if id != nil {
		if !orderedVLCAuth(r) {
			http.Error(w, "ordered control requires authenticated opt-in", http.StatusUnauthorized)
			return nil, false
		}
		// Check Boot before touching a Session from an obsolete Engine instance.
		if id.Boot != s.vlcGate.order.Snapshot().Boot {
			s.vlcCommandError(w, playbackcmd.ErrStale)
			return nil, false
		}
		if session == "" {
			http.Error(w, "sessionId required", 400)
			return nil, false
		}
		if _, ok := s.pm.GetSession(session); !ok {
			http.Error(w, "session not found", 404)
			return nil, false
		}
	}
	if id == nil && (kind == "pause" || kind == "resume" || kind == "stop") {
		session = s.vlcGate.order.Snapshot().Session
	}
	var t *playbackcmd.Ticket
	var err error
	if session == "" && id == nil {
		t, err = s.vlcGate.order.Admit(id, kind, session)
	} else {
		t, err = s.pm.AdmitSource(session, s.vlcGate.order.Snapshot().Boot, func() (*playbackcmd.Ticket, error) { return s.vlcGate.order.Admit(id, kind, session) })
	}
	if err != nil {
		s.vlcCommandError(w, err)
		return nil, false
	}
	s.watchSourceLease(t)
	return t, true
}

func (s *APIServer) handleVLCControl(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" && (r.Method != http.MethodPost || r.Header.Get("X-Main-Source") == "") {
		http.Error(w, "source claim required", 403)
		return
	}
	if !orderedVLCAuth(r) {
		http.Error(w, "ordered control requires authenticated opt-in", 401)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, s.vlcGate.order.Snapshot())
		return
	}
	var req struct {
		Boot      string `json:"boot"`
		Epoch     string `json:"epoch"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, "invalid control claim", 400)
		return
	}
	exists := true
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" {
		_, exists = s.pm.GetSession(req.SessionID)
	}
	if !exists {
		http.Error(w, "session not found", 404)
		return
	}
	if err := r.Context().Err(); err != nil {
		s.vlcCommandError(w, err)
		return
	}
	var snap playbackcmd.Snapshot
	var err error
	if os.Getenv("ADAM_MAIN_PLAYBACK") == "1" {
		snap, err = s.claimMainVLC(r, req.Boot, req.Epoch, req.SessionID)
	} else {
		snap, err = s.vlcGate.order.Claim(req.Boot, req.Epoch, req.SessionID)
	}
	if err != nil {
		s.vlcCommandError(w, err)
		return
	}
	writeJSON(w, 200, snap)
}

// A verified intent can invalidate preparation before a debounced Seek is sent.
// It neither reloads media nor claims that a native mutation has completed.
func (s *APIServer) handleVLCIntent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string                `json:"sessionId"`
		Command   *playbackcmd.Identity `json:"command"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Command == nil || req.Command.Type != "intent" {
		http.Error(w, "versioned intent required", 400)
		return
	}
	t, ok := s.admitVLC(w, r, req.Command, "intent", req.SessionID)
	if !ok {
		return
	}
	s.vlcACK(w, t, map[string]any{"ack": "intent-invalidated"})
}

// Cancel from shutdown is not a rollback of any C operation already entered.
func (s *APIServer) cancelVLCCommands() {
	if s.pm != nil {
		s.pm.RevokeSources()
	}
	s.vlcGate.order.Shutdown()
}
