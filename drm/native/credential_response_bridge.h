#ifndef ADAM_CREDENTIAL_RESPONSE_BRIDGE_H
#define ADAM_CREDENTIAL_RESPONSE_BRIDGE_H
#include "credential_consumer.h"
#include "credential_response.h"
struct drm_response_bridge;
#ifdef __cplusplus
extern "C" {
#endif
/* No public registration and no real Vendor factory. Ops/target are C process-life. */
CRED_PRIVATE struct drm_response_bridge *drm_response_bridge_new(const struct drm_response_ops *,void *,int);
CRED_PRIVATE const struct drm_credential_provider *drm_response_bridge_provider(void);
#ifdef __cplusplus
}
#endif
#endif
