package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMainAPITokenIsNotSourceAuthority(t *testing.T) {
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	for _, path := range []string{"/api/v1/playback/arbitrary/audio", "/api/v1/playback/arbitrary/touch", "/api/v1/drm/status", "/api/v1/vlc/time?access_token=offline-synthetic-key"} {
		entered := false
		h := requireToken("offline-synthetic-key", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered = true; w.WriteHeader(204) }))
		q := httptest.NewRequest("GET", path, nil)
		q.Header.Set("X-Api-Token", "offline-synthetic-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		if w.Code != 403 || entered {
			t.Errorf("Main token granted unscoped route: %s code=%d entered=%v", path, w.Code, entered)
		}
	}
}
