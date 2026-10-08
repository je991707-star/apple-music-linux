package main

import "sync"

// vlcGate remembers which playback session currently owns the libvlc player so
// a delayed seek from a track the user already skipped cannot move the next one.
type vlcGate struct {
	mu  sync.Mutex
	cur string
}

func (g *vlcGate) set(id string) {
	g.mu.Lock()
	g.cur = id
	g.mu.Unlock()
}

func (g *vlcGate) clear() { g.set("") }

// owns reports whether id may drive the player. An empty id is rejected: every
// caller of /vlc/seek supplies the session it was started for.
func (g *vlcGate) owns(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return id != "" && id == g.cur
}
