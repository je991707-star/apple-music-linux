#define _POSIX_C_SOURCE 200809L
#include "../native/credential_response_bridge.h"
#include <assert.h>
#include <stdio.h>
#include <time.h>
#include <pthread.h>
void *u223_fixture_target(int);void *u223_fixture_bridge(void *,int);
int u223_fixture_valid(void *);int u223_fixture_calls(void *);int u223_fixture_password_size(void *);void u223_fixture_release(void *);
/* Separate cases model separate Engine processes; no production reset API. */
static struct drm_native_credential *fixture_context(const struct drm_credential_provider *p,void *data,const unsigned char *tag,uint64_t gen,int64_t end){struct drm_credential_process *process=drm_credential_test_process();assert(process);return drm_credential_new_scoped(process,p,data,tag,gen,end);}
#define drm_credential_new fixture_context
static unsigned char tag[32];static const char id[]="0123456789abcdef0123456789abcdef";
static struct drm_native_credential *create(void *data){struct timespec t;assert(!clock_gettime(CLOCK_REALTIME,&t));struct drm_native_credential *c=drm_credential_new(drm_response_bridge_provider(),data,tag,1,(int64_t)t.tv_sec*1000+t.tv_nsec/1000000+60000);assert(c);return c;}
static int credential(struct drm_native_credential *c){return drm_credential_submit(c,tag,1,1,"u220-user",9,"u220-synthetic-native",21,NULL,0,id,32);}
static void *worker(void *c){assert(credential(c)<0);return NULL;}
int main(void){
 void *t=u223_fixture_target(10);struct drm_native_credential *c=create(u223_fixture_bridge(t,10));assert(credential(c)==0);assert(u223_fixture_valid(t));assert(u223_fixture_calls(t)==1);drm_credential_revoke(c);assert(u223_fixture_valid(t));assert(!drm_credential_receipt(c,tag,1));puts("PASS original response backing remains live after return/revoke");
 t=u223_fixture_target(11);c=create(u223_fixture_bridge(t,11));assert(credential(c)==1);assert(u223_fixture_calls(t)==0);assert(drm_credential_submit(c,tag,1,2,NULL,0,NULL,0,"000001",6,id,32)==0);assert(u223_fixture_password_size(t)==27);assert(u223_fixture_valid(t));assert(drm_credential_submit(c,tag,1,2,NULL,0,NULL,0,"000001",6,id,32)<0);puts("PASS original six-digit response and replay rejection");
 t=u223_fixture_target(12);c=create(u223_fixture_bridge(t,12));pthread_t th;assert(!pthread_create(&th,NULL,worker,c));struct timespec delay={0,1000000};unsigned n=0;while(!u223_fixture_calls(t)&&n++<2000)nanosleep(&delay,NULL);assert(u223_fixture_calls(t)==1);drm_credential_revoke(c);assert(u223_fixture_valid(t));u223_fixture_release(t);assert(!pthread_join(th,NULL));assert(u223_fixture_valid(t));puts("PASS inflight C++ borrower survives cancellation");
 assert(!u223_fixture_bridge(u223_fixture_target(13),13));assert(!u223_fixture_bridge(u223_fixture_target(14),14));assert(drm_credential_real_provider()==NULL);puts("PASS UNKNOWN/CALL cannot qualify bridge; real Vendor closed");return 0;
}
