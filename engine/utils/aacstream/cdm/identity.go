package wv

import (
	"engine/internal/appdir"
	"log/slog"
	"os"
	"path/filepath"
)

// IdentityDir is where an operator-supplied Widevine device identity is read
// from: $ADAM_WIDEVINE_DIR, else <user config dir>/adam/widevine.
// It holds device_private_key (PEM) and device_client_id_blob (raw bytes).
func IdentityDir() string {
	if d := os.Getenv("ADAM_WIDEVINE_DIR"); d != "" {
		return d
	}
	return filepath.Join(appdir.Config(), "widevine")
}

// InitConstants loads the device identity from IdentityDir when both files are
// present and falls back to the built-in default otherwise, so existing
// installs keep working.
func InitConstants() {
	if dir := IdentityDir(); dir != "" {
		key, kerr := os.ReadFile(filepath.Join(dir, "device_private_key"))
		cid, cerr := os.ReadFile(filepath.Join(dir, "device_client_id_blob"))
		if kerr == nil && cerr == nil {
			DefaultPrivateKey, DefaultClientID = string(key), cid
			return
		}
	}
	slog.Debug("widevine: no identity in config dir, using built-in default", slog.String("dir", IdentityDir()))
	loadEmbedded()
}
