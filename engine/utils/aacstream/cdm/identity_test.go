package wv

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestInitConstantsPrefersConfiguredIdentity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADAM_ADAMPLAY_DIR", dir)
	InitConstants()
	if len(DefaultPrivateKey) < 100 {
		t.Fatal("expected built-in fallback when dir is empty")
	}
	os.WriteFile(filepath.Join(dir, "device_private_key"), []byte(testKeyPEM(t)), 0o600)
	os.WriteFile(filepath.Join(dir, "device_client_id_blob"), []byte{1, 2}, 0o600)
	InitConstants()
	if DefaultPrivateKey != testKeyPEM(t) || len(DefaultClientID) != 2 {
		t.Fatal("configured identity not used")
	}
}

// TestGenerateIdentityProducesValidStructures verifies that generateIdentity returns
// a parseable RSA private key and a well-formed Widevine ClientIdentification blob.
func TestGenerateIdentityProducesValidStructures(t *testing.T) {
	keyPEM, blob, err := generateIdentity()
	if err != nil {
		t.Fatalf("generateIdentity: %v", err)
	}

	// Private key must be a valid RSA-2048 PKCS1 PEM.
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatal("private key PEM invalid")
	}
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	if privKey.N.BitLen() != 2048 {
		t.Fatalf("expected 2048-bit key, got %d", privKey.N.BitLen())
	}

	// Client ID blob must unmarshal to a valid ClientIdentification.
	var cid ClientIdentification
	if err := proto.Unmarshal(blob, &cid); err != nil {
		t.Fatalf("unmarshal ClientIdentification: %v", err)
	}
	if cid.GetType() != ClientIdentification_DEVICE_CERTIFICATE {
		t.Fatalf("expected DEVICE_CERTIFICATE, got %v", cid.GetType())
	}

	cert := cid.GetToken()
	if cert == nil || cert.GetXDeviceCertificate() == nil {
		t.Fatal("Token/DeviceCertificate is nil")
	}
	dc := cert.GetXDeviceCertificate()
	if dc.GetType() != DeviceCertificate_USER_DEVICE {
		t.Fatalf("expected USER_DEVICE, got %v", dc.GetType())
	}

	// Public key in the cert must match our generated private key.
	pubKeyDER := x509.MarshalPKCS1PublicKey(&privKey.PublicKey)
	if string(dc.GetPublicKey()) != string(pubKeyDER) {
		t.Fatal("public key in cert does not match generated private key")
	}

	// ClientInfo must contain at least the required fields.
	required := map[string]bool{
		"company_name":         false,
		"model_name":           false,
		"architecture_name":    false,
		"device_name":          false,
		"product_name":         false,
		"build_info":           false,
		"widevine_cdm_version": false,
	}
	for _, kv := range cid.GetClientInfo() {
		if _, ok := required[kv.GetName()]; ok {
			required[kv.GetName()] = true
		}
	}
	for k, found := range required {
		if !found {
			t.Errorf("ClientInfo missing %q", k)
		}
	}
}

// TestGenerateIdentityTwoCallsProduceDifferentKeys verifies that each call generates
// a distinct key pair (crypto/rand seeded, not deterministic).
func TestGenerateIdentityTwoCallsProduceDifferentKeys(t *testing.T) {
	key1, _, err := generateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	key2, _, err := generateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if key1 == key2 {
		t.Fatal("two generateIdentity calls produced identical keys")
	}
}

// TestInitConstantsGeneratesAndPersistsOnFirstRun verifies that when no identity
// files exist, InitConstants generates and saves them.
func TestInitConstantsGeneratesAndPersistsOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADAM_ADAMPLAY_DIR", dir)
	t.Setenv("ADAM_GENERATE_IDENTITY", "1") // generation is opt-in: Apple rejects self-made identities

	InitConstants()

	keyPath := filepath.Join(dir, "device_private_key")
	cidPath := filepath.Join(dir, "device_client_id_blob")

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("device_private_key not written: %v", err)
	}
	cidBytes, err := os.ReadFile(cidPath)
	if err != nil {
		t.Fatalf("device_client_id_blob not written: %v", err)
	}

	if !strings.Contains(string(keyBytes), "RSA PRIVATE KEY") {
		t.Fatal("persisted key is not PEM")
	}
	if len(cidBytes) < 32 {
		t.Fatal("persisted client ID blob too short")
	}

	// Global vars must be set to the generated identity, not the built-in default.
	if DefaultPrivateKey != string(keyBytes) {
		t.Fatal("DefaultPrivateKey not set to generated key")
	}
	if string(DefaultClientID) != string(cidBytes) {
		t.Fatal("DefaultClientID not set to generated blob")
	}
}

