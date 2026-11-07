package drm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// Borrowed only for the synchronous consumer call. Not Native/vendor ownership.
// Go/JSON/TLS internals can retain copies; Clear covers these owned slices only.
type PrivateInput struct {
	Email    []byte `json:"email,omitempty"`
	Password []byte `json:"password,omitempty"`
	Reply    []byte `json:"reply,omitempty"`
}

func (p *PrivateInput) Clear() {
	if p != nil {
		clear(p.Email)
		clear(p.Password)
		clear(p.Reply)
	}
}
func (p PrivateInput) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "[redacted input]") }
func (p PrivateInput) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (p PrivateInput) LogValue() slog.Value         { return slog.StringValue("[redacted input]") }
func (p *PrivateInput) valid(kind string) bool {
	if p == nil {
		return false
	}
	clean := func(v []byte, max int) bool {
		return len(v) > 0 && len(v) <= max && utf8.Valid(v) && bytes.IndexByte(v, 0) < 0
	}
	if kind == "credentials" {
		return clean(p.Email, 256) && clean(p.Password, 255) && len(p.Reply) == 0
	}
	if kind != "challenge" || len(p.Email) != 0 || len(p.Password) != 0 || len(p.Reply) != 6 {
		return false
	}
	for _, c := range p.Reply {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type PrivateInputChallenge struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	ExpiresAt int64  `json:"expiresAt"`
}
type PrivateInputStatus struct {
	State     string                 `json:"inputState"`
	Challenge *PrivateInputChallenge `json:"inputChallenge,omitempty"`
}

// Only a qualified internal backend may install a consumer. Production Vendor remains unqualified.
// Consumers borrow Go slices only for this call; Native must make owned C copies.
type privateInputConsumer func(context.Context, AuthBinding, string, *PrivateInput) (*PrivateInputChallenge, error)
type privateInputAttempt struct {
	used      bool
	busy      bool
	challenge *PrivateInputChallenge
	cancel    context.CancelFunc
	state     string
}

var ErrPrivateInputUnavailable = errors.New("private credential consumer unavailable")

func (m *DRMManager) cancelPrivateInputLocked() {
	if m.privateInputRevoke != nil {
		m.privateInputRevoke()
	}
	if p := m.privateInput; p != nil {
		p.challenge = nil
		if p.cancel != nil {
			p.cancel()
		}
		p.state = "revoked"
	}
}
func (m *DRMManager) inputScopeLocked(b AuthBinding) bool {
	m.normalizeTrustedLocked()
	return m.cfg.RequireTrustedCompletion && !m.closed.Load() && m.authBinding != nil && *m.authBinding == b && !terminalAuth(m.snapshot.State.Authentication) && m.snapshot.State.Authentication != AuthLoggedIn && time.Now().UnixMilli() < b.ExpiresAt
}
func (m *DRMManager) PrivateInputStatus(ctx context.Context, b AuthBinding) (PrivateInputStatus, error) {
	if ctx.Err() != nil {
		return PrivateInputStatus{}, errNativeUnqualified
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.authBinding == nil || *m.authBinding != b {
		return PrivateInputStatus{}, errNativeUnqualified
	}
	m.normalizeTrustedLocked()
	out := PrivateInputStatus{State: "unavailable"}
	if p := m.privateInput; p != nil {
		out.State = p.state
		if p.challenge != nil && time.Now().UnixMilli() < p.challenge.ExpiresAt {
			v := *p.challenge
			out.Challenge = &v
		}
	}
	return out, nil
}
func (m *DRMManager) HandoffPrivateInput(ctx context.Context, b AuthBinding, kind, challengeID string, in *PrivateInput) (PrivateInputStatus, error) {
	defer in.Clear()
	if ctx.Err() != nil || !in.valid(kind) {
		return PrivateInputStatus{}, errNativeUnqualified
	}
	m.gateMu.Lock()
	m.mu.Lock()
	if m.gate != gateOpen || !m.inputScopeLocked(b) {
		m.mu.Unlock()
		m.gateMu.Unlock()
		return PrivateInputStatus{}, errNativeUnqualified
	}
	consume := m.privateInputConsumer
	if consume == nil {
		m.mu.Unlock()
		m.gateMu.Unlock()
		return PrivateInputStatus{}, ErrPrivateInputUnavailable
	}
	if m.privateInput == nil {
		m.privateInput = &privateInputAttempt{}
	}
	p := m.privateInput
	allowed := !p.busy
	if kind == "credentials" {
		allowed = allowed && !p.used && challengeID == "" && (m.snapshot.State.Authentication == AuthAccepted || m.snapshot.State.Authentication == AuthLoggingIn)
	} else {
		allowed = allowed && p.used && p.challenge != nil && p.challenge.ID == challengeID && time.Now().UnixMilli() < p.challenge.ExpiresAt && m.snapshot.State.Authentication == AuthChallenging
	}
	if !allowed {
		m.mu.Unlock()
		m.gateMu.Unlock()
		return PrivateInputStatus{}, errNativeUnqualified
	}
	// Reserve once before releasing locks. Failed/cancelled sends never reopen input.
	p.used = true
	p.busy = true
	p.challenge = nil
	p.state = "consuming"
	callCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(b.ExpiresAt))
	p.cancel = cancel
	m.mu.Unlock()
	m.gateMu.Unlock()
	defer cancel()
	next, e := consume(callCtx, b, kind, in) // input remains alive through callback unwind
	m.gateMu.Lock()
	m.mu.Lock()
	defer func() { m.mu.Unlock(); m.gateMu.Unlock() }()
	p.busy = false
	p.cancel = nil
	if m.gate != gateOpen || !m.inputScopeLocked(b) || callCtx.Err() != nil {
		p.state = "revoked"
		return PrivateInputStatus{}, errNativeUnqualified
	}
	if next != nil && (kind != "credentials" || len(next.ID) != 32 || strings.Trim(next.ID, "0123456789abcdef") != "" || next.Type != "two_factor" || next.ExpiresAt <= time.Now().UnixMilli() || next.ExpiresAt > b.ExpiresAt) {
		e = errNativeUnqualified
	}
	if e != nil {
		m.snapshot.State.Authentication = AuthFailed
		p.state = "failed"
		m.normalizeTrustedLocked()
		return PrivateInputStatus{}, errNativeUnqualified
	}
	p.state = "consumed"
	m.snapshot.State.Authentication = AuthLoggingIn
	out := PrivateInputStatus{State: p.state}
	if next != nil {
		v := *next
		p.challenge = &v
		copy := v
		out.Challenge = &copy
		m.snapshot.State.Authentication = AuthChallenging
	}
	m.nativeQualified = false
	m.normalizeTrustedLocked()
	return out, nil
}
