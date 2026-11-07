#define _POSIX_C_SOURCE 200809L
#include "credential_consumer.h"
#include "request_provenance.h"
#include <limits.h>
#ifdef _WIN32
#include <process.h>
#define owner_pid _getpid
#else
#include <unistd.h>
#ifdef __linux__
#include <sys/random.h>
#endif
#define owner_pid getpid
#endif
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
struct drm_credential_process { pthread_mutex_t lock; struct drm_native_credential *owner; int shutting; };
static struct drm_credential_process native_process={.lock=PTHREAD_MUTEX_INITIALIZER};
struct payload { struct payload *next; struct drm_owned_credential value; };
struct drm_native_credential {
 pthread_mutex_t lock; struct drm_credential_provider provider; void *provider_data;
 unsigned char tag[32]; uint64_t generation; int64_t deadline;
 int revoked,used,challenge_used,challenge_ready; char challenge[33]; unsigned active,retained,calls,receipts;
 struct payload *root; struct drm_credential_process *process; long birth_pid; unsigned requests;
};
static int64_t now_ms(void){struct timespec t;if(clock_gettime(CLOCK_REALTIME,&t))return INT64_MAX;return (int64_t)t.tv_sec*1000+t.tv_nsec/1000000;}
static void erase(void *p,size_t n){volatile unsigned char *q=p;while(n--)*q++=0;}
const struct drm_credential_provider *drm_credential_real_provider(void){return NULL;/* Borrow/release and original-callback ownership UNKNOWN. */}
int drm_credential_provider_qualified(const struct drm_credential_provider *p){return p&&p->abi==DRM_CREDENTIAL_PROVIDER_ABI&&((p->allocate==NULL)==(p->deallocate==NULL))&&(p->ownership==1||p->ownership==2)&&p->submit;}
struct drm_native_credential *drm_credential_new_scoped(struct drm_credential_process *process,const struct drm_credential_provider *p,void *data,const unsigned char *tag,uint64_t gen,int64_t deadline){
 if(!drm_credential_provider_qualified(p)||!tag||!gen||deadline<=now_ms())return NULL;
 struct drm_native_credential *c=calloc(1,sizeof(*c));if(!c)return NULL;
 if(pthread_mutex_init(&c->lock,NULL)){free(c);return NULL;}
 c->provider=*p;c->provider_data=data;memcpy(c->tag,tag,32);c->generation=gen;c->deadline=deadline;
 c->process=process?process:&native_process;c->birth_pid=(long)owner_pid();
 pthread_mutex_lock(&c->process->lock);
 if(c->process->owner||c->process->shutting){pthread_mutex_unlock(&c->process->lock);pthread_mutex_destroy(&c->lock);free(c);return NULL;}
 c->process->owner=c;pthread_mutex_unlock(&c->process->lock);return c;
}
struct drm_native_credential *drm_credential_new(const struct drm_credential_provider *p,void *data,const unsigned char *tag,uint64_t gen,int64_t end){return drm_credential_new_scoped(NULL,p,data,tag,gen,end);}
static int shutting(struct drm_native_credential *c){pthread_mutex_lock(&c->process->lock);int yes=c->process->shutting;pthread_mutex_unlock(&c->process->lock);return yes;}
void drm_credential_process_shutdown(struct drm_credential_process *p){if(!p)p=&native_process;pthread_mutex_lock(&p->lock);p->shutting=1;struct drm_native_credential *c=p->owner;pthread_mutex_unlock(&p->lock);if(c)drm_credential_revoke(c);}
#ifdef DRM_CREDENTIAL_FIXTURE_SCOPES
struct drm_credential_process *drm_credential_test_process(void){struct drm_credential_process *p=calloc(1,sizeof(*p));if(!p)return NULL;if(pthread_mutex_init(&p->lock,NULL)){free(p);return NULL;}return p;}
#endif
static int scope(struct drm_native_credential *c,const unsigned char *tag,uint64_t gen){return c->birth_pid==(long)owner_pid()&&!shutting(c)&&!c->revoked&&tag&&memcmp(c->tag,tag,32)==0&&c->generation==gen&&now_ms()<c->deadline;}
static int input(const void *p,size_t n,size_t max){return p&&n&&n<=max&&!memchr(p,0,n);}
int drm_credential_submit(struct drm_native_credential *c,const unsigned char *tag,uint64_t gen,int kind,const void *user,size_t un,const void *password,size_t pn,const void *reply,size_t rn,const char *challenge,size_t cn){
 if(!c||!tag)return -1;
 if(kind==1){if(!input(user,un,256)||!input(password,pn,255)||rn||!challenge||cn!=32)return -1;}
 else if(kind==2){if(un||pn||!input(reply,rn,6)||rn!=6||!challenge||cn!=32)return -1;for(size_t i=0;i<rn;i++)if(((const unsigned char *)reply)[i]<'0'||((const unsigned char *)reply)[i]>'9')return -1;}
 else return -1;
 pthread_mutex_lock(&c->lock);
 if(!scope(c,tag,gen)||c->active||c->retained>=32||(kind==1?c->used:!c->used||!c->challenge_ready||c->challenge_used||memcmp(c->challenge,challenge,32))){pthread_mutex_unlock(&c->lock);return -2;}
 struct payload *p=c->provider.allocate?c->provider.allocate(sizeof(*p)):calloc(1,sizeof(*p));
 if(!p){c->revoked=1;pthread_mutex_unlock(&c->lock);return -4;}
 memset(p,0,sizeof(*p));p->value.user_len=un;p->value.password_len=pn;p->value.reply_len=rn;
 if(un)memcpy(p->value.user,user,un);
 if(pn)memcpy(p->value.password,password,pn);
 if(rn)memcpy(p->value.reply,reply,rn);
 if(kind==1)c->used=1;else c->challenge_used=1;
 if(c->provider.ownership==2){p->next=c->root;c->root=p;c->retained++;}
 c->active++;c->calls++;pthread_mutex_unlock(&c->lock);
 int result=c->provider.submit(c,&p->value,c->provider_data);
 pthread_mutex_lock(&c->lock);c->active--;
 if(!scope(c,tag,gen))result=-2;
 else if(result<0||(kind==2&&result!=0)||result>1){c->revoked=1;result=-5;}
 else if(result==1){memcpy(c->challenge,challenge,32);c->challenge_ready=1;}
 pthread_mutex_unlock(&c->lock);
 if(c->provider.ownership==1){erase(p,sizeof(*p));if(c->provider.deallocate)c->provider.deallocate(p,sizeof(*p));else free(p);}
 return result;
}
void drm_credential_revoke(struct drm_native_credential *c){if(!c)return;pthread_mutex_lock(&c->lock);c->revoked=1;memset(c->challenge,0,sizeof(c->challenge));pthread_mutex_unlock(&c->lock);}
/* A receipt is only a bounded progress probe. Never calls Go or publishes Ready. */
int drm_credential_receipt(struct drm_native_credential *c,const unsigned char *tag,uint64_t gen){(void)c;(void)tag;(void)gen;drm_callback_unknown(DRM_STAGE_RECEIPT);return 0;}
unsigned drm_credential_metric(struct drm_native_credential *c,int kind){if(!c)return 0;pthread_mutex_lock(&c->lock);unsigned n=kind==0?c->calls:kind==1?c->active:kind==2?c->retained:c->receipts;pthread_mutex_unlock(&c->lock);return n;}
/* Contexts/PROCESS payloads deliberately have no early-free entry point. Unknown
 * asynchronous borrowers must not outlive freed storage. Process exit reclaims them. */

