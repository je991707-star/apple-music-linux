#include "../native/credential_consumer.h"
#include <stdio.h>
#ifndef DRM_CREDENTIAL_PROVIDER_ABI
#define DRM_CREDENTIAL_PROVIDER_ABI 1
#endif
static void *foreign(size_t n){(void)n;return NULL;}
static int send_input(struct drm_native_credential *c,const struct drm_owned_credential *v,void *u){(void)c;(void)v;(void)u;return 0;}
int main(void){struct drm_credential_provider p={.abi=DRM_CREDENTIAL_PROVIDER_ABI,.ownership=1,.submit=send_input,.allocate=foreign};
 if(drm_credential_provider_qualified(&p)){puts("FAIL: custom allocator accepted without matching deallocator");return 1;}
 if(drm_credential_real_provider()!=NULL){puts("FAIL: unverified real provider enabled");return 2;}
 puts("PASS: allocator contract rejected; real provider closed");return 0;}
