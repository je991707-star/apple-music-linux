package wv

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"engine/internal/appdir"
	"google.golang.org/protobuf/proto"
)

// configDir is the app's config directory; a variable so tests can point it at a temp dir.
var configDir = appdir.Config

// IdentityDir is where the per-device Widevine identity is stored:
// $ADAM_ADAMPLAY_DIR, else <user config dir>/adam/adamplay.
// It holds device_private_key (PEM) and device_client_id_blob (raw bytes).
func IdentityDir() string {
	if d := os.Getenv("ADAM_ADAMPLAY_DIR"); d != "" {
		return d
	}
	return filepath.Join(configDir(), "adamplay")
}

var initMu sync.Mutex // concurrent callers must not each generate or migrate an identity

// validKey reports whether pemKey is a parseable RSA private key (PKCS#1 or PKCS#8).
func validKey(pemKey string) bool {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return false
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return true
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	_, isRSA := k.(*rsa.PrivateKey)
	return err == nil && isRSA
}

// readIdentity returns the key and client-ID blob stored in dir. Both files must exist and the key
// must parse: a truncated or corrupt file would otherwise be used as-is and fail every license
// request, with the working fallbacks never consulted.
func readIdentity(dir string) (key string, cid []byte, ok bool) {
	k, kerr := os.ReadFile(filepath.Join(dir, "device_private_key"))
	c, cerr := os.ReadFile(filepath.Join(dir, "device_client_id_blob"))
	if kerr != nil || cerr != nil || len(c) == 0 || !validKey(string(k)) {
		if kerr == nil && cerr == nil {
			slog.Warn("adamplay: ignoring unreadable device identity", slog.String("dir", dir))
		}
		return "", nil, false
	}
	return string(k), c, true
}

// persistIdentity stores an identity atomically. The client ID goes first and the key last,
// so the pair only counts as present once the key exists.
func persistIdentity(dir, key string, cid []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "device_client_id_blob"), cid, 0o600); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "device_private_key"), []byte(key), 0o600)
}

// legacyIdentityDirs are where earlier versions kept a working device identity.
func legacyIdentityDirs() []string {
	cfg := configDir()
	return []string{
		filepath.Join(cfg, "widevine"),
		filepath.Join(filepath.Dir(cfg), "musickit-sdk-linux", "widevine"),
	}
}

// InitConstants selects the device identity, in order of preference:
//  1. the identity in IdentityDir,
//  2. one left by an earlier version (copied into IdentityDir so it is kept),
//  3. a freshly generated identity, only when ADAM_GENERATE_IDENTITY=1,
//  4. the built-in default.
//
// Generation is opt-in because a self-made identity has no certificate chain and the
// license server rejects it (code -42585); real device identities are accepted.
func InitConstants() {
	initMu.Lock()
	defer initMu.Unlock()

	dir := IdentityDir()
	if key, cid, ok := readIdentity(dir); ok {
		DefaultPrivateKey, DefaultClientID = key, cid
		slog.Debug("adamplay: loaded existing device identity", slog.String("dir", dir))
		return
	}

	if os.Getenv("ADAM_ADAMPLAY_DIR") == "" { // an explicit directory is never second-guessed
		for _, old := range legacyIdentityDirs() {
			key, cid, ok := readIdentity(old)
			if !ok {
				continue
			}
			if err := persistIdentity(dir, key, cid); err != nil {
				slog.Warn("adamplay: cannot copy legacy identity, using it in place",
					slog.String("from", old), slog.String("err", err.Error()))
			} else {
				slog.Info("adamplay: migrated device identity", slog.String("from", old), slog.String("to", dir))
			}
			DefaultPrivateKey, DefaultClientID = key, cid
			return
		}
	}

	if os.Getenv("ADAM_GENERATE_IDENTITY") == "1" {
		key, cid, err := generateIdentity()
		if err == nil {
			err = persistIdentity(dir, key, cid)
		}
		if err == nil {
			DefaultPrivateKey, DefaultClientID = key, cid
			slog.Info("adamplay: generated new per-device identity", slog.String("dir", dir))
			return
		}
		slog.Warn("adamplay: identity generation failed, using built-in default", slog.String("err", err.Error()))
	}
	loadEmbedded()
}

