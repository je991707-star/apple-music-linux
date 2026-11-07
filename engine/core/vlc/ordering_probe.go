//go:build vlc_ordering_test

package vlc

/*
#cgo LDFLAGS: -lvlc
#include <vlc/vlc.h>
static libvlc_instance_t *ordering_instance(void) {
 const char *args[]={"--aout=dummy","--no-video","--no-metadata-network-access"};
 return libvlc_new(3,args);
}
*/
import "C"
import (
	"fmt"
	"runtime/cgo"
)

func newOrderingPlayer() (*Player, error) {
	inst := C.ordering_instance()
	if inst == nil {
		return nil, fmt.Errorf("ordering libvlc_new failed")
	}
	mp := C.libvlc_media_player_new(inst)
	if mp == nil {
		C.libvlc_release(inst)
		return nil, fmt.Errorf("ordering player failed")
	}
	return &Player{inst: inst, mp: mp, volume: 0}, nil
}

// Configure before native operations; unavailable in normal builds.
func (p *Player) SetOrderingObserver(fn func(string, int64)) { p.seekProbe = fn }
func (p *Player) OrderingOwnership() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := map[string]any{"physicalLoad": p.loadGen, "nativeSeek": p.seekGen, "callbackAttached": p.cur != nil}
	if p.cur != nil {
		v["callbackHandle"] = uint64(p.cur.handle)
		v["physicalMedia"] = fmt.Sprintf("load:%d/callback:%d", p.loadGen, uint64(p.cur.handle))
	}
	if p.lease != nil {
		v["lease"] = p.lease.Identity
	}
	return v
}
func OrderingHandleDeleted(id uint64) (deleted bool) {
	defer func() { deleted = recover() != nil }()
	cgo.Handle(id).Value()
	return false
}
