//go:build u2_credential_fixture

package drm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// EnableSyntheticCredentialConsumer is absent from production builds.
func (m *DRMManager) EnableSyntheticCredentialConsumer() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.privateInputConsumer = func(ctx context.Context, b AuthBinding, kind string, p *PrivateInput) (*PrivateInputChallenge, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if kind == "challenge" {
			return nil, nil
		}
		if !bytes.HasSuffix(p.Email, []byte("@example.invalid")) || !bytes.HasPrefix(p.Email, []byte("u220-")) || !bytes.HasPrefix(p.Password, []byte("u220-synthetic-")) {
			return nil, errors.New("synthetic input required")
		}
		var id [16]byte
		if _, e := rand.Read(id[:]); e != nil {
			return nil, errors.New("synthetic challenge unavailable")
		}
		return &PrivateInputChallenge{ID: hex.EncodeToString(id[:]), Type: "two_factor", ExpiresAt: min(b.ExpiresAt, time.Now().Add(time.Minute).UnixMilli())}, nil
	}
}
