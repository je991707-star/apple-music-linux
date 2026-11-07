//go:build !u2_source_fixture

package main

import (
	"context"
	"engine/core/media"
	"engine/core/playback"
	"engine/core/vlc"
	"errors"
)

func mainSourceFactoryKind() string                      { return "unavailable" }
func mainSourceProvider(p media.Provider) media.Provider { return p }
func mainSourcePlayer() (*vlc.Player, error)             { return vlc.New() }
func openMainTypedSource(context.Context, *playback.Manager, string) (*playback.Session, error) {
	return nil, errors.New("qualified account factory unavailable")
}
func mainSourceDiagnostics(*APIServer) map[string]any { return nil }

func prepareMainSourceDiagnostics(*APIServer, string) {}

func disposeMainSourceDiagnostics(*APIServer, string) {}
func closeMainSourceDiagnostics(*APIServer)           {}

func mainSourceAccountNetworkAllowed() bool { return true }

func mainScopedLibrary(int, int) (any, bool) { return nil, false }
func mainScopedArtwork() (any, bool)         { return nil, false }
