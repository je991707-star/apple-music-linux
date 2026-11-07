package drm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Internal capability seam. No HTTP/Renderer/env installer exists.
type nativeCredentialConsumerProvider interface {
	nativeCredentialConsumer() (privateInputConsumer, func())
}
type nativeCredentialEndpoint interface {
	available() bool
	bind([32]byte, uint64, int64) error
	submit([32]byte, uint64, string, string, *PrivateInput) (bool, error)
	cancel()
}

var errNativeCredentialBoundary = errors.New("native credential boundary rejected")

type nativeCredentialAdapter struct {
	mu            sync.Mutex
	endpoint      nativeCredentialEndpoint
	bound         *AuthBinding
	tag           [32]byte
	revoked       bool
	nextChallenge string
}

func newNativeCredentialAdapter(e nativeCredentialEndpoint) *nativeCredentialAdapter {
	return &nativeCredentialAdapter{endpoint: e}
}
func (a *nativeCredentialAdapter) consumer() (privateInputConsumer, func()) {
	if a.endpoint == nil || !a.endpoint.available() {
		return nil, nil
	}
	return a.consume, a.revoke
}
func (a *nativeCredentialAdapter) revoke() {
	a.mu.Lock()
	a.revoked = true
	a.mu.Unlock()
	a.endpoint.cancel()
}
func (a *nativeCredentialAdapter) consume(ctx context.Context, b AuthBinding, kind string, p *PrivateInput) (*PrivateInputChallenge, error) {
	if ctx.Err() != nil || !validAuthBinding(b) || !p.valid(kind) {
		return nil, errNativeCredentialBoundary
	}
	a.mu.Lock()
	if a.revoked || (a.bound != nil && *a.bound != b) {
		a.mu.Unlock()
		return nil, errNativeCredentialBoundary
	}
	if a.bound == nil {
		raw, _ := json.Marshal(b)
		a.tag = sha256.Sum256(raw)
		clear(raw)
		copy := b
		a.bound = &copy
		if e := a.endpoint.bind(a.tag, b.Generation, b.ExpiresAt); e != nil {
			a.revoked = true
			a.mu.Unlock()
			return nil, errNativeCredentialBoundary
		}
	}
	tag := a.tag
	a.mu.Unlock()
	stop := context.AfterFunc(ctx, a.revoke)
	defer stop()
	if ctx.Err() != nil {
		a.revoke()
		return nil, errNativeCredentialBoundary
	}
	var id [16]byte
	if _, e := rand.Read(id[:]); e != nil {
		a.revoke()
		return nil, errNativeCredentialBoundary
	}
	challengeID := hex.EncodeToString(id[:])
	// Manager checks the incoming challenge ID. The Native slot independently checks
	// its expected one; no request may choose a new Native scope.
	if kind == "challenge" {
		a.mu.Lock()
		challengeID = a.challengeID()
		a.mu.Unlock()
	}
	next, e := a.endpoint.submit(tag, b.Generation, kind, challengeID, p)
	a.mu.Lock()
	defer a.mu.Unlock()
	if e != nil || a.revoked || ctx.Err() != nil || time.Now().UnixMilli() >= b.ExpiresAt {
		a.revoked = true
		a.endpoint.cancel()
		return nil, errNativeCredentialBoundary
	}
	if next {
		a.nextChallenge = challengeID
		return &PrivateInputChallenge{ID: challengeID, Type: "two_factor", ExpiresAt: b.ExpiresAt}, nil
	}
	a.nextChallenge = ""
	return nil, nil // consumption is NEVER completion
}
func (a *nativeCredentialAdapter) challengeID() string { return a.nextChallenge }

func (a *nativeCredentialAdapter) shutdown() {
	a.mu.Lock()
	a.revoked = true
	a.mu.Unlock()
	if e, ok := a.endpoint.(interface{ shutdown() }); ok {
		e.shutdown()
	} else {
		a.endpoint.cancel()
	}
}
