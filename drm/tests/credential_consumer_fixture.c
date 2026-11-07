/* Controlled Native provider, compiled ONLY under u2_native_credential_fixture. */
#include "../native/credential_consumer.h"
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
struct fixture_data{pthread_mutex_t lock;pthread_cond_t cond;int mode,started,released;const struct drm_owned_credential *held;};
void *u222_fixture_data(int mode){struct fixture_data *d=calloc(1,sizeof(*d));if(!d)return NULL;pthread_mutex_init(&d->lock,NULL);pthread_cond_init(&d->cond,NULL);d->mode=mode;return d;}
static int fixture_submit(struct drm_native_credential *ctx,const struct drm_owned_credential *in,void *ud){
 (void)ctx;struct fixture_data *d=ud;if(!d)return -1;
 if(in->user_len&&(in->user_len<8||memcmp(in->user,"u220-",5)||in->password_len<15||memcmp(in->password,"u220-synthetic-",15)))return -1;
 pthread_mutex_lock(&d->lock);d->started=1;d->held=in;pthread_cond_broadcast(&d->cond);
 while(d->mode==4&&!d->released)pthread_cond_wait(&d->cond,&d->lock);
 int result=d->mode==2?-1:(in->user_len&&(d->mode==1||d->mode==3)?1:0);
 if(d->mode!=3)d->held=NULL;pthread_mutex_unlock(&d->lock);return result;
}
static void *fixture_no_memory(size_t n){(void)n;return NULL;}
static const struct drm_credential_provider sync_provider={DRM_CREDENTIAL_PROVIDER_ABI,1,fixture_submit,NULL,NULL};
static const struct drm_credential_provider retained_provider={DRM_CREDENTIAL_PROVIDER_ABI,2,fixture_submit,NULL,NULL};
static void fixture_release_memory(void *p,size_t n){(void)n;free(p);}
static const struct drm_credential_provider oom_provider={DRM_CREDENTIAL_PROVIDER_ABI,1,fixture_submit,fixture_no_memory,fixture_release_memory};
const struct drm_credential_provider *u222_fixture_provider(int mode){return mode==3?&retained_provider:mode==5?&oom_provider:&sync_provider;}
void u222_fixture_release(void *ud){struct fixture_data *d=ud;pthread_mutex_lock(&d->lock);d->released=1;pthread_cond_broadcast(&d->cond);pthread_mutex_unlock(&d->lock);}
int u222_fixture_started(void *ud){struct fixture_data *d=ud;pthread_mutex_lock(&d->lock);int n=d->started;pthread_mutex_unlock(&d->lock);return n;}
int u222_fixture_held_valid(void *ud){struct fixture_data *d=ud;pthread_mutex_lock(&d->lock);int yes=d->held&&d->held->password_len>=15&&memcmp(d->held->password,"u220-synthetic-",15)==0;pthread_mutex_unlock(&d->lock);return yes;}