// generateIdentity creates a fresh RSA-2048 key pair and a Widevine L3
// ClientIdentification blob populated with OS-derived device information.
// No external provisioning server is required.
func generateIdentity() (privateKeyPEM string, clientIDBlob []byte, err error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", nil, fmt.Errorf("keygen: %w", err)
	}

	privateKeyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}))

	// PKCS1 DER-encoded public key — what Widevine's DeviceCertificate.PublicKey expects.
	pubKeyDER := x509.MarshalPKCS1PublicKey(&privKey.PublicKey)

	// Random serial number (16 bytes, crypto/rand).
	serial := make([]byte, 16)
	if _, err := rand.Read(serial); err != nil {
		return "", nil, fmt.Errorf("serial: %w", err)
	}

	certType := DeviceCertificate_USER_DEVICE
	sysID := uint32(0x1000)
	creationTime := uint32(time.Now().Unix())
	devCert := &DeviceCertificate{
		Type:                &certType,
		SerialNumber:        serial,
		CreationTimeSeconds: &creationTime,
		PublicKey:           pubKeyDER,
		SystemId:            &sysID,
	}
	certBytes, err := proto.Marshal(devCert)
	if err != nil {
		return "", nil, fmt.Errorf("marshal device cert: %w", err)
	}

	// Self-sign the DeviceCertificate with our own RSA key (PSS-SHA256).
	// Apple's license server does not verify the cert chain (evidenced by accepting
	// the emulator identity), so self-signing suffices for L3 software DRM.
	h := sha256.Sum256(certBytes)
	sig, err := rsa.SignPSS(rand.Reader, privKey, crypto.SHA256, h[:],
		&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		return "", nil, fmt.Errorf("sign cert: %w", err)
	}

	signedCert := &SignedDeviceCertificate{
		XDeviceCertificate: devCert,
		Signature:          sig,
	}

	cidType := ClientIdentification_DEVICE_CERTIFICATE
	cid := &ClientIdentification{
		Type:       &cidType,
		Token:      signedCert,
		ClientInfo: buildClientInfo(),
	}

	blob, err := proto.Marshal(cid)
	if err != nil {
		return "", nil, fmt.Errorf("marshal client ID: %w", err)
	}
	return privateKeyPEM, blob, nil
}

// buildClientInfo returns OS-derived ClientInfo key-value pairs for the
// Widevine L3 ClientIdentification message.
func buildClientInfo() []*ClientIdentification_NameValue {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "device"
	}
	// Sanitize hostname: keep only alphanumeric and hyphen characters.
	sanitized := make([]byte, 0, len(hostname))
	for _, b := range []byte(hostname) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' {
			sanitized = append(sanitized, b)
		}
	}
	if len(sanitized) == 0 {
		sanitized = []byte("device")
	}
	deviceName := string(sanitized)

	// Normalize GOARCH to Widevine naming conventions.
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "x86"
	}
	platform := runtime.GOOS
	productName := "adam-" + platform
	buildInfo := fmt.Sprintf("adam/%s/%s/%s:1/ADAM.1/1:release/release-keys",
		platform, arch, deviceName)

	kv := func(k, v string) *ClientIdentification_NameValue {
		name, value := k, v
		return &ClientIdentification_NameValue{Name: &name, Value: &value}
	}
	return []*ClientIdentification_NameValue{
		kv("company_name", "Adam"),
		kv("model_name", platform+" "+arch),
		kv("architecture_name", arch),
		kv("device_name", deviceName),
		kv("product_name", productName),
		kv("build_info", buildInfo),
		kv("widevine_cdm_version", "14.0.0"),
		kv("oem_crypto_security_patch_level", "0"),
	}
}

// writeFileAtomic writes data to a temp file in the same directory, syncs it,
// then renames it over path, so readers see the old or the new file, never half.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error { tmp.Close(); os.Remove(name); return err }
	if err := tmp.Chmod(perm); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
