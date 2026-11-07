//go:build u2_credential_fixture

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"engine/core/drm"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func inputCall220(s *APIServer, b drm.AuthBinding, op, id string, p *drm.PrivateInput, edit func(*http.Request)) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]any{"operation": op, "binding": b, "input": map[string][]byte{"email": p.Email, "password": p.Password, "reply": p.Reply}, "challengeId": id})
	defer clear(raw)
	r := httptest.NewRequest("POST", "https://127.0.0.1/api/v1/main/auth", bytes.NewReader(raw))
	r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	r.Header.Set("X-Api-Token", os.Getenv("ADAM_API_TOKEN"))
	r.Header.Set("X-Main-Issuer", os.Getenv("ADAM_MAIN_SOURCE_ISSUER"))
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	s.handlePrivateAuth(w, r)
	return w
}
func cred220() *drm.PrivateInput {
	return &drm.PrivateInput{Email: []byte("u220-user@example.invalid"), Password: []byte("u220-synthetic-marker")}
}
func TestU220FixtureBoundCredentialChallenge(t *testing.T) {
	s, b := authServer(t)
	configurePrivateInputFixture(s.dm)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	p := cred220()
	w := inputCall220(s, b, "credentials", "", p, nil)
	if w.Code != 200 {
		t.Fatal("credential not consumed", w.Code)
	}
	var out struct {
		drm.PrivateInputStatus
		NativeAuthenticated bool
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.NativeAuthenticated || out.Challenge == nil {
		t.Fatal("unsafe receipt")
	}
	if bytes.Contains(w.Body.Bytes(), p.Password) {
		t.Fatal("secret in response")
	}
	if inputCall220(s, b, "challenge", "foreign", &drm.PrivateInput{Reply: []byte("000001")}, nil).Code != 409 {
		t.Fatal("wrong challenge accepted")
	}
	if inputCall220(s, b, "challenge", out.Challenge.ID, &drm.PrivateInput{Reply: []byte("000001")}, nil).Code != 200 {
		t.Fatal("challenge not consumed")
	}
	if inputCall220(s, b, "challenge", out.Challenge.ID, &drm.PrivateInput{Reply: []byte("000001")}, nil).Code != 409 {
		t.Fatal("code replay")
	}
	if s.dm.Status().NativeAuthenticated {
		t.Fatal("fixture Ready")
	}
}
func TestU220FixturePrivateAuthorityStillRequired(t *testing.T) {
	for _, kind := range []string{"issuer", "tls", "origin", "peer", "owner", "epoch"} {
		t.Run(kind, func(t *testing.T) {
			s, b := authServer(t)
			configurePrivateInputFixture(s.dm)
			callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
			edit := func(r *http.Request) {
				switch kind {
				case "issuer":
					r.Header.Del("X-Main-Issuer")
				case "tls":
					r.TLS = nil
				case "origin":
					r.Header.Set("Origin", "https://music.apple.com")
				}
			}
			switch kind {
			case "owner":
				b.EngineOwnerID = "foreign"
			case "epoch":
				b.AccountEpoch++
			case "peer":
				s.privateAuth = privateAuthGuard{}
			}
			if inputCall220(s, b, "credentials", "", cred220(), edit).Code != 403 {
				t.Fatal("private authority lost")
			}
		})
	}
}
func TestU220FixtureCancelRejectsCredential(t *testing.T) {
	s, b := authServer(t)
	configurePrivateInputFixture(s.dm)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	callAuth(s, privateAuthRequest{Operation: "cancel", Binding: b}, nil)
	if inputCall220(s, b, "credentials", "", cred220(), nil).Code != 409 {
		t.Fatal("cancelled input delivered")
	}
}
func TestU220FixtureRejectsNonSyntheticInput(t *testing.T) {
	s, b := authServer(t)
	configurePrivateInputFixture(s.dm)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	p := cred220()
	p.Password = []byte("invalid-fixture-value")
	if inputCall220(s, b, "credentials", "", p, nil).Code != 409 {
		t.Fatal("fixture accepted arbitrary input")
	}
	if s.dm.Status().NativeAuthenticated {
		t.Fatal("fixture Ready")
	}
}
func TestU220FixtureOversizeAndMalformedInput(t *testing.T) {
	s, b := authServer(t)
	configurePrivateInputFixture(s.dm)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	p := cred220()
	p.Password = []byte(strings.Repeat("x", 256))
	if inputCall220(s, b, "credentials", "", p, nil).Code != 409 {
		t.Fatal("oversize accepted")
	}
}
