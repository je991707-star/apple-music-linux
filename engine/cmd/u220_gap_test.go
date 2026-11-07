package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestU220ClosedConsumerIsAnExplicitBoundary(t *testing.T) {
	s, b := authServer(t)
	if callAuth(s, privateAuthRequest{Operation: "begin", Binding: b}, nil).Code != 202 {
		t.Fatal("begin rejected")
	}
	data, _ := json.Marshal(map[string]any{"operation": "credentials", "binding": b, "input": map[string][]byte{"email": []byte("u220-user@example.invalid"), "password": []byte("u220-synthetic-only")}})
	r := httptest.NewRequest("POST", "https://127.0.0.1/api/v1/main/auth", bytes.NewReader(data))
	r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	r.Header.Set("X-Api-Token", os.Getenv("ADAM_API_TOKEN"))
	r.Header.Set("X-Main-Issuer", os.Getenv("ADAM_MAIN_SOURCE_ISSUER"))
	out := httptest.NewRecorder()
	s.handlePrivateAuth(out, r)
	if out.Code != 503 {
		t.Fatalf("missing explicit closed consumer boundary: status=%d", out.Code)
	}
}
