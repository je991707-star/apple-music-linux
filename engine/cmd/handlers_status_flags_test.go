package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Without a DRM manager (a build without adamplay_backend) the session can never become ready, which the rest
// of the status cannot tell apart from "not ready yet"; the page uses the flag to stop offering lossless.
func TestDRMAvailabilityIsReportedForABuildWithoutDRM(t *testing.T) {
	s := &APIServer{lifecycle: &engineLifecycle{}}

	rec := httptest.NewRecorder()
	s.handleDRMStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/drm/status", nil))
	var status struct {
		Available *bool `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || status.Available == nil || *status.Available {
		t.Fatalf("drm status: available = %v (err %v), want present and false; body %s", status.Available, err, rec.Body)
	}

	rec = httptest.NewRecorder()
	s.handleCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	var caps struct {
		Lossless bool `json:"lossless"`
		DRM      struct {
			Available *bool `json:"available"`
		} `json:"drm"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil || caps.DRM.Available == nil || *caps.DRM.Available || caps.Lossless {
		t.Fatalf("capabilities: %+v (err %v), want lossless=false and drm.available=false; body %s", caps, err, rec.Body)
	}
}

func TestToolsReportsFFmpegAvailabilityHonestly(t *testing.T) {
	s := &APIServer{}
	rec := httptest.NewRecorder()
	// A path that cannot name ffmpeg must come back unavailable with a reason, never as a pass.
	s.handleTools(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tools?ffmpegPath=/bin/ls", nil))
	var out struct {
		FFmpeg struct {
			Available bool   `json:"available"`
			Error     string `json:"error"`
		} `json:"ffmpeg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.FFmpeg.Available || out.FFmpeg.Error == "" {
		t.Fatalf("tools: %+v (err %v), want unavailable with an error; body %s", out, err, rec.Body)
	}
}
