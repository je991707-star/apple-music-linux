package drm

import (
	"context"
	"errors"
	"strings"
	"time"
)

// AuthBinding is a non-secret correlation scope, not a grant or Vendor account proof.
// A production Main-to-Engine binding channel is deliberately not opened here.
type AuthBinding struct {
	AccountID          string `json:"accountId"`
	AccountEpoch       uint64 `json:"accountEpoch"`
	AttemptID          string `json:"attemptId"`
	EngineOwnerID      string `json:"engineOwnerId"`
	EngineBoot         string `json:"engineBoot"`
	BootNonce          string `json:"bootNonce"`
	CredentialRevision uint64 `json:"credentialRevision"`
	Generation         uint64 `json:"generation"`
	ExpiresAt          int64  `json:"expiresAt"`
}

// NativeEvidence names a verified receipt, never credentials, tokens or raw SDK data.
// Its shape/ID alone proves nothing: the production verifier is absent.
type NativeEvidence struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

var errNativeUnqualified = errors.New("native completion is unqualified")

func validAuthBinding(b AuthBinding) bool {
	const safe = uint64(9007199254740991)
	for _, s := range []string{b.AccountID, b.AttemptID, b.EngineOwnerID} {
		if len(s) == 0 || len(s) > 128 || strings.ContainsRune(s, 0) {
			return false
		}
	}
	for _, n := range []uint64{b.AccountEpoch, b.CredentialRevision, b.Generation} {
		if n == 0 || n > safe {
			return false
		}
	}
	for _, x := range []struct {
		s string
		n int
	}{{b.EngineBoot, 32}, {b.BootNonce, 64}} {
		if len(x.s) != x.n || strings.Trim(x.s, "0123456789abcdef") != "" {
			return false
		}
	}
	return b.ExpiresAt > time.Now().UnixMilli() && b.ExpiresAt <= time.Now().Add(10*time.Minute).UnixMilli()
}
func terminalAuth(a AuthState) bool {
	return a == AuthCancelled || a == AuthExpired || a == AuthRevoked || a == AuthFailed
}

// beginTrustedAttempt is engine-internal. No HTTP/IPC endpoint calls it; a future
// qualified Main channel must supply/bind identity before production use.
func (m *DRMManager) beginTrustedAttempt(b AuthBinding) error {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.gateMu.Lock()
	defer m.gateMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.RequireTrustedCompletion || m.closed.Load() || m.gate != gateOpen || m.authBinding != nil || !validAuthBinding(b) {
		return errNativeUnqualified
	}
	m.authBinding = &b
	m.snapshot.State.Authentication = AuthAccepted
	m.normalizeTrustedLocked()
	return nil
}

