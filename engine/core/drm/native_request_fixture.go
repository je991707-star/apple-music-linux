//go:build linux && cgo && u2_native_credential_fixture

package drm

/*
#include "../../../drm/native/request_provenance.h"
#include <stdlib.h>
const struct drm_credential_provider *u223_fixture_provider(void);
int u224_callback(void *,struct drm_native_credential *);
int u224_wrong_nonce(void *,struct drm_native_credential *);
int u224_pointer_reuse(void *,void *,struct drm_native_credential *);
int u224_unknown(struct drm_native_credential *);
static int u224_global_barrier(void){
 unsigned char tag[32]={0};const struct drm_credential_provider *p=u223_fixture_provider();
 struct drm_native_credential *a=drm_credential_new(p,NULL,tag,1,INT64_MAX);
 if(!a)return 0;drm_credential_revoke(a);tag[0]=1;
 return drm_credential_new(p,NULL,tag,2,INT64_MAX)==NULL;
}
*/
import "C"
import "unsafe"

func callback224(target unsafe.Pointer, receiver *cCredentialEndpoint) int {
	return int(C.u224_callback(target, receiver.ptr()))
}
func unknown224(receiver *cCredentialEndpoint) int            { return int(C.u224_unknown(receiver.ptr())) }
func globalBarrier224() bool                                  { return C.u224_global_barrier() != 0 }
func sameProcess224(destination, source *cCredentialEndpoint) { destination.process = source.process }
func diagnostic224(reason, stage int) uint {
	return uint(C.drm_callback_diagnostic(C.enum_drm_callback_reason(reason), C.enum_drm_callback_stage(stage)))
}

func wrongNonce224(target unsafe.Pointer, receiver *cCredentialEndpoint) int {
	return int(C.u224_wrong_nonce(target, receiver.ptr()))
}
func pointerReuse224(old, current unsafe.Pointer, receiver *cCredentialEndpoint) int {
	return int(C.u224_pointer_reuse(old, current, receiver.ptr()))
}