int drm_credential_live(struct drm_native_credential *c){if(!c)return 0;pthread_mutex_lock(&c->lock);int yes=c->birth_pid==(long)owner_pid()&&!shutting(c)&&!c->revoked&&now_ms()<c->deadline;pthread_mutex_unlock(&c->lock);return yes;}

struct drm_native_request { struct drm_native_request *next; struct drm_native_credential *origin; unsigned local_number; unsigned char nonce[16]; int consumed; };
static pthread_mutex_t request_lock=PTHREAD_MUTEX_INITIALIZER;
static struct drm_native_request *request_registry;
static unsigned diagnostics[7][3];
static void record(enum drm_callback_reason why,enum drm_callback_stage stage){pthread_mutex_lock(&request_lock);if(diagnostics[why][stage]!=UINT_MAX)diagnostics[why][stage]++;pthread_mutex_unlock(&request_lock);}
void drm_callback_unknown(enum drm_callback_stage stage){if(stage<0||stage>DRM_STAGE_RECEIPT)return;record(DRM_UNKNOWN_ORIGIN,stage);}
unsigned drm_callback_diagnostic(enum drm_callback_reason why,enum drm_callback_stage stage){if(why<0||why>DRM_SHUTDOWN_IN_PROGRESS||stage<0||stage>DRM_STAGE_RECEIPT)return 0;pthread_mutex_lock(&request_lock);unsigned n=diagnostics[why][stage];pthread_mutex_unlock(&request_lock);return n;}
struct drm_native_request *drm_request_begin(struct drm_native_credential *c){
 if(!c)return NULL;pthread_mutex_lock(&c->lock);
 if(!scope(c,c->tag,c->generation)||c->requests>=2){pthread_mutex_unlock(&c->lock);return NULL;}
 struct drm_native_request *r=calloc(1,sizeof(*r));if(!r){pthread_mutex_unlock(&c->lock);return NULL;}
 #ifdef __linux__
 if(getrandom(r->nonce,sizeof(r->nonce),GRND_NONBLOCK)!=(long)sizeof(r->nonce)){free(r);pthread_mutex_unlock(&c->lock);return NULL;}
#else
 free(r);pthread_mutex_unlock(&c->lock);return NULL; /* Unqualified platform: fail closed. */
#endif
 r->origin=c;r->local_number=++c->requests;pthread_mutex_unlock(&c->lock);
 pthread_mutex_lock(&request_lock);r->next=request_registry;request_registry=r;pthread_mutex_unlock(&request_lock);return r;
}
enum drm_callback_reason drm_request_callback(struct drm_native_credential *receiver,const struct drm_request_proof *proof){
 /* Address membership BEFORE dereference. No current-attempt lookup or raw
  * Vendor request-pointer mapping. Tickets remain rooted through process exit. */
 pthread_mutex_lock(&request_lock);struct drm_native_request *r=request_registry;while(r&&(!proof||r!=proof->ticket))r=r->next;pthread_mutex_unlock(&request_lock);
 enum drm_callback_reason why=DRM_UNKNOWN_ORIGIN;
 if(r&&proof&&memcmp(r->nonce,proof->nonce,16)==0&&receiver){
  pthread_mutex_lock(&receiver->lock);
  if(r->origin!=receiver||receiver->birth_pid!=(long)owner_pid())why=DRM_STALE_ENGINE;
  else if(shutting(receiver))why=DRM_SHUTDOWN_IN_PROGRESS;
  else if(now_ms()>=receiver->deadline)why=DRM_EXPIRED_REQUEST;
  else if(receiver->revoked)why=DRM_REVOKED_SCOPE;
  else if(r->consumed)why=DRM_DUPLICATE_CALLBACK;
  else{r->consumed=1;receiver->receipts++;why=DRM_QUALIFIED_ORIGIN;}
  pthread_mutex_unlock(&receiver->lock);
 }
 record(why,DRM_STAGE_LOCAL_REQUEST);return why;
}

int drm_request_capture(const struct drm_native_request *ticket,struct drm_request_proof *out){
 if(!out)return 0;pthread_mutex_lock(&request_lock);struct drm_native_request *r=request_registry;while(r&&r!=ticket)r=r->next;
 if(r){out->ticket=r;memcpy(out->nonce,r->nonce,16);}pthread_mutex_unlock(&request_lock);return r!=NULL;
}
