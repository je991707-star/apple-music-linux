package wv

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCertData(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("CERT")) }))
	defer ok.Close()
	if b, err := GetCertData(ok.Client(), ok.URL); err != nil || string(b) != "CERT" {
		t.Fatalf("got %q err=%v", b, err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<html>forbidden</html>", http.StatusForbidden)
	}))
	defer bad.Close()
	if b, err := GetCertData(bad.Client(), bad.URL); err == nil {
		t.Fatalf("an HTTP 403 must be an error, got body %q", b)
	}
}
