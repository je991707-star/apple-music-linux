//go:build linux && cgo && adamplay_backend && u2_auth_mock

package drm

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func setU216Runtime(c *BackendConfig, p string) {
	v := reflect.ValueOf(c).Elem().FieldByName("RuntimeLib64Dir")
	if v.IsValid() {
		v.SetString(p)
	}
}
func TestU216RuntimeOutsideAccountWorkspace(t *testing.T) {
	b, cfg := mockNative(t)
	os.RemoveAll(filepath.Join(filepath.Dir(cfg.BaseDir), "rootfs"))
	setU216Runtime(&cfg, t.TempDir())
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatalf("explicit runtime directory ignored: %v", err)
	}
	if nativeCallCount(t, cfg) != 1 {
		t.Fatal("configured runtime must enter the mock Native call")
	}
}
func TestU216InvalidExplicitRuntimeDoesNotFallBack(t *testing.T) {
	b, cfg := mockNative(t)
	setU216Runtime(&cfg, "relative-runtime")
	if err := b.Start(context.Background(), cfg); err == nil {
		t.Fatal("invalid explicit runtime silently fell back to account sibling")
	}
}
