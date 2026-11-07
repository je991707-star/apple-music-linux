package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestU216ScopedConfig(t *testing.T) {
	old := Config
	t.Cleanup(func() { Config = old })
	t.Setenv("ADAM_MAIN_PLAYBACK", "1")
	t.Chdir(t.TempDir())
	for _, body := range []string{"drm-base-dir: ../old-account\n", "storefront: us\n", "drm-base-dir: /work/files\ndrm-runtime-lib64-dir: ../old-runtime\n"} {
		if err := os.WriteFile("config.yaml", []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := loadConfig(); err == nil {
			t.Errorf("invalid scoped config accepted: %q", body)
		}
	}
	body := "drm-base-dir: /work/files\ndrm-runtime-lib64-dir: /assets/lib64\n"
	os.WriteFile("config.yaml", []byte(body), 0600)
	if err := loadConfig(); err != nil {
		t.Fatal(err)
	}
	field := reflect.ValueOf(Config).FieldByName("DRMRuntimeLib64Dir")
	if !field.IsValid() || field.String() != filepath.Clean("/assets/lib64") {
		t.Fatal("explicit runtime directory was dropped by config loader")
	}
}
