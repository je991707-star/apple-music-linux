package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// Main creates a private directory and fresh nonce for its own spawned Engine.
// The file contains no API token and is not an authority to adopt a Source.
func (s *APIServer) publishPlaybackBoot(t *tls.Config) error {
	if os.Getenv("ADAM_MAIN_PLAYBACK") != "1" {
		return nil
	}
	dir := os.Getenv("ADAM_MAIN_CONTROL_DIR")
	nonce := os.Getenv("ADAM_MAIN_BOOT_NONCE")
	canonical, e := filepath.EvalSymlinks(dir)
	if e != nil || canonical != dir || !filepath.IsAbs(dir) || len(nonce) != 64 {
		return fmt.Errorf("private playback bootstrap required")
	}
	st, e := os.Stat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return fmt.Errorf("private playback bootstrap required")
	}
	der := t.Certificates[0].Certificate[0]
	pin := sha256.Sum256(der)
	data, e := json.Marshal(map[string]any{"factory": mainSourceFactoryKind(), "pid": os.Getpid(), "port": s.port, "nonce": nonce, "boot": s.vlcGate.order.Snapshot().Boot, "pin": hex.EncodeToString(pin[:]), "certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))})
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".playback-boot-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(name, filepath.Join(dir, "playback-boot.json"))
}
