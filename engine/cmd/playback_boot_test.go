package main

import (
	"crypto/x509"
	"testing"
)

func TestMainPlaybackTLSCertificate(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ADAM_MAIN_PLAYBACK", mode)
			cfg, e := loopbackTLSConfig()
			if e != nil {
				t.Fatal(e)
			}
			cert, e := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
			if e != nil {
				t.Fatal(e)
			}
			if cert.IsCA != (mode == "1") {
				t.Fatal("private Main CA mode mismatch")
			}
			if mode == "1" && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
				t.Fatal("Main CA is not accepted as a signing-capable trust anchor")
			}
		})
	}
}
