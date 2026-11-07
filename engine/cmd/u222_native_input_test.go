//go:build linux && cgo && u2_native_credential_fixture

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"engine/core/drm"
	"net/http/httptest"
	"os"
	"testing"
)

func nativeCall222(s *APIServer, b drm.AuthBinding, kind, id string) *httptest.ResponseRecorder {
	input := map[string][]byte{"email": []byte("u220-user@example.invalid"), "password": []byte("u220-synthetic-native")}
	if kind == "challenge" {
		input = map[string][]byte{"reply": []byte("000001")}
	}
	raw, _ := json.Marshal(map[string]any{"operation": kind, "binding": b, "challengeId": id, "input": input})
	defer clear(raw)
	r := httptest.NewRequest("POST", "https://127.0.0.1/api/v1/main/auth", bytes.NewReader(raw))
	r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	r.Header.Set("X-Api-Token", os.Getenv("ADAM_API_TOKEN"))
	r.Header.Set("X-Main-Issuer", os.Getenv("ADAM_MAIN_SOURCE_ISSUER"))
	w := httptest.NewRecorder()
	s.handlePrivateAuth(w, r)
	return w
}
func TestU222ActualHandlerNativeBoundary(t *testing.T) {
	s, b := authServer(t)
	s.dm.EnableControlledNativeCredentialConsumer(1)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	w := nativeCall222(s, b, "credentials", "")
	if w.Code != 200 {
		t.Fatal("credential did not reach Native boundary", w.Code)
	}
	var out struct {
		drm.PrivateInputStatus
		NativeAuthenticated bool
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Challenge == nil || out.NativeAuthenticated || s.dm.Status().NativeAuthenticated {
		t.Fatal("unsafe Native challenge")
	}
	if nativeCall222(s, b, "challenge", "wrong").Code != 409 {
		t.Fatal("wrong challenge")
	}
	if nativeCall222(s, b, "challenge", out.Challenge.ID).Code != 200 {
		t.Fatal("challenge did not reach C")
	}
	if nativeCall222(s, b, "challenge", out.Challenge.ID).Code != 409 {
		t.Fatal("challenge replay")
	}
	if s.dm.Status().NativeAuthenticated {
		t.Fatal("consumer authorized")
	}
	callAuth(s, privateAuthRequest{Operation: "cancel", Binding: b}, nil)
	if nativeCall222(s, b, "credentials", "").Code != 409 {
		t.Fatal("cancel reopened input")
	}
}
func TestU222ActualHandlerNoDefaultNativeProvider(t *testing.T) {
	s, b := authServer(t)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	if nativeCall222(s, b, "credentials", "").Code != 503 {
		t.Fatal("default Native provider opened")
	}
}

func TestU222ProductionSourceRemainsUnavailable(t *testing.T) {
	if mainSourceFactoryKind() != "unavailable" {
		t.Fatal("Production Source unexpectedly qualified")
	}
	session, err := openMainTypedSource(context.Background(), nil, "u222-synthetic-content")
	if session != nil || err == nil {
		t.Fatal("unqualified account produced source")
	}
}

func TestU223ActualHandlerResponseBridge(t *testing.T) {
	s, b := authServer(t)
	s.dm.EnableControlledNativeCredentialConsumer(11)
	callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil)
	w := nativeCall222(s, b, "credentials", "")
	if w.Code != 200 {
		t.Fatal("credential did not reach Native boundary", w.Code)
	}
	var out struct {
		drm.PrivateInputStatus
		NativeAuthenticated bool
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Challenge == nil || out.NativeAuthenticated || s.dm.Status().NativeAuthenticated {
		t.Fatal("unsafe Native challenge")
	}
	if nativeCall222(s, b, "challenge", "wrong").Code != 409 {
		t.Fatal("wrong challenge")
	}
	if nativeCall222(s, b, "challenge", out.Challenge.ID).Code != 200 {
		t.Fatal("challenge did not reach C")
	}
	if nativeCall222(s, b, "challenge", out.Challenge.ID).Code != 409 {
		t.Fatal("challenge replay")
	}
	if s.dm.Status().NativeAuthenticated {
		t.Fatal("consumer authorized")
	}
	callAuth(s, privateAuthRequest{Operation: "cancel", Binding: b}, nil)
	if nativeCall222(s, b, "credentials", "").Code != 409 {
		t.Fatal("cancel reopened input")
	}
}
