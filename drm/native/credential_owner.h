/* Private, process-lifetime storage for one Native initialization attempt.
 * Not a public C ABI. A fresh credential attempt requires a fresh Engine.
 * Callback userdata belongs to the caller and must live through shutdown and
 * (for reentrant shutdown) until all callbacks unwind and an external shutdown/drain completes. */
#ifndef ADAM_CREDENTIAL_OWNER_H
#define ADAM_CREDENTIAL_OWNER_H
#include "drm_lib.h"
#include "credential_consumer.h"
#ifdef DRM_LIB_BUILD
#include "credential_response.h"
#else
struct drm_response_owner;
#endif
#ifndef _WIN32
#define DRM_OWNER_PRIVATE __attribute__((visibility("hidden")))
#else
#define DRM_OWNER_PRIVATE
#endif
struct drm_credential_payload;
union std_string;
struct shared_ptr;
DRM_OWNER_PRIVATE int drm_owner_begin(const drm_lib_config_t *, drm_lib_config_t *);
DRM_OWNER_PRIVATE int drm_owner_active(void);
DRM_OWNER_PRIVATE int drm_owner_complete(void);
DRM_OWNER_PRIVATE void drm_owner_cancel(void);
DRM_OWNER_PRIVATE void drm_owner_fail(void);
DRM_OWNER_PRIVATE int drm_owner_state(const char *);
DRM_OWNER_PRIVATE struct drm_credential_payload *drm_credential_enter(void);
DRM_OWNER_PRIVATE int drm_credential_code(struct drm_credential_payload *);
DRM_OWNER_PRIVATE int drm_credential_publish(struct drm_credential_payload *);
DRM_OWNER_PRIVATE struct drm_response_owner *drm_credential_storage(struct drm_credential_payload *);
DRM_OWNER_PRIVATE struct shared_ptr *drm_credential_response(struct drm_credential_payload *);
DRM_OWNER_PRIVATE union std_string *drm_credential_username(struct drm_credential_payload *);
DRM_OWNER_PRIVATE union std_string *drm_credential_password(struct drm_credential_payload *);
static inline int drm_credential_publish_token(void *p) { return drm_credential_publish((struct drm_credential_payload *)p); }
DRM_OWNER_PRIVATE void drm_credential_leave(struct drm_credential_payload *);
#endif
