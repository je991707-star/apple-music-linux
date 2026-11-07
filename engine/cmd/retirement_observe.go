//go:build !vlc_ordering_test

package main

import "engine/core/vlc"

func observeRetirementSource(_ *APIServer, src vlc.Source) vlc.Source { return src }
