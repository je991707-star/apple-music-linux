//go:build !cgo

package vlc

import (
	"context"
	"engine/internal/playbackcmd"
	"errors"
	"io"
)

// Without cgo there is no libvlc, so lossless (ALAC) playback is unavailable. The engine already treats a
// failed New as "no player" (APIServer.vlcPlayer stays nil), so AAC and music videos, which play in the
// browser, are unaffected. This file replaces player.go and callbacks.go only when cgo is off; the API
// below must match theirs.
var ErrUnavailable = errors.New("vlc: built without cgo, lossless playback is unavailable")

// Source mirrors the interface in callbacks.go, which the engine's handlers implement.
type Source interface {
	io.ReadSeeker
	Size() int64
	Abort()
	Close()
}

// Player is never constructed: New always fails.
type Player struct{}

func New() (*Player, error) { return nil, ErrUnavailable }

func (p *Player) Load(string) error            { return ErrUnavailable }
func (p *Player) LoadSource(Source) error      { return ErrUnavailable }
func (p *Player) Volume() (int, bool)          { return 0, false }
func (p *Player) SetTime(int64)                {}
func (p *Player) Time() (int64, int64, string) { return 0, 0, "stopped" }
func (p *Player) SourceEnded() bool            { return false }
func (p *Player) Pause()                       {}
func (p *Player) Resume()                      {}
func (p *Player) SetVolume(int)                {}
func (p *Player) Stop()                        {}
func (p *Player) Close()                       {}

var ErrTerminalSeek = errors.New("vlc: ended media requires reload for seek")

func (p *Player) LoadCommand(string, *playbackcmd.Ticket, int64) error { return ErrUnavailable }
func (p *Player) LoadSourceCommand(s Source, _ *playbackcmd.Ticket, _ int64) error {
	s.Abort()
	s.Close()
	return ErrUnavailable
}
func (p *Player) ReloadSourceCommand(_ context.Context, s Source, _ *playbackcmd.Ticket, _ int64) error {
	s.Abort()
	s.Close()
	return ErrUnavailable
}
func (p *Player) SetTimeCommand(context.Context, int64, *playbackcmd.Ticket) error {
	return ErrUnavailable
}
func (p *Player) QueueTimeCommand(int64, *playbackcmd.Ticket) error  { return ErrUnavailable }
func (p *Player) TransportCommand(string, *playbackcmd.Ticket) error { return ErrUnavailable }

func (p *Player) StopRetiredLease(*playbackcmd.Lease) (bool, int) { return false, 0 }

func (p *Player) SetVolumeForLease(int, *playbackcmd.Lease, func() bool) error {
	return playbackcmd.ErrStale
}

func (p *Player) OwnStatusForLease(*playbackcmd.Lease) (int64, int64, string, *bool) {
	return -1, -1, "unloaded", nil
}

func (p *Player) OutputBackend() string { return "unavailable" }

var ErrVolumeOutputUnsupported = errors.New("vlc: output volume capability unavailable")
