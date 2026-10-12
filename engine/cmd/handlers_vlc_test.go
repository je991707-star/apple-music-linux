package main

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidateALACCacheRemovesOnlyALACFilesOnce(t *testing.T) {
	dir := t.TempDir()
	keep := []string{"123-aac.m4a", "456.mp4", "notes.txt"}
	drop := []string{"123-alac.m4a", "789-alac.m4a"}
	for _, n := range append(append([]string{}, keep...), drop...) {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	os.WriteFile(filepath.Join(dir, "in-progress-alac.m4a.tmp"), []byte("x"), 0o600)

	invalidateALACCacheIfStale(dir)

	for _, n := range drop {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s from an older decrypt path must be deleted", n)
		}
	}
	for _, n := range append(keep, "in-progress-alac.m4a.tmp") {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s must be kept: %v", n, err)
		}
	}
	ver, _ := os.ReadFile(filepath.Join(dir, ".alac_ver"))
	if strings.TrimSpace(string(ver)) != alacCacheVersion {
		t.Fatalf("version marker = %q, want %q", ver, alacCacheVersion)
	}

	// Files committed after the migration survive later startups.
	fresh := filepath.Join(dir, "999-alac.m4a")
	os.WriteFile(fresh, []byte("x"), 0o600)
	invalidateALACCacheIfStale(dir)
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a current-version cache file was deleted on the next start: %v", err)
	}
}

func TestInvalidateALACCacheToleratesMissingDir(t *testing.T) {
	invalidateALACCacheIfStale(filepath.Join(t.TempDir(), "absent"))
}

// Without libvlc every VLC endpoint must answer 503, not panic on a nil player.
func TestVLCEndpointsWithoutLibVLC(t *testing.T) {
	s := &APIServer{}
	for name, h := range map[string]http.HandlerFunc{
		"load":   s.handleVLCLoad,
		"pause":  s.handleVLCPause,
		"resume": s.handleVLCResume,
		"time":   s.handleVLCTime,
		"seek":   s.handleVLCSeek,
		"stop":   s.handleVLCStop,
		"volume": s.handleVLCVolume,
	} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/api/v1/vlc/"+name, strings.NewReader(`{"volume":50}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", name, rec.Code)
		}
	}
}

func TestHandshakeNoiseFilterDropsOnlyEOFHandshakes(t *testing.T) {
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	f := handshakeNoiseFilter{}
	f.Write([]byte("2026/10/13 http: TLS handshake error from 127.0.0.1:45758: EOF\n"))
	if buf.Len() != 0 {
		t.Fatalf("EOF handshake noise was logged: %q", buf.String())
	}
	f.Write([]byte("2026/10/13 http: TLS handshake error from 127.0.0.1:1: tls: bad certificate\n"))
	f.Write([]byte("2026/10/13 http: panic serving 127.0.0.1:1: boom\n"))
	if !strings.Contains(buf.String(), "bad certificate") || !strings.Contains(buf.String(), "panic serving") {
		t.Fatalf("real server errors must pass through: %q", buf.String())
	}
}
