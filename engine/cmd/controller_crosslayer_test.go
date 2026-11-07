//go:build vlc_ordering_test && cgo && linux

package main

import (
	"context"
	"encoding/json"
	"engine/core/playback"
	"engine/core/vlc"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Private synthetic bridge only. No application API, TLS/principal qualification,
// account, DRM, or normal MusicKit window is exercised by this test.
func TestControllerNativeCrossLayer(t *testing.T) {
	t.Setenv("ADAM_VLC_ORDERED", "1")
	t.Setenv("ADAM_API_TOKEN", "u2-local-fixture-client-only")
	node := os.Getenv("U2_NODE")
	if node == "" {
		t.Fatal("U2_NODE required; never launch Electron application")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"retry-seek", "retry-pause", "preparing-retry-seek", "preparing-retry-pause", "late-retry-retire-swap", "old-404-after-swap", "source-failure-ended", "duplicate-terminal", "terminal-reload", "engine-boot-rotation"} {
		t.Run(name, func(t *testing.T) {
			r := newAuthorityRig(t, true)
			b, e := r.s.pm.Open(context.Background(), playback.OpenRequest{AssetID: "authority-synthetic", Private: true})
			if e != nil {
				t.Fatal(e)
			}
			var current atomic.Pointer[authorityRig]
			current.Store(r)
			var restarted *authorityRig
			if name == "engine-boot-rotation" {
				restarted = newAuthorityRig(t, true)
			}
			prepareEntered, prepareRelease := make(chan struct{}), make(chan struct{})
			var blockPrepare atomic.Bool
			var enteredOnce, releaseOnce sync.Once
			releasePrepare := func() { releaseOnce.Do(func() { close(prepareRelease) }) }
			t.Cleanup(releasePrepare)
			baseHooks, _ := retirementHooks.Load(r.s)
			hooks := baseHooks.(retirementTestHooks)
			hooks.prepare = func(_ vlc.Source) {
				if blockPrepare.Swap(false) {
					enteredOnce.Do(func() { close(prepareEntered) })
					<-prepareRelease
				}
			}
			retirementHooks.Store(r.s, hooks)
			mux := http.NewServeMux()
			mux.HandleFunc("/fixture/hold-preparation", func(w http.ResponseWriter, q *http.Request) { blockPrepare.Store(true); w.WriteHeader(204) })
			mux.HandleFunc("/fixture/preparing", func(w http.ResponseWriter, q *http.Request) {
				select {
				case <-prepareEntered:
					w.WriteHeader(204)
				case <-time.After(3 * time.Second):
					http.Error(w, "preparation deadline", 500)
				}
			})
			mux.HandleFunc("/fixture/release-preparation", func(w http.ResponseWriter, q *http.Request) { releasePrepare(); w.WriteHeader(204) })
			mux.HandleFunc("/fixture/restart", func(w http.ResponseWriter, q *http.Request) {
				if restarted == nil {
					http.Error(w, "not configured", 400)
					return
				}
				r.s.cancelVLCCommands()
				current.Store(restarted)
				json.NewEncoder(w).Encode(map[string]string{"sessionId": restarted.id})
			})
			for _, route := range []string{"load", "time", "control", "intent", "seek", "pause", "resume", "stop"} {
				mux.HandleFunc("/api/v1/vlc/"+route, func(w http.ResponseWriter, q *http.Request) {
					s := current.Load().s
					fn := map[string]http.HandlerFunc{"load": s.handleVLCLoad, "time": s.handleVLCTime, "control": s.handleVLCControl, "intent": s.handleVLCIntent, "seek": s.handleVLCSeek, "pause": s.handleVLCPause, "resume": s.handleVLCResume, "stop": s.handleVLCStop}[route]
					fn(w, q)
				})
			}
			mux.HandleFunc("/fixture/stats", func(w http.ResponseWriter, q *http.Request) {
				v := current.Load()
				json.NewEncoder(w).Encode(map[string]any{"pcm": v.sink.Snapshot(), "native": v.s.vlcPlayer.OrderingOwnership(), "control": v.s.vlcGate.order.Snapshot()})
			})
			mux.HandleFunc("/fixture/retire", func(w http.ResponseWriter, q *http.Request) { r.s.pm.Release(r.id); w.WriteHeader(204) })
			mux.HandleFunc("/fixture/fail", func(w http.ResponseWriter, q *http.Request) {
				if !r.finished.Swap(true) {
					r.writer.Discard()
				}
				w.WriteHeader(204)
			})
			mux.HandleFunc("/fixture/complete", func(w http.ResponseWriter, q *http.Request) {
				if !r.finished.Swap(true) {
					if e := r.writer.Commit(); e != nil {
						http.Error(w, e.Error(), 500)
						return
					}
				}
				w.WriteHeader(204)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, node, filepath.Join(root, "verification/u2-controller/crosslayer.mjs"), name, server.URL, r.id, b.ID)
			cmd.Env = append(os.Environ(), "ELECTRON_RUN_AS_NODE=1")
			out, e := cmd.CombinedOutput()
			t.Log(strings.TrimSpace(string(out)))
			if e != nil {
				t.Fatalf("controller/native %s: %v", name, e)
			}
		})
	}
}
