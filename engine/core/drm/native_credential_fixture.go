//go:build linux && cgo && u2_native_credential_fixture

package drm

/*
#include "../../../drm/native/credential_consumer.h"
#include <stdlib.h>
#cgo CFLAGS: -DDRM_CREDENTIAL_FIXTURE_SCOPES
#cgo LDFLAGS: -lstdc++
void *u223_fixture_target(int);
void *u223_fixture_bridge(void *,int);
const struct drm_credential_provider *u223_fixture_provider(void);
void *u222_fixture_data(int);
const struct drm_credential_provider *u222_fixture_provider(int);
void u222_fixture_release(void *);
int u222_fixture_started(void *);
int u222_fixture_held_valid(void *);
*/
import "C"

func fixtureCredentialEndpoint(mode int) *cCredentialEndpoint {
	if mode >= 10 {
		target := C.u223_fixture_target(C.int(mode))
		return &cCredentialEndpoint{process: C.drm_credential_test_process(), provider: C.u223_fixture_provider(), data: C.u223_fixture_bridge(target, C.int(mode))}
	}
	return &cCredentialEndpoint{process: C.drm_credential_test_process(), provider: C.u222_fixture_provider(C.int(mode)), data: C.u222_fixture_data(C.int(mode))}
}
func (e *cCredentialEndpoint) fixtureRelease()      { C.u222_fixture_release(e.data) }
func (e *cCredentialEndpoint) fixtureStarted() bool { return C.u222_fixture_started(e.data) != 0 }
func (e *cCredentialEndpoint) fixtureHeld() bool    { return C.u222_fixture_held_valid(e.data) != 0 }
func (e *cCredentialEndpoint) fixtureMetric(k int) uint {
	return uint(C.drm_credential_metric(e.ptr(), C.int(k)))
}
func (e *cCredentialEndpoint) fixtureReceipt(tag [32]byte, gen uint64) bool {
	p := C.CBytes(tag[:])
	defer C.free(p)
	return C.drm_credential_receipt(e.ptr(), (*C.uchar)(p), C.uint64_t(gen)) != 0
}

// Exists in fixture builds ONLY. Uses the actual production adapter and Native C source.
func (m *DRMManager) EnableControlledNativeCredentialConsumer(mode int) {
	a := newNativeCredentialAdapter(fixtureCredentialEndpoint(mode))
	m.mu.Lock()
	m.privateInputConsumer, m.privateInputRevoke = a.consumer()
	m.privateInputShutdown = a.shutdown
	m.mu.Unlock()
}
