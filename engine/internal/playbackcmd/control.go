// Package playbackcmd separates cancellable intent admission from native commit.
package playbackcmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrStale = errors.New("stale playback command")
var ErrLegacy = errors.New("versioned playback control is active")

type Identity struct {
	Boot     string `json:"boot"`
	Epoch    string `json:"epoch"`
	Session  string `json:"sessionId"`
	Load     uint64 `json:"loadGeneration"`
	Sequence uint64 `json:"sequence"`
	Type     string `json:"type"`
	Request  string `json:"requestId"`
}
type Snapshot struct {
	Boot      string `json:"boot"`
	Epoch     string `json:"epoch"`
	Session   string `json:"sessionId"`
	Load      uint64 `json:"loadGeneration"`
	Sequence  uint64 `json:"sequence"`
	Versioned bool   `json:"versioned"`
}
type Ticket struct {
	Identity      Identity
	once          sync.Once
	done          chan struct{}
	cancelled     atomic.Bool
	entered       atomic.Bool
	nativeEntered atomic.Bool
	lease         atomic.Pointer[Lease]
}

func (t *Ticket) Valid() bool {
	return t != nil && !t.cancelled.Load() && (t.lease.Load() == nil || t.lease.Load().Active())
}
func (t *Ticket) Done() <-chan struct{} { return t.done }
func (t *Ticket) Cancel()               { t.once.Do(func() { t.cancelled.Store(true); close(t.done) }) }
func nonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type Gate struct {
	mu                        sync.Mutex
	boot, epoch, cur, desired string
	stoppedSession            string
	claimedSource             *Lease
	load, sequence            uint64
	versioned                 bool
	closed                    bool
	currentSource             *Lease
	latest                    *Ticket
	commit                    chan struct{}
}

func (g *Gate) init() {
	if g.boot == "" {
		g.boot = nonce()
		g.commit = make(chan struct{}, 1)
	}
}
func (g *Gate) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.init()
	return Snapshot{g.boot, g.epoch, g.desired, g.load, g.sequence, g.versioned}
}

// Claim requires prior API authentication and application Session ownership.
// CAS prevents an old claim from silently replacing a newer renderer epoch.
func (g *Gate) Claim(boot, previous, session string) (Snapshot, error) {
	return g.claim(boot, previous, session, nil)
}

// ClaimSource is called under Manager -> Lease lifetime admission. It may replace
// a retired source claim, including a claim that never admitted a Native ticket.
// This changes desired authority only; no Native rollback is implied.
func (g *Gate) ClaimSource(boot, previous, session string, l *Lease) (Snapshot, error) {
	if l == nil || l.Identity.Boot != boot || l.Identity.Session != session || !l.Active() {
		return Snapshot{}, ErrStale
	}
	return g.claim(boot, previous, session, l)
}
func (g *Gate) claim(boot, previous, session string, source *Lease) (Snapshot, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.init()
	if g.closed || boot != g.boot || previous != g.epoch || session == "" || (g.desired != "" && session != g.desired && !(source != nil && g.claimedSource != nil && !g.claimedSource.Active())) {
		return Snapshot{}, ErrStale
	}
	if g.latest != nil {
		g.latest.Cancel()
	}
	g.epoch = nonce()
	g.versioned = true
	g.load = 0
	g.sequence = 0
	g.desired = session
	g.claimedSource = source
	return Snapshot{g.boot, g.epoch, g.desired, g.load, g.sequence, true}, nil
}
func (g *Gate) SetSession(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.init()
	g.cur = id
	g.desired = id
	if g.latest != nil {
		g.latest.Cancel()
	}
}
func (g *Gate) Owns(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return id != "" && id == g.cur && id == g.desired
}
func (g *Gate) Admit(id *Identity, kind, session string) (*Ticket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.init()
	if g.closed {
		return nil, ErrStale
	}
	var cmd Identity
	if id == nil {
		if g.versioned {
			return nil, ErrLegacy
		}
		cmd = Identity{Boot: g.boot, Epoch: "legacy", Session: session, Load: g.load, Sequence: g.sequence + 1, Type: kind, Request: fmt.Sprint("legacy-", g.sequence+1)}
		if kind == "load" {
			cmd.Load++
		}
	} else {
		cmd = *id
		if !g.versioned || cmd.Boot != g.boot || cmd.Epoch != g.epoch || cmd.Type != kind || cmd.Session != session || !validRequestID(cmd.Request) || cmd.Sequence <= g.sequence || cmd.Sequence > (1<<53)-1 || cmd.Load > (1<<53)-1 {
			return nil, ErrStale
		}
		if kind == "load" {
			if cmd.Load <= g.load {
				return nil, ErrStale
			}
		} else if cmd.Load != g.load {
			return nil, ErrStale
		}
	}
	if kind == "resume" && id != nil && session != g.cur {
		return nil, ErrStale
	}
	if kind != "load" {
		if id != nil && (session == "" || (session != g.desired && !(kind == "stop" && g.desired == "" && session == g.stoppedSession))) {
			return nil, ErrStale
		}
		if (kind == "seek" || kind == "intent") && (session == "" || session != g.cur || session != g.desired) {
			return nil, ErrStale
		}
	}
	if g.latest != nil {
		g.latest.Cancel()
	}
	t := &Ticket{Identity: cmd, done: make(chan struct{})}
	g.latest = t
	g.sequence = cmd.Sequence
	g.load = cmd.Load
	if kind == "load" {
		g.desired = session
	}
	return t, nil
}

