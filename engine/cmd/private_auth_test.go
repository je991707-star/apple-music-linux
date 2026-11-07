package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"engine/core/drm"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type privateAuthBackend struct {
	drm.DRMBackend
	events chan drm.DRMEvent
}

func (b *privateAuthBackend) SetAuthSource(drm.AuthSource) {}
func (b *privateAuthBackend) Running() bool                { return false }
func (b *privateAuthBackend) Events() <-chan drm.DRMEvent  { return b.events }
func authServer(t *testing.T) (*APIServer, drm.AuthBinding) {
	t.Helper()
	for k, v := range map[string]string{"ADAM_MAIN_PLAYBACK": "1", "ADAM_API_TOKEN": strings.Repeat("x", 64), "ADAM_MAIN_SOURCE_ISSUER": strings.Repeat("y", 64), "ADAM_MAIN_AUTH_ACCOUNT": "00000000-0000-4000-8000-000000000001", "ADAM_MAIN_AUTH_EPOCH": "1", "ADAM_MAIN_AUTH_OWNER": "00000000-0000-4000-8000-000000000002", "ADAM_MAIN_BOOT_NONCE": strings.Repeat("a", 64)} {
		t.Setenv(k, v)
	}
	dir := t.TempDir()
	b := &privateAuthBackend{events: make(chan drm.DRMEvent)}
	m := drm.NewDRMManager(b, drm.NewSessionManager(dir), nil, drm.BackendConfig{BaseDir: dir, RequireTrustedCompletion: true}, drm.DefaultRestartPolicy)
	t.Cleanup(func() { m.Shutdown(); close(b.events) })
	s := &APIServer{dm: m, privateAuth: privateAuthGuard{peerCheck: func(*http.Request) error { return nil }}}
	scope := drm.AuthBinding{AccountID: os.Getenv("ADAM_MAIN_AUTH_ACCOUNT"), AccountEpoch: 1, AttemptID: "attempt-anonymous", EngineOwnerID: os.Getenv("ADAM_MAIN_AUTH_OWNER"), EngineBoot: s.vlcGate.order.Snapshot().Boot, BootNonce: os.Getenv("ADAM_MAIN_BOOT_NONCE"), CredentialRevision: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	return s, scope
}
func callAuth(s *APIServer, q privateAuthRequest, edit func(*http.Request)) *httptest.ResponseRecorder {
	data, _ := json.Marshal(q)
	r := httptest.NewRequest("POST", "https://127.0.0.1/api/v1/main/auth", bytes.NewReader(data))
	r.TLS = &tls.ConnectionState{HandshakeComplete: true, Version: tls.VersionTLS13}
	r.Header.Set("X-Api-Token", os.Getenv("ADAM_API_TOKEN"))
	r.Header.Set("X-Main-Issuer", os.Getenv("ADAM_MAIN_SOURCE_ISSUER"))
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	s.handlePrivateAuth(w, r)
	return w
}
func TestU219PrivateRouteProgressAndReplay(t *testing.T) {
	s, b := authServer(t)
	for i, op := range []string{"begin", "status", "cancel", "status"} {
		w := callAuth(s, privateAuthRequest{Operation: op, Binding: b}, nil)
		want := 200
		if i == 0 {
			want = 202
		}
		if w.Code != want {
			t.Fatal(op, w.Code, w.Body.String())
		}
		var snap struct {
			AuthBinding         *drm.AuthBinding
			NativeAuthenticated bool
			AuthEvidence        *drm.NativeEvidence
		}
		if e := json.Unmarshal(w.Body.Bytes(), &snap); e != nil {
			t.Fatal(e)
		}
		if snap.NativeAuthenticated || snap.AuthEvidence != nil || snap.AuthBinding == nil || *snap.AuthBinding != b {
			t.Fatal("unscoped/qualified reply", snap)
		}
	}
	if w := callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil); w.Code != 409 {
		t.Fatal("replay", w.Code)
	}
}
func TestU219PrivateRouteRejectsUnownedRequests(t *testing.T) {
	for _, name := range []string{"tls", "api", "issuer", "origin", "fetch", "owner", "epoch", "account", "boot", "nonce", "peer"} {
		t.Run(name, func(t *testing.T) {
			s, b := authServer(t)
			edit := func(r *http.Request) {
				switch name {
				case "tls":
					r.TLS = nil
				case "api":
					r.Header.Del("X-Api-Token")
				case "issuer":
					r.Header.Del("X-Main-Issuer")
				case "origin":
					r.Header.Set("Origin", "app://adam")
				case "fetch":
					r.Header.Set("Sec-Fetch-Site", "same-origin")
				}
			}
			switch name {
			case "owner":
				b.EngineOwnerID = "other"
			case "epoch":
				b.AccountEpoch++
			case "account":
				b.AccountID = "other"
			case "boot":
				b.EngineBoot = strings.Repeat("f", 32)
			case "nonce":
				b.BootNonce = strings.Repeat("f", 64)
			case "peer":
				s.privateAuth.peerCheck = func(*http.Request) error { return errors.New("not parent") }
			}
			if w := callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, edit); w.Code != 403 {
				t.Fatal(name, w.Code, w.Body.String())
			}
		})
	}
}
func TestU219OtherDRMEndpointsStayDenied(t *testing.T) {
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	h := mainPlaybackBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, p := range []string{"/api/v1/drm/authenticate", "/api/v1/drm/status", "/api/v1/drm/challenge", "/api/v1/main/auth?x=1"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "https://127.0.0.1"+p, nil))
		if w.Code != 403 {
			t.Fatal(p, w.Code)
		}
	}
}
func TestU219CannotSendCredentialOrFixtureOperation(t *testing.T) {
	s, b := authServer(t)
	for _, op := range []string{"complete", "fixture", "login", "challenge", "password"} {
		if w := callAuth(s, privateAuthRequest{Operation: op, Binding: b}, nil); w.Code != 400 {
			t.Fatal(op, w.Code)
		}
	}
	data, _ := json.Marshal(privateAuthRequest{Operation: "begin", Binding: b})
	data = append(data[:len(data)-1], []byte(`,"password":"synthetic-disallowed"}`)...)
	r := httptest.NewRequest("POST", "https://127.0.0.1/api/v1/main/auth", bytes.NewReader(data))
	r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	r.Header.Set("X-Api-Token", os.Getenv("ADAM_API_TOKEN"))
	r.Header.Set("X-Main-Issuer", os.Getenv("ADAM_MAIN_SOURCE_ISSUER"))
	w := httptest.NewRecorder()
	s.handlePrivateAuth(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestU219ProcMetadataIsFailClosed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tcp")
	os.WriteFile(p, []byte("sl local remote st q q q uid time inode\n0: AAA BBB 01 0 0 0 123 0 456\n"), 0600)
	if inode, e := privateSocketInode([]string{p}, "AAA", "BBB", 123); e != nil || inode != "456" {
		t.Fatal(inode, e)
	}
	if _, e := privateSocketInode([]string{p}, "AAA", "BBB", 124); e == nil {
		t.Fatal("wrong UID")
	}
	if e := (privateAuthGuard{}).checkPeer(httptest.NewRequest("POST", "http://localhost", nil)); e == nil {
		t.Fatal("missing parent accepted")
	}
}
