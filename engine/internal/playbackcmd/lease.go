package playbackcmd

import (
	"context"
	"sync"
	"sync/atomic"
)

// Lease is an Engine-internal Session lifetime fence, NOT Main principal authorization.
// Manager.mu -> Lease.mu -> command Gate.mu is the admission order. No I/O under it.
type LeaseIdentity struct {
	Boot       string `json:"boot"`
	Session    string `json:"sessionId"`
	Generation uint64 `json:"sourceGeneration"`
	ID         string `json:"leaseId"`
	OwnerKind  string `json:"ownerKind"`
}
type Lease struct {
	Identity  LeaseIdentity
	mu        sync.Mutex
	active    atomic.Bool
	done      chan struct{}
	tickets   map[*Ticket]struct{}
	entries   atomic.Uint64
	resources int
	drained   chan struct{}
}

func NewLease(boot, session string, generation uint64) *Lease {
	l := &Lease{Identity: LeaseIdentity{boot, session, generation, nonce(), "engine-session-lifecycle; main-principal-unverified"}, done: make(chan struct{}), tickets: map[*Ticket]struct{}{}}
	l.active.Store(true)
	return l
}
func (l *Lease) Active() bool          { return l != nil && l.active.Load() }
func (l *Lease) Done() <-chan struct{} { return l.done }

// Admit binds BEFORE the command can be handed to Native. Retire cannot race past it.
func (l *Lease) Admit(fn func() (*Ticket, error)) (*Ticket, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active.Load() {
		return nil, ErrStale
	}
	t, e := fn()
	if e != nil {
		return nil, e
	}
	if t.Identity.Boot != l.Identity.Boot || t.Identity.Session != l.Identity.Session {
		t.Cancel()
		return nil, ErrStale
	}
	for old := range l.tickets {
		if !old.Valid() {
			delete(l.tickets, old)
		}
	}
	t.lease.Store(l)
	l.tickets[t] = struct{}{}
	return t, nil
}

// WhileActive is for short control/metadata admission only. Never perform I/O
// or Native calls here. It shares retirement's exact linearization boundary.
func (l *Lease) WhileActive(fn func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active.Load() {
		return ErrStale
	}
	return fn()
}

// Retire only flips authority and cancels tickets; never invokes user cleanup/Native.
func (l *Lease) Retire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active.Swap(false) {
		return
	}
	for t := range l.tickets {
		t.Cancel()
	}
	close(l.done)
}

// enter is the irrevocable admission point, under Player.mu, just before Native
// effects. Retirement need not wait for C; already entered effects may have applied.
func (l *Lease) enter(t *Ticket) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active.Load() || t.cancelled.Load() {
		return false
	}
	t.nativeEntered.Store(true)
	l.entries.Add(1)
	return true
}
func (t *Ticket) SourceLease() *Lease { return t.lease.Load() }
func (t *Ticket) EnterNative() bool {
	if !t.Valid() {
		return false
	}
	if l := t.lease.Load(); l != nil {
		return l.enter(t)
	}
	t.nativeEntered.Store(true)
	return true
}
func (t *Ticket) NativeEntered() bool { return t.nativeEntered.Load() }
func (l *Lease) Entries() uint64      { return l.entries.Load() }

// Resource tracks a prepared reader through transfer/close. No late acquisition
// after retirement; waiting for drain never holds Lease/Manager/Native locks.
func (l *Lease) Resource() (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active.Load() {
		return nil, false
	}
	if l.resources == 0 {
		l.drained = make(chan struct{})
	}
	l.resources++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.resources--
			if l.resources == 0 {
				close(l.drained)
			}
		})
	}, true
}
func (l *Lease) WaitResources(ctx context.Context) error {
	l.mu.Lock()
	if l.resources == 0 {
		l.mu.Unlock()
		return nil
	}
	ch := l.drained
	l.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
