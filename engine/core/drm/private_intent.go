package drm

import "context"

// These operations register intent/progress only. They never call Start,
// Authenticate, credentials/challenge callbacks, or the Native Vendor.
func (m *DRMManager) BeginPrivateIntent(ctx context.Context, b AuthBinding) (DRMSnapshot, error) {
	if ctx.Err() != nil {
		return DRMSnapshot{}, ctx.Err()
	}
	if err := m.beginTrustedAttempt(b); err != nil {
		return DRMSnapshot{}, err
	}
	return m.PrivateIntentStatus(ctx, b, false)
}
func (m *DRMManager) PrivateIntentStatus(ctx context.Context, b AuthBinding, advance bool) (DRMSnapshot, error) {
	if ctx.Err() != nil {
		return DRMSnapshot{}, ctx.Err()
	}
	m.mu.RLock()
	match := m.cfg.RequireTrustedCompletion && m.authBinding != nil && *m.authBinding == b
	m.mu.RUnlock()
	if !match {
		return DRMSnapshot{}, errNativeUnqualified
	}
	s := m.Status()
	if advance && s.State.Authentication == AuthAccepted {
		// RUNNING means this bounded intent coordinator is waiting for an unavailable
		// Native provider, not that a Vendor login has been attempted.
		_ = m.transitionTrusted(ctx, b, AuthLoggingIn, NativeEvidence{})
		s = m.Status()
	}
	return s, nil
}
func (m *DRMManager) EndPrivateIntent(ctx context.Context, b AuthBinding, state AuthState) (DRMSnapshot, error) {
	if ctx.Err() != nil {
		return DRMSnapshot{}, ctx.Err()
	}
	// Once an authenticated revocation is admitted it remains a tombstone even
	// if the requester disconnects before receiving its acknowledgement.
	ctx = context.WithoutCancel(ctx)
	if state != AuthCancelled && state != AuthRevoked {
		return DRMSnapshot{}, errNativeUnqualified
	}
	m.mu.RLock()
	empty := m.authBinding == nil
	m.mu.RUnlock()
	if empty {
		// A cancellation can arrive before its in-flight begin. Keep the tombstone
		// so a late/replayed begin cannot register work afterward.
		_ = m.beginTrustedAttempt(b)
	}
	s, err := m.PrivateIntentStatus(ctx, b, false)
	if err != nil {
		return DRMSnapshot{}, err
	}
	if !terminalAuth(s.State.Authentication) {
		if err = m.transitionTrusted(ctx, b, state, NativeEvidence{}); err != nil {
			return DRMSnapshot{}, err
		}
	}
	return m.PrivateIntentStatus(ctx, b, false)
}
