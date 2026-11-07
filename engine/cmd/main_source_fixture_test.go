//go:build u2_source_fixture && vlc_ordering_test && linux && cgo

package main

import (
	"context"
	"encoding/json"
	"engine/core/diskcache"
	"engine/core/playback"
	"engine/core/vlc"
	"engine/internal/playbackcmd"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type mainSourceRig struct {
	s      *APIServer
	t      *testing.T
	issuer string
}

func newMainSourceRig(t *testing.T) *mainSourceRig {
	t.Helper()
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	t.Setenv("ADAM_VLC_ORDERED", "1")
	t.Setenv("ADAM_API_TOKEN", strings.Repeat("t", 64))
	t.Setenv("ADAM_MAIN_SOURCE_ISSUER", strings.Repeat("i", 64))
	if os.Getenv("ADAM_MAIN_FIXTURE_DIR") == "" {
		t.Fatal("ADAM_MAIN_FIXTURE_DIR required")
	}
	p, e := mainSourcePlayer()
	if e != nil {
		t.Fatal(e)
	}
	cache, e := diskcache.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &APIServer{pm: playback.NewWithProvider(fixedMainProvider{}), vlcPlayer: p, diskCache: cache, alac: newALACGate(), shutdownCtx: ctx}
	t.Cleanup(func() {
		cancel()
		s.cancelVLCCommands()
		p.Close()
		if sink, ok := mainFixtureSinks.LoadAndDelete(p); ok {
			sink.(*vlc.IsolatedSink).Close()
		}
	})
	return &mainSourceRig{s: s, t: t, issuer: strings.Repeat("i", 64)}
}
func (r *mainSourceRig) call(fn http.HandlerFunc, q any, secret, issuer string) (int, map[string]json.RawMessage) {
	b, _ := json.Marshal(q)
	req := httptest.NewRequest("POST", "/api/v1/main/source", strings.NewReader(string(b)))
	req.Header.Set("X-Api-Token", strings.Repeat("t", 64))
	req.Header.Set("X-Main-Source", secret)
	req.Header.Set("X-Main-Issuer", issuer)
	w := httptest.NewRecorder()
	fn(w, req)
	var result map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &result)
	r.t.Logf("status=%d stage=%s", w.Code, result["stage"])
	return w.Code, result
}
func (r *mainSourceRig) create(asset string, revision uint64) (mainSourceGrant, *playback.Session) {
	q := mainSourceRequest{Operation: "create", Principal: "00000000-0000-0000-0000-000000000001", MainGeneration: "00000000-0000-0000-0000-000000000002", DocumentGeneration: 1, Boot: r.s.vlcGate.order.Snapshot().Boot, OwnershipRevision: revision, Intent: asset}
	code, v := r.call(r.s.handleMainSource, q, "", r.issuer)
	if code != 201 {
		r.t.Fatalf("factory status=%d", code)
	}
	var g mainSourceGrant
	var sess playback.Session
	json.Unmarshal(v["grant"], &g)
	json.Unmarshal(v["session"], &sess)
	return g, &sess
}
func (r *mainSourceRig) claim(g mainSourceGrant) (mainSourceGrant, int) {
	snap := r.s.vlcGate.order.Snapshot()
	code, v := r.call(r.s.handleVLCControl, map[string]any{"boot": g.Boot, "epoch": snap.Epoch, "sessionId": g.Session}, g.Secret, "")
	if code == 200 {
		var epoch string
		json.Unmarshal(v["epoch"], &epoch)
		g.Epoch = epoch
	}
	return g, code
}
func TestMainSourceFactoryPrivateAndProofBound(t *testing.T) {
	r := newMainSourceRig(t)
	g, a := r.create("alac-a", 1)
	for _, op := range []string{"read", "touch", "release"} {
		code, _ := r.call(r.s.handleMainSource, mainSourceRequest{Operation: op, Session: g.Session}, "", "")
		if code != 403 {
			t.Fatalf("transport token became source right %s", op)
		}
	}
	code, _ := r.call(r.s.handleMainSource, mainSourceRequest{Operation: "touch", Session: g.Session}, g.Secret, "")
	if code != 200 {
		t.Fatal("own touch rejected")
	}
	g2, b := r.create("alac-a", 2)
	if a.ID == b.ID || g.LeaseID == g2.LeaseID || g.SourceGeneration == g2.SourceGeneration {
		t.Fatal("private session/lease reused")
	}
	for _, op := range []string{"touch", "read", "release"} {
		code, _ = r.call(r.s.handleMainSource, mainSourceRequest{Operation: op, Session: g.Session}, g.Secret, "")
		if code != 403 {
			t.Fatal("retired source revived")
		}
	}
	code, _ = r.call(r.s.handleMainSource, mainSourceRequest{Operation: "touch", Session: g2.Session}, g.Secret, "")
	if code != 403 {
		t.Fatal("old proof adopted B")
	}
	code, _ = r.call(r.s.handleMainSource, mainSourceRequest{Operation: "release", Session: g2.Session}, g2.Secret, "")
	if code != 200 {
		t.Fatal("release rejected")
	}
	if _, ok := r.s.pm.GetSession(g2.Session); ok {
		t.Fatal("released session retained")
	}
}
func TestMainSourceRetiredClaimDoesNotBlockNewSession(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	_, code := r.claim(g)
	if code != 200 {
		t.Fatal("first claim rejected")
	}
	next, _ := r.create("alac-b", 2)
	_, code = r.claim(next)
	if code != 200 {
		t.Fatalf("retired unloaded claim stranded new source: status=%d", code)
	}
}
func TestMainSourceActualNativePCM(t *testing.T) {
	for _, asset := range []string{"alac-a", "alac-b", "alac-tail"} {
		t.Run(asset, func(t *testing.T) {
			r := newMainSourceRig(t)
			g, _ := r.create(asset, 1)
			g, code := r.claim(g)
			if code != 200 {
				t.Fatal("claim failed")
			}
			_, ref, _, e := fixtureBytes(asset)
			if e != nil {
				t.Fatal(e)
			}
			sink, _ := mainFixtureSinks.Load(r.s.vlcPlayer)
			sink.(*vlc.IsolatedSink).Reset(ref)
			id := playbackcmd.Identity{Boot: g.Boot, Epoch: g.Epoch, Session: g.Session, Load: 1, Sequence: 1, Type: "load", Request: "source-native-load"}
			code, _ = r.call(r.s.handleVLCLoad, map[string]any{"sessionId": g.Session, "assetId": g.Asset, "startMs": 0, "command": id}, g.Secret, "")
			if code != 200 {
				t.Fatal("load failed")
			}
			deadline := time.Now().Add(7 * time.Second)
			var stats map[string]any
			for time.Now().Before(deadline) {
				stats = sink.(*vlc.IsolatedSink).Snapshot()
				_, _, state := r.s.vlcPlayer.Time()
				if state == "ended" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if stats["full_pcm_exact"] != true || stats["frames"] != len(ref)/4 {
				t.Fatalf("PCM identity failed: %+v", stats)
			}
			t.Logf("actual callback PCM frames=%v exact=%v", stats["frames"], stats["full_pcm_exact"])
			code, _ = r.call(r.s.handleMainSource, mainSourceRequest{Operation: "release", Session: g.Session}, g.Secret, "")
			if code != 200 {
				t.Fatal("release failed")
			}
		})
	}
}
func TestMainSourceIssuerAndSessionSecurity(t *testing.T) {
	r := newMainSourceRig(t)
	valid := mainSourceRequest{Operation: "create", Principal: "00000000-0000-0000-0000-000000000001", MainGeneration: "00000000-0000-0000-0000-000000000002", DocumentGeneration: 1, Boot: r.s.vlcGate.order.Snapshot().Boot, OwnershipRevision: 1, Intent: "alac-a"}
	t.Run("global-token-not-issuer", func(t *testing.T) {
		code, _ := r.call(r.s.handleMainSource, valid, "", "")
		if code != 403 {
			t.Fatal(code)
		}
	})
	t.Run("old-boot", func(t *testing.T) {
		q := valid
		q.Boot = "old-boot"
		code, _ := r.call(r.s.handleMainSource, q, "", r.issuer)
		if code != 409 {
			t.Fatal(code)
		}
	})
	t.Run("arbitrary-source-and-path-denied", func(t *testing.T) {
		q := valid
		q.Intent = "/tmp/arbitrary.mp4"
		code, _ := r.call(r.s.handleMainSource, q, "", r.issuer)
		if code != 422 {
			t.Fatal(code)
		}
		code, _ = r.call(r.s.handleMainSource, map[string]any{"operation": "create", "url": "https://example.invalid/input"}, "", r.issuer)
		if code != 400 {
			t.Fatal(code)
		}
	})
	g, a := r.create("alac-a", 2)
	t.Run("no-duplicate-issuance", func(t *testing.T) {
		q := valid
		q.OwnershipRevision = 2
		code, _ := r.call(r.s.handleMainSource, q, "", r.issuer)
		if code != 409 {
			t.Fatal(code)
		}
	})
	q := valid
	q.Principal = "00000000-0000-0000-0000-000000000003"
	q.MainGeneration = "00000000-0000-0000-0000-000000000004"
	q.OwnershipRevision = 1
	code, v := r.call(r.s.handleMainSource, q, "", r.issuer)
	if code != 201 {
		t.Fatal("second owner factory", code)
	}
	var other mainSourceGrant
	json.Unmarshal(v["grant"], &other)
	if other.Session == g.Session || other.LeaseID == g.LeaseID {
		t.Fatal("same asset crossed owners")
	}
	if _, ok := r.s.pm.GetSession(a.ID); !ok {
		t.Fatal("other owner retired A")
	}
	t.Run("cross-owner-proof-rejected", func(t *testing.T) {
		code, _ := r.call(r.s.handleMainSource, mainSourceRequest{Operation: "touch", Session: other.Session}, g.Secret, "")
		if code != 403 {
			t.Fatal(code)
		}
	})
	t.Run("retired-proof-cannot-reopen", func(t *testing.T) {
		r.s.pm.Release(g.Session)
		code, _ := r.call(r.s.handleMainSource, mainSourceRequest{Operation: "reopen", Session: g.Session}, g.Secret, "")
		if code != 403 {
			t.Fatal(code)
		}
	})
	t.Run("expired-source-touch-cannot-revive", func(t *testing.T) {
		code, _ := r.call(r.s.handleMainSource, mainSourceRequest{Operation: "touch", Session: g.Session}, g.Secret, "")
		if code != 404 {
			t.Fatal(code)
		}
		if _, ok := r.s.pm.GetSession(g.Session); ok {
			t.Fatal("resurrected")
		}
	})
	t.Run("unknown-operation-and-lease-injection", func(t *testing.T) {
		for _, q := range []any{map[string]any{"operation": "debug", "sessionId": other.Session}, map[string]any{"operation": "touch", "sessionId": other.Session, "leaseId": g.LeaseID}} {
			code, _ := r.call(r.s.handleMainSource, q, other.Secret, "")
			if code != 400 {
				t.Fatal(code)
			}
		}
	})
}
func TestMainSourceTerminalSeekActualSuffix(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	g, code := r.claim(g)
	if code != 200 {
		t.Fatal(code)
	}
	id := playbackcmd.Identity{Boot: g.Boot, Epoch: g.Epoch, Session: g.Session, Load: 1, Sequence: 1, Type: "load", Request: "terminal-load"}
	code, _ = r.call(r.s.handleVLCLoad, map[string]any{"sessionId": g.Session, "startMs": 0, "command": id}, g.Secret, "")
	if code != 200 {
		t.Fatal(code)
	}
	end := time.Now().Add(7 * time.Second)
	for time.Now().Before(end) {
		_, _, state := r.s.vlcPlayer.Time()
		if state == "ended" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	id.Sequence = 2
	id.Type = "seek"
	id.Request = "terminal-seek"
	code, v := r.call(r.s.handleVLCSeek, map[string]any{"sessionId": g.Session, "posMs": 500, "command": id}, g.Secret, "")
	if code != 200 || string(v["reloaded"]) != "true" {
		t.Fatalf("terminal reload not accepted %d", code)
	}
	sink, _ := mainFixtureSinks.Load(r.s.vlcPlayer)
	end = time.Now().Add(7 * time.Second)
	var stats map[string]any
	for time.Now().Before(end) {
		stats = sink.(*vlc.IsolatedSink).Snapshot()
		if stats["post_flush_suffix_exact"] == true {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	origin := stats["first_post_flush_pcm_ms"].(float64)
	if stats["post_flush_suffix_exact"] != true || origin < 500 || origin-500 > 4096.0/48 {
		t.Fatalf("strict PCM suffix: %+v", stats)
	}
	t.Logf("terminal native suffix exact, origin=%v target=500 one-frame=%v", origin, 4096.0/48)
}
func TestMainTransportStatusHasNoSourceMetadata(t *testing.T) {
	r := newMainSourceRig(t)
	w := httptest.NewRecorder()
	q := httptest.NewRequest("GET", "/api/v1/vlc/time", nil)
	q.Header.Set("X-Api-Token", strings.Repeat("t", 64))
	r.s.handleVLCTime(w, q)
	var v map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &v)
	for _, key := range []string{"posMs", "lengthMs", "eof", "volume", "pcm", "sessionId"} {
		if _, exists := v[key]; exists {
			t.Fatalf("transport token exposed unowned source field %s", key)
		}
	}
	var c map[string]json.RawMessage
	json.Unmarshal(v["control"], &c)
	if _, exists := c["sessionId"]; exists {
		t.Fatal("transport token exposed another session")
	}
}
func TestMainSourceReleaseCancelsPrivateProducerAndDrains(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-delay", 1)
	g, code := r.claim(g)
	if code != 200 {
		t.Fatal(code)
	}
	r.s.mainSources.mu.Lock()
	l := r.s.mainSources.grants[g.Session].lease
	r.s.mainSources.mu.Unlock()
	id := playbackcmd.Identity{Boot: g.Boot, Epoch: g.Epoch, Session: g.Session, Load: 1, Sequence: 1, Type: "load", Request: "delayed-source-load"}
	code, _ = r.call(r.s.handleVLCLoad, map[string]any{"sessionId": g.Session, "command": id}, g.Secret, "")
	if code != 200 {
		t.Fatal(code)
	}
	code, _ = r.call(r.s.handleMainSource, mainSourceRequest{Operation: "release", Session: g.Session}, g.Secret, "")
	if code != 200 {
		t.Fatal(code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := l.WaitResources(ctx); e != nil {
		t.Fatal("producer/reader drain", e)
	}
	if r.s.pm.ActiveStreams() != 0 {
		t.Fatal("private producer remained active")
	}
	if l.Active() {
		t.Fatal("release did not revoke authority")
	}
	t.Log("authority revoked; actual private producer and callback resources drained")
}
func TestMainSourceMissingCallbackCannotFallBackToLegacyHTTP(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	g, code := r.claim(g)
	if code != 200 {
		t.Fatal(code)
	}
	r.s.diskCache = nil
	id := playbackcmd.Identity{Boot: g.Boot, Epoch: g.Epoch, Session: g.Session, Load: 1, Sequence: 1, Type: "load", Request: "no-authorized-callback"}
	code, _ = r.call(r.s.handleVLCLoad, map[string]any{"sessionId": g.Session, "command": id}, g.Secret, "")
	if code == 200 {
		t.Fatal("missing private callback acknowledged as media-committed")
	}
	if r.s.vlcPlayer.OrderingOwnership()["physicalLoad"].(int) != 0 {
		t.Fatal("Main source reached unqualified legacy HTTP Native load")
	}
}