// Current runs metadata-only work, never I/O or libVLC, under the admission lock.
func (g *Gate) Current(t *Ticket, fn func()) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !t.Valid() {
		return ErrStale
	}
	fn()
	return nil
}

// Commit serializes native effects without blocking admission/cancellation.
func (g *Gate) Commit(ctx context.Context, t *Ticket, fn func() error) error {
	g.mu.Lock()
	g.init()
	commit := g.commit
	g.mu.Unlock()
	select {
	case commit <- struct{}{}:
	case <-t.Done():
		return ErrStale
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-commit }()
	if !t.Valid() {
		return ErrStale
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.entered.Store(true)
	if err := fn(); err != nil {
		return err
	}
	if !t.Valid() {
		return ErrStale
	}
	return nil
}

// Loaded/Stopped are called only within Commit, after the actual native effect.
func (g *Gate) Loaded(t *Ticket) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cur = t.Identity.Session
	g.currentSource = t.SourceLease()
}
func (g *Gate) Stopped(t *Ticket) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.currentSource = nil
	g.cur = "" // The completed C call stopped the native media.
	// Admission may have advanced while C was running. Only the latest Stop
	// may retire its desired owner; never erase a newer Load/transport intent.
	if g.latest == t {
		g.stoppedSession = t.Identity.Session
		g.desired = ""
	}
}

// Context permits bounded cancellation callbacks without coupling native locks to admission.
func (t *Ticket) Context() context.Context { return ticketContext{Context: context.Background(), t: t} }

type ticketContext struct {
	context.Context
	t *Ticket
}

func (c ticketContext) Done() <-chan struct{} { return c.t.Done() }
func (c ticketContext) Err() error {
	if !c.t.Valid() {
		return context.Canceled
	}
	return nil
}

func (t *Ticket) Entered() bool { return t.entered.Load() }

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 80 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// Shutdown revokes pending work before the Player is torn down. A new Engine
// constructs a fresh Gate/Boot; a closed Gate cannot be reclaimed.
func (g *Gate) Shutdown() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.latest != nil {
		g.latest.Cancel()
	}
}

// Retirement cleanup shares native serialization but never admits a new command.
func (g *Gate) Cleanup(ctx context.Context, fn func() error) error {
	g.mu.Lock()
	g.init()
	c := g.commit
	g.mu.Unlock()
	select {
	case c <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c }()
	return fn()
}
func (g *Gate) RetiredStopped(l *Lease) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cur == l.Identity.Session && g.currentSource == l {
		g.cur = ""
		g.currentSource = nil
	}
	if g.desired == l.Identity.Session && g.latest != nil && g.latest.SourceLease() == l {
		g.desired = ""
	}
}
