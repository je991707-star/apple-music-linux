/* Private LOCAL request capabilities. Never Vendor identity or authentication. */
#ifndef ADAM_REQUEST_PROVENANCE_H
#define ADAM_REQUEST_PROVENANCE_H
#include "credential_consumer.h"
struct drm_native_request;
struct drm_request_proof { const struct drm_native_request *ticket; unsigned char nonce[16]; };
enum drm_callback_reason { DRM_QUALIFIED_ORIGIN=0, DRM_UNKNOWN_ORIGIN=1, DRM_REVOKED_SCOPE=2, DRM_STALE_ENGINE=3, DRM_DUPLICATE_CALLBACK=4, DRM_EXPIRED_REQUEST=5, DRM_SHUTDOWN_IN_PROGRESS=6 };
enum drm_callback_stage { DRM_STAGE_LOCAL_REQUEST=0, DRM_STAGE_VENDOR_CREDENTIAL=1, DRM_STAGE_RECEIPT=2 };
#ifdef __cplusplus
extern "C" {
#endif
CRED_PRIVATE struct drm_native_request *drm_request_begin(struct drm_native_credential *);
CRED_PRIVATE int drm_request_capture(const struct drm_native_request *,struct drm_request_proof *);
CRED_PRIVATE enum drm_callback_reason drm_request_callback(struct drm_native_credential *,const struct drm_request_proof *);
CRED_PRIVATE void drm_callback_unknown(enum drm_callback_stage);
CRED_PRIVATE unsigned drm_callback_diagnostic(enum drm_callback_reason,enum drm_callback_stage);
#ifdef __cplusplus
}
#endif
#endif
