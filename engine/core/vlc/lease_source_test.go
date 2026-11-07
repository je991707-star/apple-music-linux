//go:build cgo

package vlc

import (
	"bytes"
	"engine/internal/playbackcmd"
	"testing"
	"time"
)

func TestLeaseWatcherCloseIdempotent(t *testing.T) {
	l := playbackcmd.NewLease("boot", "session", 1)
	w := watchLeaseSource(&onceSource{Source: memSource{bytes.NewReader(nil)}}, l)
	w.Close()
	done := make(chan struct{})
	go func() { w.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("repeated lease wrapper Close blocked on a stopped watcher")
	}
}
