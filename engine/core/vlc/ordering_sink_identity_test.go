//go:build vlc_ordering_test && cgo && linux

package vlc

import (
	"bytes"
	"testing"
)

func TestIsolatedSinkResetDoesNotAttributeOldPCMToB(t *testing.T) {
	a := bytes.Repeat([]byte{1, 0, 2, 0}, 16)
	b := bytes.Repeat([]byte{3, 0, 4, 0}, 16)
	s := &IsolatedSink{}
	s.ResetForSource(a, map[string]any{"sessionId": "a"})
	s.mu.Lock()
	s.pcm = append([]byte(nil), a...)
	s.events = []map[string]any{{"event": "MediaPlayerEndReached", "resetSerial": s.resetSerial}}
	s.mu.Unlock()
	old := s.Snapshot()
	s.ResetForSource(b, map[string]any{"sessionId": "b"})
	v := s.Snapshot()
	if v["frames"] != 0 || v["full_pcm_exact"] != false || v["resetSerial"].(uint64) <= old["resetSerial"].(uint64) {
		t.Fatal("reset retained current A PCM")
	}
	h := v["history"].([]map[string]any)
	if len(h) != 1 || h[0]["sessionId"] != "a" || h[0]["full_pcm_exact"] != true {
		t.Fatal("A history lost or relabeled")
	}
	s.mu.Lock()
	s.pcm = append(append([]byte(nil), a...), b...)
	s.mu.Unlock()
	if s.Snapshot()["full_pcm_exact"] != false {
		t.Fatal("late A contamination passed as B")
	}
	s.ResetForSource(b, map[string]any{"sessionId": "b2"})
	s.mu.Lock()
	s.pcm = append([]byte(nil), b...)
	s.mu.Unlock()
	if s.Snapshot()["full_pcm_exact"] != true {
		t.Fatal("exact B not accepted")
	}
}
