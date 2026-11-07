#define _GNU_SOURCE
#include "../native/credential_consumer.h"
#include <sys/mman.h>
#include <unistd.h>
#include <assert.h>
#include <time.h>
#include <stdio.h>
static size_t mapped;static unsigned released;
static void *allocate(size_t n){mapped=(n+4095)&~(size_t)4095;void *p=mmap(NULL,mapped,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS,-1,0);return p==MAP_FAILED?NULL:p;}
static void deallocate(void *p,size_t n){for(size_t i=0;i<n;i++)assert(!((unsigned char*)p)[i]);released++;assert(!munmap(p,mapped));}
static int send_input(struct drm_native_credential *c,const struct drm_owned_credential *v,void *u){(void)c;(void)u;assert(v->user_len&&v->password_len);return 0;}
int main(void){struct timespec t;assert(!clock_gettime(CLOCK_REALTIME,&t));int64_t deadline=(int64_t)t.tv_sec*1000+t.tv_nsec/1000000+60000;unsigned char tag[32]={0};
 struct drm_credential_provider p={DRM_CREDENTIAL_PROVIDER_ABI,1,send_input,allocate,deallocate};
 struct drm_native_credential *c=drm_credential_new(&p,NULL,tag,1,deadline);assert(c);
 /* A descriptor changed after bind cannot turn a CALL payload into PROCESS. */
 p.ownership=2;p.deallocate=NULL;
 assert(drm_credential_submit(c,tag,1,1,"u220-user",9,"u220-synthetic-native",21,NULL,0,"0123456789abcdef0123456789abcdef",32)==0);
 assert(released==1);
 struct drm_credential_provider bad={DRM_CREDENTIAL_PROVIDER_ABI,1,send_input,NULL,deallocate};assert(!drm_credential_provider_qualified(&bad));
 puts("PASS explicit mmap/munmap pair, wipe before release, immutable CALL contract, unmatched release rejected");return 0;
}
