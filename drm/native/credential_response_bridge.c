#include "credential_response_bridge.h"
#include <pthread.h>
#include "request_provenance.h"
struct response_node { struct response_node *next; struct drm_response_owner storage; };
struct drm_response_bridge {
 pthread_mutex_t lock; struct drm_response_ops ops; void *target;
 struct drm_native_credential *origin; struct drm_native_request *request; struct response_node *root,*base;
 unsigned count; int need_code;
};
struct drm_response_bridge *drm_response_bridge_new(const struct drm_response_ops *ops,void *target,int need_code){
 if(!drm_response_ops_present(ops)||ops->contract!=2||!ops->observe_request||!target)return NULL;
 struct drm_response_bridge *b=calloc(1,sizeof(*b));if(!b)return NULL;
 if(pthread_mutex_init(&b->lock,NULL)){free(b);return NULL;}
 b->ops=*ops;b->target=target;b->need_code=!!need_code;return b;
}
static int publish_response(void *token){return drm_credential_live(token);}
static int bridge_submit(struct drm_native_credential *ctx,const struct drm_owned_credential *in,void *data){
 struct drm_response_bridge *b=data;if(!b||!drm_credential_live(ctx))return -1;
 pthread_mutex_lock(&b->lock);
 if((b->origin&&b->origin!=ctx)||b->count>=2||(!in->user_len&&!b->base)||(in->user_len&&b->base)){
  pthread_mutex_unlock(&b->lock);return -1;
 }
 if(in->user_len>256||in->password_len>255||in->reply_len>6){pthread_mutex_unlock(&b->lock);return -1;}
 struct response_node *p=calloc(1,sizeof(*p));if(!p){pthread_mutex_unlock(&b->lock);return -1;}
 if(in->user_len){
  memcpy(p->storage.user,in->user,in->user_len);memcpy(p->storage.password,in->password,in->password_len);
 }else{
  if(!b->need_code||in->reply_len!=6){free(p);pthread_mutex_unlock(&b->lock);return -1;}
  for(size_t i=0;i<6;i++)if(in->reply[i]<'0'||in->reply[i]>'9'){free(p);pthread_mutex_unlock(&b->lock);return -1;}
  memcpy(p->storage.user,b->base->storage.user,sizeof(p->storage.user));
  size_t bare=strlen(b->base->storage.password);memcpy(p->storage.password,b->base->storage.password,bare);
  memcpy(p->storage.password+bare,in->reply,6);
 }
 b->origin=ctx;p->next=b->root;b->root=p;b->count++;if(in->user_len)b->base=p;
 int challenge=b->need_code&&in->user_len;pthread_mutex_unlock(&b->lock);
 if(challenge)return 1;
 b->request=drm_request_begin(ctx);if(!b->request)return -1;
 struct drm_request_proof proof; if(!drm_request_capture(b->request,&proof))return -1;
 b->ops.observe_request(b->target,&proof);
 /* Root before any C++ exposure. Inflight borrowers are never freed on revoke. */
 return drm_response_materialize(&p->storage,&b->ops,b->target,publish_response,ctx);
}
static const struct drm_credential_provider bridge_provider={DRM_CREDENTIAL_PROVIDER_ABI,2,bridge_submit,NULL,NULL};
const struct drm_credential_provider *drm_response_bridge_provider(void){return &bridge_provider;}
