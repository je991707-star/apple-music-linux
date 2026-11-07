package main

import (
	"net/http"
	"os"
)

// Opt-in normal Main qualification is status-only until a per-source Session
// Factory is qualified. Even the Engine-wide API token cannot acquire audio,
// touch/reopen/release another Session, or call DRM/non-playback APIs here.
// Legacy construction returns the original handler unchanged.
func mainPlaybackBoundary(next http.Handler) http.Handler {
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := r.Method == http.MethodGet && r.URL.Path == "/api/v1/vlc/time"
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/v1/main/auth", "/api/v1/main/source/events", "/api/v1/main/source", "/api/v1/vlc/control", "/api/v1/vlc/intent", "/api/v1/vlc/load", "/api/v1/vlc/seek", "/api/v1/vlc/pause", "/api/v1/vlc/resume", "/api/v1/vlc/stop":
				allowed = true
			}
		}
		if !allowed || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" {
			http.Error(w, "source authorization unavailable", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
