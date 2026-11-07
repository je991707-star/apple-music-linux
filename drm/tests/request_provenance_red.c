#define _POSIX_C_SOURCE 200809L
#include "../native/credential_consumer.h"
#include <stdio.h>
#include <time.h>
#include <string.h>
static int consume(struct drm_native_credential *c,const struct drm_owned_credential *v,void *d){(void)c;(void)v;(void)d;return 0;}
int main(int argc,char **argv){(void)argc;unsigned char a[32]={0},b[32]={1};struct timespec t;clock_gettime(CLOCK_REALTIME,&t);int64_t end=(int64_t)t.tv_sec*1000+t.tv_nsec/1000000+60000;struct drm_credential_provider p={DRM_CREDENTIAL_PROVIDER_ABI,1,consume,NULL,NULL};struct drm_native_credential *A=drm_credential_new(&p,NULL,a,1,end);if(!A)return 2;
 if(!strcmp(argv[1],"rebind")){drm_credential_revoke(A);if(drm_credential_new(&p,NULL,b,2,end)){puts("FAIL: a second Native attempt can claim this process after A revoke");return 1;}}
 else if(drm_credential_receipt(A,a,1)){puts("FAIL: receipt accepted without original request evidence");return 1;}
 puts("PASS provenance boundary fail closed");return 0;}
