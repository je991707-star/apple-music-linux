package main

import (
	"context"
	"engine/internal/playbackcmd"
	"sync"
	"time"
)

type sourceRetirement struct {
	done     chan struct{}
	mu       sync.Mutex
	err      error
	physical int
	stopped  bool
}

func (s *APIServer) watchSourceLease(t *playbackcmd.Ticket) {
	s.watchLease(t.SourceLease())
}
func (s *APIServer) watchLease(l *playbackcmd.Lease) {
	if l == nil {
		return
	}
	v := &sourceRetirement{done: make(chan struct{})}
	if _, loaded := s.sourceRetirements.LoadOrStore(l, v); loaded {
		return
	}
	go func() {
		<-l.Done()
		// Authority is already synchronously revoked. This goroutine only performs
		// conditional cleanup; it is independent of the potentially delayed vseg hook.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := s.vlcGate.order.Cleanup(ctx, func() error {
			if s.vlcPlayer == nil {
				return nil
			}
			stopped, physical := s.vlcPlayer.StopRetiredLease(l)
			v.mu.Lock()
			v.stopped, v.physical = stopped, physical
			v.mu.Unlock()
			s.vlcGate.order.RetiredStopped(l)
			return nil
		})
		if err == nil {
			err = l.WaitResources(ctx)
		}
		v.mu.Lock()
		v.err = err
		v.mu.Unlock()
		close(v.done)
		// A bounded history is not kept; handles to receipts remain valid for callers.
		if err == nil {
			s.sourceRetirements.Delete(l)
			disposeMainSourceDiagnostics(s, l.Identity.Session)
		}
	}()
}
