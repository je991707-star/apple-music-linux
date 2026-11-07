package main

import "engine/internal/playbackcmd"

// Admission and native commit have separate locks. Source preparation never
// occupies the commit gate; newer intent can cancel it immediately.
type vlcGate struct{ order playbackcmd.Gate }

func (g *vlcGate) set(id string)       { g.order.SetSession(id) }
func (g *vlcGate) clear()              { g.order.SetSession("") }
func (g *vlcGate) owns(id string) bool { return g.order.Owns(id) }
