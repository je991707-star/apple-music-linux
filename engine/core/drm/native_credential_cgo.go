//go:build linux && cgo

package drm

/*
#cgo CFLAGS: -D_POSIX_C_SOURCE=200809L
#cgo LDFLAGS: -lpthread
#include "../../../drm/native/credential_consumer.h"
#include <stdlib.h>
static void u222_wipe_free(void *p,size_t n){volatile unsigned char *q=p;if(p){while(n--)*q++=0;free(p);}}
*/
import "C"
import (
	"sync"
	"unsafe"
)

type cCredentialEndpoint struct {
	mu       sync.Mutex
	p        *C.struct_drm_native_credential
	provider *C.struct_drm_credential_provider
	data     unsafe.Pointer
	process  *C.struct_drm_credential_process
}

func newNativeCredentialEndpoint() nativeCredentialEndpoint {
	return &cCredentialEndpoint{provider: C.drm_credential_real_provider()}
}
func (e *cCredentialEndpoint) available() bool {
	return C.drm_credential_provider_qualified(e.provider) != 0
}
func (e *cCredentialEndpoint) bind(tag [32]byte, gen uint64, deadline int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil {
		return errNativeCredentialBoundary
	}
	t := C.CBytes(tag[:])
	defer C.free(t)
	e.p = C.drm_credential_new_scoped(e.process, e.provider, e.data, (*C.uchar)(t), C.uint64_t(gen), C.int64_t(deadline))
	if e.p == nil {
		return errNativeCredentialBoundary
	}
	return nil
}
func (e *cCredentialEndpoint) ptr() *C.struct_drm_native_credential {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.p
}
func (e *cCredentialEndpoint) cancel() { C.drm_credential_revoke(e.ptr()) }
func (e *cCredentialEndpoint) submit(tag [32]byte, gen uint64, kind, id string, in *PrivateInput) (bool, error) {
	t := C.CBytes(tag[:])
	defer C.free(t)
	ch := C.CString(id)
	defer C.free(unsafe.Pointer(ch))
	u := C.CBytes(in.Email)
	p := C.CBytes(in.Password)
	r := C.CBytes(in.Reply)
	defer C.u222_wipe_free(u, C.size_t(len(in.Email)))
	defer C.u222_wipe_free(p, C.size_t(len(in.Password)))
	defer C.u222_wipe_free(r, C.size_t(len(in.Reply)))
	k := 1
	if kind == "challenge" {
		k = 2
	}
	code := C.drm_credential_submit(e.ptr(), (*C.uchar)(t), C.uint64_t(gen), C.int(k), u, C.size_t(len(in.Email)), p, C.size_t(len(in.Password)), r, C.size_t(len(in.Reply)), ch, C.size_t(len(id)))
	if code < 0 {
		return false, errNativeCredentialBoundary
	}
	return code == 1, nil
}

func (e *cCredentialEndpoint) shutdown() { C.drm_credential_process_shutdown(e.process) }
func shutdownNativeCredentialProcess()   { C.drm_credential_process_shutdown(nil) }
