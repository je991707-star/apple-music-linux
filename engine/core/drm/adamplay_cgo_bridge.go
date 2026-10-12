//go:build (linux || windows) && adamplay_backend

package drm

/*
#include <string.h>
#include <stdlib.h>
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
	"unsafe"
)

// adamplayBridgeAuth is the wrapper's auth callback: it fills buf (size bytes,
// NUL-terminated) with the user's reply, blocking until they have entered it.
//
//export adamplayBridgeAuth
func adamplayBridgeAuth(cType *C.char, buf *C.char, size C.int, ud unsafe.Pointer) {
	if size <= 0 || buf == nil {
		return
	}
	C.memset(unsafe.Pointer(buf), 0, C.size_t(size))
	if ud == nil {
		return
	}
	b, ok := cgo.Handle(*(*uintptr)(ud)).Value().(*adamplayBackend)
	if !ok {
		return
	}
	reply := b.answerChallenge(C.GoString(cType))
	if len(reply) > int(size)-1 {
		reply = reply[:int(size)-1]
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(size)), reply)
}

//export adamplayBridgeState
func adamplayBridgeState(cState *C.char, ud unsafe.Pointer) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Get the state channel from userdata
	if ud == nil {
		return
	}

	// ud points at the C-allocated cell that holds the cgo.Handle.
	handle := cgo.Handle(*(*uintptr)(ud))
	stateCh, ok := handle.Value().(chan string)
	if !ok {
		return
	}

	// Convert C string to Go string
	state := C.GoString(cState)

	// Send to channel (non-blocking)
	select {
	case stateCh <- state:
	default:
		// Channel full, ignore
	}
}
