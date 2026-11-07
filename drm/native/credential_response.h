/* Private response materialization, shared by the original callback and owned ingress.
 * This describes the existing wrapper layout, NOT a verified Vendor ABI. */
#ifndef ADAM_CREDENTIAL_RESPONSE_H
#define ADAM_CREDENTIAL_RESPONSE_H
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#ifndef IMPORT_H_NO_DATA_DEFS
#define IMPORT_H_NO_DATA_DEFS
#endif
#include "import.h"
struct drm_response_owner {
 char user[257],password[262];
 union std_string user_value,password_value;
 struct shared_ptr response;
};
struct drm_request_proof;
struct drm_response_ops {
 /* Only a controlled, explicitly qualified implementation may be used by the
  * new ingress bridge. Existing legacy callers do not qualify a Vendor. */
 unsigned contract; /* 2 = borrower neither releases nor outlives process storage */
 void *control_vtable;
 void (*construct)(void *);
 void (*username)(void *,union std_string *);
 void (*password)(void *,union std_string *);
 void (*type)(void *,int);
 void (*deliver)(void *,struct shared_ptr *);
 void (*observe_request)(void *,const struct drm_request_proof *);
};
static inline int drm_response_ops_present(const struct drm_response_ops *ops){return ops&&ops->control_vtable&&ops->construct&&ops->username&&ops->password&&ops->type&&ops->deliver;}
static inline int drm_response_materialize(struct drm_response_owner *o,const struct drm_response_ops *ops,void *target,int (*publish)(void *),void *token){
 if(!o||!drm_response_ops_present(ops)||!publish)return -1;
 unsigned char *ptr=(unsigned char *)calloc(1,80);if(!ptr)return -1;
 if(!publish(token)){free(ptr);return -1;}
 /* Published backing/control/object are retained; no destructor is guessed. */
 o->user_value=new_std_string(o->user);o->password_value=new_std_string(o->password);
 *(void **)ptr=ops->control_vtable;
 o->response=(struct shared_ptr){.obj=ptr+24,.ctrl_blk=ptr};
 ops->construct(o->response.obj);ops->username(o->response.obj,&o->user_value);
 ops->password(o->response.obj,&o->password_value);ops->type(o->response.obj,2);
 ops->deliver(target,&o->response);return 0;
}
#endif
