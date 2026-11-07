//go:build linux && cgo && u2_native_credential_fixture

package drm

/*
#include <stdlib.h>
#include "../../../drm/native/credential_response_bridge.h"
void *u223_fixture_target(int);
void *u223_fixture_bridge(void *,int);
const struct drm_credential_provider *u223_fixture_provider(void);
int u223_fixture_valid(void *);
int u223_fixture_calls(void *);
int u223_fixture_password_size(void *);
void u223_fixture_release(void *);
*/
import "C"
import "unsafe"

func bridge223(mode int) (*cCredentialEndpoint, unsafe.Pointer) {
	target := C.u223_fixture_target(C.int(mode))
	return &cCredentialEndpoint{process: C.drm_credential_test_process(), provider: C.u223_fixture_provider(), data: C.u223_fixture_bridge(target, C.int(mode))}, target
}
func valid223(target unsafe.Pointer) bool { return C.u223_fixture_valid(target) != 0 }
func calls223(target unsafe.Pointer) int  { return int(C.u223_fixture_calls(target)) }
func size223(target unsafe.Pointer) int   { return int(C.u223_fixture_password_size(target)) }
func release223(target unsafe.Pointer)    { C.u223_fixture_release(target) }
func qualifiedBridge223(mode int) bool {
	target := C.u223_fixture_target(C.int(mode))
	return C.u223_fixture_bridge(target, C.int(mode)) != nil
}