// TestInitConstantsLoadsPersistedIdentityOnSecondRun verifies that a second
// InitConstants call loads what was written by the first.
func TestInitConstantsLoadsPersistedIdentityOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADAM_ADAMPLAY_DIR", dir)

	InitConstants()
	firstKey := DefaultPrivateKey
	firstCID := string(DefaultClientID)

	// Reset globals so second call must reload from disk.
	DefaultPrivateKey = ""
	DefaultClientID = nil

	InitConstants()

	if DefaultPrivateKey != firstKey {
		t.Fatal("second InitConstants did not reload persisted private key")
	}
	if string(DefaultClientID) != firstCID {
		t.Fatal("second InitConstants did not reload persisted client ID blob")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k")
	if err := writeFileAtomic(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "two" {
		t.Fatalf("got %q, want replaced content", got)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "k"), nil, 0o600); err == nil {
		t.Fatal("expected error for unwritable directory")
	}
}

func TestInitConstantsMigratesLegacyIdentityAndDoesNotGenerate(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "adam")
	oldCfg := configDir
	configDir = func() string { return cfg }
	defer func() { configDir = oldCfg }()
	t.Setenv("ADAM_ADAMPLAY_DIR", "")
	t.Setenv("ADAM_GENERATE_IDENTITY", "")
	legacy := filepath.Join(root, "musickit-sdk-linux", "widevine")
	os.MkdirAll(legacy, 0o700)
	os.WriteFile(filepath.Join(legacy, "device_private_key"), []byte(testKeyPEM(t)), 0o600)
	os.WriteFile(filepath.Join(legacy, "device_client_id_blob"), []byte{9, 9, 9}, 0o600)

	InitConstants()
	if DefaultPrivateKey != testKeyPEM(t) || len(DefaultClientID) != 3 {
		t.Fatalf("legacy identity not used: key=%q", DefaultPrivateKey)
	}
	if k, c, ok := readIdentity(IdentityDir()); !ok || k != testKeyPEM(t) || len(c) != 3 {
		t.Fatal("legacy identity was not copied into the identity dir")
	}

	// With nothing anywhere, the built-in default is used and nothing is written.
	empty := filepath.Join(t.TempDir(), "adam")
	configDir = func() string { return empty }
	InitConstants()
	if DefaultPrivateKey == testKeyPEM(t) || len(DefaultPrivateKey) < 100 {
		t.Fatal("expected the built-in default")
	}
	if _, _, ok := readIdentity(IdentityDir()); ok {
		t.Fatal("an identity must not be generated unless explicitly enabled")
	}
}

func TestInitConstantsConcurrentCallersAgree(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADAM_ADAMPLAY_DIR", dir)
	t.Setenv("ADAM_GENERATE_IDENTITY", "1")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); InitConstants() }()
	}
	wg.Wait()
	key, cid, ok := readIdentity(dir)
	if !ok || key != DefaultPrivateKey || string(cid) != string(DefaultClientID) {
		t.Fatal("stored identity does not match the one in use: callers raced")
	}
}

var cachedTestKey string

// testKeyPEM returns one valid RSA key for the whole test run (generation is slow).
func testKeyPEM(t *testing.T) string {
	t.Helper()
	if cachedTestKey == "" {
		k, _, err := generateIdentity()
		if err != nil {
			t.Fatal(err)
		}
		cachedTestKey = k
	}
	return cachedTestKey
}

func TestCorruptIdentityFallsThroughToTheNextSource(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "adam")
	oldCfg := configDir
	configDir = func() string { return cfg }
	defer func() { configDir = oldCfg }()
	t.Setenv("ADAM_ADAMPLAY_DIR", "")
	t.Setenv("ADAM_GENERATE_IDENTITY", "")

	// A truncated key in the live dir, a good one in a legacy dir.
	live := filepath.Join(cfg, "adamplay")
	os.MkdirAll(live, 0o700)
	os.WriteFile(filepath.Join(live, "device_private_key"), []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIE"), 0o600)
	os.WriteFile(filepath.Join(live, "device_client_id_blob"), []byte{1, 2, 3}, 0o600)
	legacy := filepath.Join(root, "musickit-sdk-linux", "widevine")
	os.MkdirAll(legacy, 0o700)
	os.WriteFile(filepath.Join(legacy, "device_private_key"), []byte(testKeyPEM(t)), 0o600)
	os.WriteFile(filepath.Join(legacy, "device_client_id_blob"), []byte{4, 5}, 0o600)

	InitConstants()
	if DefaultPrivateKey != testKeyPEM(t) || len(DefaultClientID) != 2 {
		t.Fatal("a corrupt identity must be skipped in favour of the legacy one")
	}
	if k, _, ok := readIdentity(live); !ok || k != testKeyPEM(t) {
		t.Fatal("the good identity must replace the corrupt one on disk")
	}
}

func TestValidKey(t *testing.T) {
	if !validKey(testKeyPEM(t)) {
		t.Error("a generated key must validate")
	}
	for _, bad := range []string{"", "KEY", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----"} {
		if validKey(bad) {
			t.Errorf("%q must not validate", bad)
		}
	}
}
