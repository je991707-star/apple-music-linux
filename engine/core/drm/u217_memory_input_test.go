//go:build linux && cgo && adamplay_backend && u2_auth_mock

package drm

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestU217RejectUnsafeCStringInputs(t *testing.T) {
	for _, name := range []string{"password-long", "username-long", "password-nul", "username-nul", "device-nul"} {
		t.Run(name, func(t *testing.T) {
			b, cfg := mockNative(t)
			switch name {
			case "password-long":
				cfg.Credentials.Password = strings.Repeat("x", 256)
			case "username-long":
				cfg.Credentials.Email = strings.Repeat("u", 257)
			case "password-nul":
				cfg.Credentials.Password = "mock-ok\x00suffix"
			case "username-nul":
				cfg.Credentials.Email = "user\x00suffix"
			case "device-nul":
				cfg.DeviceInfo = "device\x00suffix"
			}
			if err := b.Start(context.Background(), cfg); err == nil {
				t.Fatal("unsafe C-string input accepted")
			}
			b.mu.Lock()
			ready := b.hybrisReady
			b.mu.Unlock()
			if ready {
				t.Fatal("rejected input published authentication readiness")
			}
		})
	}
}
func TestU217ChallengeLengthAndNUL(t *testing.T) {
	for _, value := range []string{"123456", "000000", "1234567", "12\x00456", " 123456", "１２３４５６"} {
		t.Run("shape", func(t *testing.T) {
			b, cfg := mockNative(t)
			b.SetAuthSource(u213Challenge(func(context.Context, AuthChallenge) (string, error) { return value, nil }))
			if err := b.Start(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			got := b.answerChallenge("2fa")
			valid := value == "123456" || value == "000000"
			if (got != "") != valid {
				t.Fatal("challenge input was accepted/truncated unexpectedly")
			}
		})
	}
}
func TestU217ChallengeErrorDoesNotReturnCredential(t *testing.T) {
	b, cfg := mockNative(t)
	b.SetAuthSource(u213Challenge(func(context.Context, AuthChallenge) (string, error) {
		return "123456", errors.New("synthetic-input-error")
	}))
	if err := b.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	got := b.answerChallenge("2fa")
	log.SetOutput(previous)
	if got != "" {
		t.Fatal("failed challenge returned credential")
	}
	if strings.Contains(buf.String(), "synthetic-input-error") {
		t.Fatal("challenge error detail leaked to logs")
	}
}

func TestU217BridgeNeverTruncatesReply(t *testing.T) {
	for _, n := range []int{0, 1, 6, 7, 16} {
		if got := authReplyFits("123456", n); got != (n >= 7) {
			t.Fatal("bridge capacity permits a truncated code")
		}
	}
	if authReplyFits("12\x00456", 16) {
		t.Fatal("bridge allowed embedded NUL")
	}
}
