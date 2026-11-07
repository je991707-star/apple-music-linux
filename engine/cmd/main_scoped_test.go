//go:build u2_source_fixture && vlc_ordering_test && linux && cgo

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"engine/internal/playbackcmd"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScopedOwnMetadataAndUnknownSamples(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	code, v := r.call(r.s.handleMainSource, map[string]any{"operation": "metadata", "sessionId": g.Session}, g.Secret, "")
	if code != 200 {
		t.Fatalf("own metadata rejected: %d", code)
	}
	if string(v["sampleDurationMs"]) != "null" || string(v["readerError"]) != "null" {
		t.Fatal("unknown metadata invented")
	}
	code, _ = r.call(r.s.handleMainSource, map[string]any{"operation": "metadata", "sessionId": g.Session}, "", "")
	if code != 403 {
		t.Fatal("metadata without source proof", code)
	}
}
func TestScopedEventsReachTheirOwnFence(t *testing.T) {
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	entered := false
	h := mainPlaybackBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered = true
		http.Error(w, "source proof required", 401)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/main/source/events", nil))
	if !entered || w.Code != 401 {
		t.Fatalf("scoped SSE boundary not migrated: %d", w.Code)
	}
}

func TestScopedRealStreamReleaseAndForeignProof(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	server := httptest.NewServer(http.HandlerFunc(r.s.handleOwnSourceEvents))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	request := func(proof, session string) *http.Request {
		q, _ := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(`{"sessionId":"`+session+`"}`))
		q.Header.Set("X-Api-Token", strings.Repeat("t", 64))
		q.Header.Set("X-Main-Source", proof)
		return q
	}
	wrong, e := http.DefaultClient.Do(request(strings.Repeat("f", 64), g.Session))
	if e != nil {
		t.Fatal(e)
	}
	wrong.Body.Close()
	if wrong.StatusCode != 403 {
		t.Fatal("foreign proof", wrong.StatusCode)
	}
	resp, e := http.DefaultClient.Do(request(g.Secret, g.Session))
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	if !scan.Scan() || scan.Text() != "event: own" || !scan.Scan() {
		t.Fatal("no scoped event")
	}
	var event ownEvent
	if json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &event) != nil || event.Identity.Session != g.Session || event.Metadata == nil || event.Metadata.SampleDuration != nil {
		t.Fatal("invalid scoped snapshot")
	}
	if strings.Contains(scan.Text(), g.Secret) {
		t.Fatal("source proof leaked")
	}
	code, _ := r.call(r.s.handleMainSource, map[string]any{"operation": "release", "sessionId": g.Session}, g.Secret, "")
	if code != 200 {
		t.Fatal(code)
	}
	// EOF must arrive without cancelling the client or waiting for stream expiry.
	done := make(chan bool, 1)
	go func() {
		for scan.Scan() {
		}
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retired source kept stream open")
	}
}
func TestScopedFixedLibraryArtworkAndMutationRejection(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	for _, op := range []string{"metadata", "library", "artwork"} {
		q := map[string]any{"operation": op, "sessionId": g.Session}
		if op == "library" {
			q["limit"] = 1
		}
		code, _ := r.call(r.s.handleMainSource, q, g.Secret, "")
		if code != 200 {
			t.Fatal(op, code)
		}
	}
	for _, q := range []map[string]any{{"operation": "metadata", "sessionId": g.Session, "value": 50}, {"operation": "library", "sessionId": g.Session, "page": 11, "limit": 1}, {"operation": "artwork", "sessionId": g.Session, "url": "https://example.test"}, {"operation": "touch", "sessionId": g.Session, "value": 50}} {
		code, _ := r.call(r.s.handleMainSource, q, g.Secret, "")
		if code < 400 {
			t.Fatal("unsafe query accepted")
		}
	}
}
func TestScopedVolumeNativeUnconfirmedIsNotSuccess(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	g, code := r.claim(g)
	if code != 200 {
		t.Fatal(code)
	}
	id := playbackcmd.Identity{Boot: g.Boot, Epoch: g.Epoch, Session: g.Session, Load: 1, Sequence: 1, Type: "load", Request: "volume-load"}
	code, _ = r.call(r.s.handleVLCLoad, map[string]any{"sessionId": g.Session, "assetId": g.Asset, "command": id}, g.Secret, "")
	if code != 200 {
		t.Fatal(code)
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, _, state := r.s.vlcPlayer.Time()
		if state == "playing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native playing deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for n, expected := range []int{503, 409} {
		code, _ = r.call(r.s.handleMainSource, map[string]any{"operation": "volume", "sessionId": g.Session, "value": 80, "revision": 1}, g.Secret, "")
		if code != expected {
			t.Fatal(n, code)
		}
	}
	r.call(r.s.handleMainSource, map[string]any{"operation": "release", "sessionId": g.Session}, g.Secret, "")
	code, _ = r.call(r.s.handleMainSource, map[string]any{"operation": "volume", "sessionId": g.Session, "value": 100, "revision": 2}, g.Secret, "")
	if code != 403 {
		t.Fatal("retired volume", code)
	}
}
func TestScopedRingBoundAndSubscriptionLimit(t *testing.T) {
	l := &ownEventLog{}
	if !l.open() || !l.open() || l.open() {
		t.Fatal("subscription bound")
	}
	defer l.close()
	defer l.close()
	for i := 0; i < 100; i++ {
		l.push(ownIdentity{}, "snapshot", nil)
	}
	if len(l.items) != 16 {
		t.Fatal("unbounded ring")
	}
	if _, ok := l.since(1); ok {
		t.Fatal("lagging subscriber silently skipped events")
	}
}

