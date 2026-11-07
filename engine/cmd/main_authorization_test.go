//go:build vlc_ordering_test && cgo && linux

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMainSourceFactoryUnavailableCannotAdoptSession(t *testing.T) {
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	t.Setenv("ADAM_VLC_ORDERED", "1")
	t.Setenv("ADAM_API_TOKEN", "offline-synthetic-key")
	r := newAuthorityRig(t, true)
	for route, fn := range map[string]http.HandlerFunc{"load": r.s.handleVLCLoad, "seek": r.s.handleVLCSeek, "pause": r.s.handleVLCPause, "resume": r.s.handleVLCResume, "stop": r.s.handleVLCStop, "control": r.s.handleVLCControl} {
		b, _ := json.Marshal(map[string]any{"sessionId": r.id, "posMs": 500})
		req := httptest.NewRequest("POST", "/api/v1/vlc/"+route, bytes.NewReader(b))
		req.Header.Set("X-Api-Token", "offline-synthetic-key")
		w := httptest.NewRecorder()
		fn(w, req)
		if w.Code != 403 {
			t.Fatalf("%s global token adopted source: %d", route, w.Code)
		}
	}
	if r.s.vlcPlayer.OrderingOwnership()["physicalLoad"].(int) != 0 {
		t.Fatal("denied source reached Native")
	}
	r.s.pm.Release(r.id)
	if _, ok := r.s.pm.GetSession(r.id); ok {
		t.Fatal("released Source resurrected")
	}
}
