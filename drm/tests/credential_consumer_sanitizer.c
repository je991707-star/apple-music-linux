#define _POSIX_C_SOURCE 200809L
#include "../native/credential_consumer.h"
#include <assert.h>
#include <pthread.h>
#include <string.h>
#include <time.h>
#include <stdio.h>
void *u222_fixture_data(int);
const struct drm_credential_provider *u222_fixture_provider(int);
void u222_fixture_release(void *);
int u222_fixture_started(void *);
int u222_fixture_held_valid(void *);
/* Separate cases model separate Engine processes; no production reset API. */
static struct drm_native_credential *fixture_context(const struct drm_credential_provider *p,void *data,const unsigned char *tag,uint64_t gen,int64_t end){struct drm_credential_process *process=drm_credential_test_process();assert(process);return drm_credential_new_scoped(process,p,data,tag,gen,end);}
#define drm_credential_new fixture_context
static unsigned char tag[32];
static const char user[]="u220-user@example.invalid",password[]="u220-synthetic-native",challenge[]="0123456789abcdef0123456789abcdef";
static int64_t deadline(void){struct timespec t;assert(!clock_gettime(CLOCK_REALTIME,&t));return (int64_t)t.tv_sec*1000+t.tv_nsec/1000000+60000;}
static struct drm_native_credential *make(int mode,void **data){*data=u222_fixture_data(mode);assert(*data);struct drm_native_credential *c=drm_credential_new(u222_fixture_provider(mode),*data,tag,1,deadline());assert(c);return c;}
static int submit(struct drm_native_credential *c){return drm_credential_submit(c,tag,1,1,user,sizeof(user)-1,password,sizeof(password)-1,NULL,0,challenge,32);}
static void *worker(void *c){assert(submit(c)<0);return NULL;}
int main(void){
 void *data;struct drm_native_credential *c=make(1,&data);
 assert(submit(c)==1);assert(submit(c)<0);
 assert(drm_credential_submit(c,tag,1,2,NULL,0,NULL,0,"000001",6,challenge,32)==0);
 assert(drm_credential_submit(c,tag,1,2,NULL,0,NULL,0,"000001",6,challenge,32)<0);
 puts("PASS challenge/replay");
 c=make(3,&data);assert(submit(c)==1);assert(u222_fixture_held_valid(data));drm_credential_revoke(c);assert(u222_fixture_held_valid(data));assert(!drm_credential_receipt(c,tag,1));puts("PASS process-borrow/revoke");
 c=make(4,&data);pthread_t th;assert(!pthread_create(&th,NULL,worker,c));
 struct timespec delay={0,1000000};int n=0;while(!u222_fixture_started(data)&&n++<2000)nanosleep(&delay,NULL);assert(u222_fixture_started(data));
 drm_credential_revoke(c);assert(u222_fixture_held_valid(data));assert(!drm_credential_receipt(c,tag,1));u222_fixture_release(data);assert(!pthread_join(th,NULL));assert(!drm_credential_metric(c,1));puts("PASS inflight-cancel/borrow");
 c=make(5,&data);assert(submit(c)<0);assert(drm_credential_metric(c,0)==0);puts("PASS allocation failure");
 c=make(0,&data);assert(drm_credential_submit(c,tag,1,1,user,257,password,sizeof(password)-1,NULL,0,challenge,32)<0);assert(!drm_credential_metric(c,0));assert(drm_credential_real_provider()==NULL);puts("PASS bounds/production closed");
 return 0;
}
