//go:build vlc_ordering_test

package main

import (
	"engine/core/vlc"
	"sync"
)

type retirementTestHooks struct {
	prepare func(vlc.Source)
	wrap    func(vlc.Source) vlc.Source
}

var retirementHooks sync.Map

func observeRetirementSource(s *APIServer, src vlc.Source) vlc.Source {
	if h, ok := retirementHooks.Load(s); ok {
		v := h.(retirementTestHooks)
		if v.wrap != nil && src != nil {
			src = v.wrap(src)
		}
		if v.prepare != nil {
			v.prepare(src)
		}
	}
	return src
}
