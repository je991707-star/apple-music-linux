/* Internal owned ingress. No public login API and no qualified Apple provider. */
#ifndef ADAM_CREDENTIAL_CONSUMER_H
#define ADAM_CREDENTIAL_CONSUMER_H
#include <stddef.h>
#include <stdint.h>
#ifndef _WIN32
#define CRED_PRIVATE __attribute__((visibility("hidden")))
#else
#define CRED_PRIVATE
#endif
#define DRM_CREDENTIAL_PROVIDER_ABI 2
struct drm_native_credential;
struct drm_credential_process;
struct drm_owned_credential { size_t user_len, password_len, reply_len; unsigned char user[257], password[256], reply[7]; };
/* allocate/deallocate must both be NULL or a trusted compatible pair.
 * The descriptor and its callbacks must outlive the context.
 * CALL: borrowed until submit returns. PROCESS: storage is retained until exec exit.
 * This is a wrapper contract, NOT a discovered Vendor CredentialResponse ABI. */
struct drm_credential_provider { unsigned abi, ownership; int (*submit)(struct drm_native_credential *,const struct drm_owned_credential *,void *); void *(*allocate)(size_t); void (*deallocate)(void *,size_t); };
CRED_PRIVATE const struct drm_credential_provider *drm_credential_real_provider(void);
CRED_PRIVATE int drm_credential_provider_qualified(const struct drm_credential_provider *);
CRED_PRIVATE struct drm_native_credential *drm_credential_new(const struct drm_credential_provider *,void *,const unsigned char *,uint64_t,int64_t);
CRED_PRIVATE struct drm_native_credential *drm_credential_new_scoped(struct drm_credential_process *,const struct drm_credential_provider *,void *,const unsigned char *,uint64_t,int64_t);
CRED_PRIVATE void drm_credential_process_shutdown(struct drm_credential_process *);
#ifdef DRM_CREDENTIAL_FIXTURE_SCOPES
/* Controlled tests model separate Engine processes, never a production reset. */
CRED_PRIVATE struct drm_credential_process *drm_credential_test_process(void);
#endif
CRED_PRIVATE int drm_credential_submit(struct drm_native_credential *,const unsigned char *,uint64_t,int,const void *,size_t,const void *,size_t,const void *,size_t,const char *,size_t);
CRED_PRIVATE int drm_credential_live(struct drm_native_credential *);
CRED_PRIVATE void drm_credential_revoke(struct drm_native_credential *);
CRED_PRIVATE int drm_credential_receipt(struct drm_native_credential *,const unsigned char *,uint64_t);
CRED_PRIVATE unsigned drm_credential_metric(struct drm_native_credential *,int);
#endif
