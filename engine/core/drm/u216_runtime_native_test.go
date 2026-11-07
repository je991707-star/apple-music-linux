//go:build linux && cgo && adamplay_backend && u2_native_offline && !u2_auth_mock

package drm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type u216NoCredentials struct{ calls atomic.Int32 }

func (a *u216NoCredentials) Challenge(context.Context, AuthChallenge) (string, error) {
	a.calls.Add(1)
	return "", errors.New("offline credentials unavailable")
}
func TestU216AnonymousRealNativeLoader(t *testing.T) {
	if os.Getenv("U216_RESTRICTED_NATIVE_TEST") != "anonymous-no-account" {
		t.Fatal("restricted offline harness required")
	}
	for _, p := range []string{"/home/h97m", "/var/home/h97m"} {
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Fatal("host home must be absent")
		}
	}
	dir := filepath.Join(t.TempDir(), "files")
	b := AdamplayBackend("/runtime/bin").(*adamplayBackend)
	a := new(u216NoCredentials)
	b.SetAuthSource(a)
	cfg := BackendConfig{BaseDir: dir}
	field := reflect.ValueOf(&cfg).Elem().FieldByName("RuntimeLib64Dir")
	if field.IsValid() {
		field.SetString("/assets/lib64")
	}
	if e := b.Start(context.Background(), cfg); e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	done := b.hybrisInitDone
	b.mu.Unlock()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("Native loader did not finish; process termination required")
	}
	b.mu.Lock()
	ready := b.hybrisReady
	b.mu.Unlock()
	if ready || a.calls.Load() != 0 {
		t.Fatal("anonymous bootstrap must not authenticate or request credentials")
	}
	if _, e := os.Stat(filepath.Join(dir, "session-ready")); !os.IsNotExist(e) {
		t.Fatal("anonymous init must not publish session readiness")
	}
	if _, e := b.GetAccount(context.Background()); e == nil {
		t.Fatal("empty explicit C BaseDir must not produce account data")
	}
	if e := b.Stop(); e != nil {
		t.Fatal(e)
	}
	t.Log("real Go/C loader returned; no credentials, no session marker, no authentication; Vendor RequestContext intentionally not entered")
}