// transitionTrusted is the completion seam. Only _test.go installs a verifier.
// The verifier runs without locks; all identities/revocations are checked again afterward.
func (m *DRMManager) transitionTrusted(ctx context.Context, b AuthBinding, next AuthState, e NativeEvidence) error {
	if ctx == nil {
		return errNativeUnqualified
	}
	m.mu.Lock()
	matches := !m.completionConsumed && m.authBinding != nil && *m.authBinding == b && !terminalAuth(m.snapshot.State.Authentication) && m.snapshot.State.Authentication != AuthLoggedIn
	verify := m.verifyNativeEvidence
	m.mu.Unlock()
	if !matches {
		return errNativeUnqualified
	}
	qualified := false
	if next == AuthLoggedIn {
		if verify == nil || e.Kind != "native-qualified" || e.ID == "" || len(e.ID) > 128 {
			return errNativeUnqualified
		}
		qualified = verify(b, e)
		if !qualified {
			return errNativeUnqualified
		}
	} else if next != AuthLoggingIn && next != AuthChallenging && next != AuthFailed && next != AuthCancelled && next != AuthExpired && next != AuthRevoked {
		return errNativeUnqualified
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.gateMu.Lock()
	m.mu.Lock()
	if ctx.Err() != nil && m.authBinding != nil && *m.authBinding == b && !terminalAuth(m.snapshot.State.Authentication) {
		m.nativeQualified = false
		m.snapshot.State.Authentication = m.authFailureState(ctx.Err())
		m.normalizeTrustedLocked()
		m.mu.Unlock()
		m.gateMu.Unlock()
		return errNativeUnqualified
	}
	if m.completionConsumed || ctx.Err() != nil || (next == AuthLoggedIn && m.snapshot.State.Process != ProcessRunning) || m.closed.Load() || m.gate != gateOpen || m.authBinding == nil || *m.authBinding != b || terminalAuth(m.snapshot.State.Authentication) || m.snapshot.State.Authentication == AuthLoggedIn {
		m.mu.Unlock()
		m.gateMu.Unlock()
		return errNativeUnqualified
	}
	if time.Now().UnixMilli() >= b.ExpiresAt {
		m.snapshot.State.Authentication = AuthExpired
		m.nativeQualified = false
		m.normalizeTrustedLocked()
		m.mu.Unlock()
		m.gateMu.Unlock()
		return errNativeUnqualified
	}
	m.nativeQualified = qualified
	if qualified {
		m.completionConsumed = true
		m.snapshot.State.Session = SessionValid
	}
	m.nativeEvidence = e
	m.snapshot.State.Authentication = next
	m.normalizeTrustedLocked()
	out := cloneAuthSnapshot(m.snapshot)
	m.mu.Unlock()
	m.gateMu.Unlock()
	if m.sink != nil {
		m.sink(out)
	}
	return nil
}

// Called under gateMu; no emitMu acquisition (preserves existing sign-out lock order).
func (m *DRMManager) revokeTrustedLocked() {
	if !m.cfg.RequireTrustedCompletion {
		return
	}
	m.mu.Lock()
	m.nativeQualified = false
	m.snapshot.State.Authentication = AuthRevoked
	m.normalizeTrustedLocked()
	m.mu.Unlock()
}

// Caller holds mu. Raw backend strings, persisted markers and nil Start results
// may update diagnostics; they cannot manufacture qualified Native authority.
func (m *DRMManager) normalizeTrustedLocked() {
	if !m.cfg.RequireTrustedCompletion {
		return
	}
	if m.closed.Load() {
		m.nativeQualified = false
		m.snapshot.State.Authentication = AuthRevoked
	}
	if m.authBinding != nil && time.Now().UnixMilli() >= m.authBinding.ExpiresAt && !terminalAuth(m.snapshot.State.Authentication) {
		m.nativeQualified = false
		m.snapshot.State.Authentication = AuthExpired
	}
	if m.nativeQualified && (m.snapshot.State.Authentication == AuthLoggedOut || m.snapshot.State.Process == ProcessStopped || m.snapshot.State.Process == ProcessFailed) {
		m.snapshot.State.Authentication = AuthRevoked
	}
	if terminalAuth(m.snapshot.State.Authentication) {
		m.cancelPrivateInputLocked()
	}
	if terminalAuth(m.snapshot.State.Authentication) || m.snapshot.State.Process == ProcessStopped || m.snapshot.State.Process == ProcessFailed {
		m.nativeQualified = false
	}
	if !m.nativeQualified {
		if m.snapshot.State.Authentication == AuthLoggedIn {
			m.snapshot.State.Authentication = AuthLoggingIn
		}
		if m.snapshot.State.Session == SessionValid {
			m.snapshot.State.Session = SessionEmpty
		}
		m.snapshot.Capabilities = CapabilityState{}
		m.snapshot.AuthEvidence = nil
	} else {
		e := m.nativeEvidence
		m.snapshot.AuthEvidence = &e
	}
	m.snapshot.NativeAuthenticated = m.nativeQualified
	if m.authBinding != nil {
		b := *m.authBinding
		m.snapshot.AuthBinding = &b
	}
}
func cloneAuthSnapshot(s DRMSnapshot) DRMSnapshot {
	if s.AuthBinding != nil {
		b := *s.AuthBinding
		s.AuthBinding = &b
	}
	if s.AuthEvidence != nil {
		e := *s.AuthEvidence
		s.AuthEvidence = &e
	}
	return s
}
