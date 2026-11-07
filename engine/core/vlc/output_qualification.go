//go:build vlc_ordering_test && cgo && linux

package vlc

/*
#include <vlc/vlc.h>
static libvlc_instance_t *private_pulse_instance(void){const char *a[]={"--aout=pulse,none","--no-video","--no-metadata-network-access"};return libvlc_new(3,a);}
*/
import "C"
import "fmt"

// Test build only. Caller must supply a private, isolated PULSE_SERVER.
func NewPrivatePulseTestPlayer() (*Player, error) {
	i := C.private_pulse_instance()
	if i == nil {
		return nil, fmt.Errorf("private pulse instance")
	}
	mp := C.libvlc_media_player_new(i)
	if mp == nil {
		C.libvlc_release(i)
		return nil, fmt.Errorf("private pulse player")
	}
	return &Player{inst: i, mp: mp, volume: 100, outputBackend: "native-pulse"}, nil
}

// PrivateRawVolumeReadback is diagnostic only; unlike Volume it never substitutes
// the remembered application setting for an unavailable native value.
func (p *Player) PrivateRawVolumeReadback() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mp == nil {
		return -1
	}
	return int(C.libvlc_audio_get_volume(p.mp))
}
