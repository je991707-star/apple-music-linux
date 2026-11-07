package aacstream

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestAfterRequestRejectsErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(PlaybackLicense{
			ErrorCode: 42,
			License:   base64.StdEncoding.EncodeToString(make([]byte, 64)),
		})
	}))
	defer srv.Close()

	resp, err := resty.New().R().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AfterRequest(resp); err == nil {
		t.Fatal("AfterRequest accepted a response with non-zero ErrorCode")
	}
}

func TestAfterRequestAcceptsValidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(PlaybackLicense{
			License: base64.StdEncoding.EncodeToString(make([]byte, 64)),
		})
	}))
	defer srv.Close()

	resp, err := resty.New().R().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	license, iv, err := AfterRequest(resp)
	if err != nil {
		t.Fatalf("AfterRequest returned unexpected error: %v", err)
	}
	if len(license) == 0 {
		t.Fatal("AfterRequest returned empty license")
	}
	if len(iv) != 16 {
		t.Fatalf("AfterRequest returned IV of wrong length: %d", len(iv))
	}
}
