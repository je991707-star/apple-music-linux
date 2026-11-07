package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMainTypedFactoryCanReachItsOwnAuthorizationFence(t *testing.T) {
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	entered := false
	h := mainPlaybackBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered = true
		http.Error(w, "issuer proof required", 401)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/main/source", nil))
	if !entered || w.Code != 401 {
		t.Fatalf("typed factory hidden behind status-only boundary: entered=%v status=%d; want factory's own issuer fence", entered, w.Code)
	}
}
