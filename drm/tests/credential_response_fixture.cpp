/* Controlled substitute for the existing C++ function signatures, not Apple ABI proof. */
#include "../native/credential_response_bridge.h"
#include <pthread.h>
#include "../native/request_provenance.h"
#include <cstring>
struct fixture { pthread_mutex_t lock; pthread_cond_t cond; int mode,entered,released,delivered; struct shared_ptr *held; struct drm_request_proof proof; };
struct object { union std_string *user,*password; int type; };
static void construct(void *p){memset(p,0,sizeof(object));}
static void username(void *p,union std_string *v){((object*)p)->user=v;}
static void password(void *p,union std_string *v){((object*)p)->password=v;}
static void type(void *p,int n){((object*)p)->type=n;}
static void deliver(void *target,struct shared_ptr *value){
 fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);f->held=value;f->entered=1;f->delivered++;
 while(f->mode==12&&!f->released)pthread_cond_wait(&f->cond,&f->lock);
 pthread_mutex_unlock(&f->lock);
}
static void observe(void *target,const struct drm_request_proof *proof){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);f->proof=*proof;pthread_mutex_unlock(&f->lock);}
extern "C" {
void *u223_fixture_target(int mode){fixture *f=(fixture*)calloc(1,sizeof(*f));if(!f)return NULL;pthread_mutex_init(&f->lock,NULL);pthread_cond_init(&f->cond,NULL);f->mode=mode;return f;}
const struct drm_credential_provider *u223_fixture_provider(void){return drm_response_bridge_provider();}
void *u223_fixture_bridge(void *target,int mode){static int table;struct drm_response_ops ops={2,&table,construct,username,password,type,deliver,observe};
 if(mode==13)ops.contract=0;if(mode==14)ops.contract=1;
 return drm_response_bridge_new(&ops,target,mode==11);
}
int u223_fixture_valid(void *target){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);int ok=0;
 if(f->held){object *o=(object*)f->held->obj;ok=o->type==2&&o->user&&o->password&&o->user->size>=5&&o->password->size>=15&&memcmp(o->user->data,"u220-",5)==0&&memcmp(o->password->data,"u220-synthetic-",15)==0;}
 pthread_mutex_unlock(&f->lock);return ok;
}
int u223_fixture_calls(void *target){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);int n=f->delivered;pthread_mutex_unlock(&f->lock);return n;}
int u223_fixture_password_size(void *target){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);int n=f->held?(int)((object*)f->held->obj)->password->size:0;pthread_mutex_unlock(&f->lock);return n;}
void u223_fixture_release(void *target){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);f->released=1;pthread_cond_broadcast(&f->cond);pthread_mutex_unlock(&f->lock);}
int u224_callback(void *target,struct drm_native_credential *receiver){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);struct drm_request_proof proof=f->proof;pthread_mutex_unlock(&f->lock);return (int)drm_request_callback(receiver,&proof);}
int u224_unknown(struct drm_native_credential *receiver){struct drm_request_proof proof={(const struct drm_native_request *)(uintptr_t)1,{0}};return (int)drm_request_callback(receiver,&proof);}
int u224_wrong_nonce(void *target,struct drm_native_credential *receiver){fixture *f=(fixture*)target;pthread_mutex_lock(&f->lock);struct drm_request_proof proof=f->proof;pthread_mutex_unlock(&f->lock);proof.nonce[0]^=1;return (int)drm_request_callback(receiver,&proof);}
int u224_pointer_reuse(void *old_target,void *new_target,struct drm_native_credential *receiver){fixture *old=(fixture*)old_target,*current=(fixture*)new_target;pthread_mutex_lock(&old->lock);struct drm_request_proof proof=old->proof;pthread_mutex_unlock(&old->lock);pthread_mutex_lock(&current->lock);proof.ticket=current->proof.ticket;pthread_mutex_unlock(&current->lock);return (int)drm_request_callback(receiver,&proof);}
}
