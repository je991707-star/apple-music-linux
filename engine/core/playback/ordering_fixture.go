//go:build vlc_ordering_test

package playback

import "time"

// NewOrderingSessionFixture supplies fixed, account-free sessions only to the
// recording Handler regression harness. Production excludes this test entry.
func NewOrderingSessionFixture() *Manager {
	m := &Manager{}
	for _, id := range []string{"a", "b"} {
		m.store("", &Session{ID: id, AssetID: "synthetic", Codec: "alac"}, &playContext{expiry: time.Now().Add(sessionTTL)})
	}
	return m
}