func TestScopedCrossBootAndRetiredSourceCannotObserve(t *testing.T) {
	a := newMainSourceRig(t)
	ga, _ := a.create("alac-a", 1)
	b := newMainSourceRig(t)
	gb, _ := b.create("alac-b", 1)
	if ga.Boot == gb.Boot {
		t.Fatal("shared Boot")
	}
	for _, q := range []map[string]any{{"operation": "metadata", "sessionId": ga.Session}, {"operation": "metadata", "sessionId": gb.Session}, {"operation": "volume", "sessionId": gb.Session, "value": 80, "revision": 1}} {
		code, _ := b.call(b.s.handleMainSource, q, ga.Secret, "")
		if code != 403 {
			t.Fatal("cross-instance proof", code)
		}
	}
	next, _ := a.create("alac-b", 2)
	for _, op := range []string{"metadata", "library", "artwork", "volume"} {
		q := map[string]any{"operation": op, "sessionId": next.Session}
		code, _ := a.call(a.s.handleMainSource, q, ga.Secret, "")
		if code != 403 {
			t.Fatal("A adopted B", op, code)
		}
	}
}
func TestScopedReconnectionStartsAfterOldEventHistory(t *testing.T) {
	r := newMainSourceRig(t)
	g, _ := r.create("alac-a", 1)
	r.s.mainSources.mu.Lock()
	grant := r.s.mainSources.grants[g.Session]
	r.s.mainSources.mu.Unlock()
	for i := 0; i < 20; i++ {
		grant.events.push(grant.ownIdentity(), "command-seek", nil)
	}
	server := httptest.NewServer(http.HandlerFunc(r.s.handleOwnSourceEvents))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	q, _ := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(`{"sessionId":"`+g.Session+`"}`))
	q.Header.Set("X-Api-Token", strings.Repeat("t", 64))
	q.Header.Set("X-Main-Source", g.Secret)
	resp, e := http.DefaultClient.Do(q)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	if !scan.Scan() || !scan.Scan() {
		t.Fatal("no fresh snapshot")
	}
	var event ownEvent
	if json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &event) != nil || event.Kind != "snapshot" || event.Seq <= 20 || event.Metadata == nil {
		t.Fatal("replayed old stream instead of snapshot", scan.Text())
	}
}
