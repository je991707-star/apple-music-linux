/*
 * drm_lib.c — in-process C API implementation.
 *
 * Built with -DDRM_LIB_BUILD alongside main.c (which guards int main() with
 * #ifndef DRM_LIB_BUILD).  The result is libdrm-native.so, loaded by the Go
 * engine via CGO instead of forking drm-native as a subprocess.
 */

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

#define IMPORT_H_NO_DATA_DEFS
#include "import.h"
#include "drm_lib.h"
#include "cmdline.h"

#ifdef _WIN32
static int drm_windows_setenv(const char *name, const char *value, int overwrite)
{
    if (!overwrite && getenv(name) != NULL) return 0;
    return _putenv_s(name, value != NULL ? value : "");
}
#define setenv drm_windows_setenv
#endif

/* ── Externs from hybris_stubs.c ────────────────────────────────────────────*/

extern uint8_t endLeaseCallback[32];
extern uint8_t pbErrCallback[32];

/* ── Forward declarations for functions in main.c ──────────────────────────*/

/* init helpers (defined in main.c, de-staticized) */
extern void              drm_init_internal(void);
extern struct shared_ptr drm_init_ctx(void);

/* globals in main.c (de-staticized) */
extern struct shared_ptr apInf;
extern uint8_t           leaseMgr[16];
extern struct shared_ptr reqCtx;
extern struct gengetopt_args_info args_info;
extern char             *amUsername, *amPassword;
extern size_t            amPasswordLen;
extern int               decryptCount;
extern int               offlineFlag;
extern char             *device_infos[9];
extern void             *FHinstance;
extern void             *preshareCtx;
extern struct shared_ptr g_itun_decryptor;
extern unsigned long     g_itun_adam_id;
extern pthread_mutex_t   g_itun_mutex;

extern drm_auth_cb_t  g_drm_auth_cb;
extern void          *g_drm_auth_ud;
extern drm_state_cb_t g_drm_state_cb;
extern void          *g_drm_state_ud;

/* init/codec helpers in main.c / hybris_stubs.c */
extern void hybris_init_callbacks(void);
extern void start_recovery_thread(void);
extern int hybris_init_libs(const char *lib64_path);
#ifdef _WIN32
extern void hybris_guest_fs_enter(void);
extern void hybris_guest_fs_leave(void);
#endif

/* account helpers */
extern char *get_account_storefront_id(struct shared_ptr reqCtx);
extern char *get_dev_token(struct shared_ptr reqCtx);
extern char *get_music_user_token(char *guid, char *authToken, struct shared_ptr reqCtx);
extern char *get_guid(void);
extern void  write_storefront_id(void);
extern void  write_music_token(void);
extern int   offline_available(void);

/* decrypt helpers */
extern void *getKdContext(const char *adam, const char *uri);

/* m3u8 / progressive helpers */
extern const char *get_m3u8_method_play(uint8_t leaseMgr[16], unsigned long adamID, char **dk);
extern const char *get_m3u8_method_download(struct shared_ptr reqCtx, unsigned long adamID, char **dk);
extern const char *get_progressive_method_play(uint8_t leaseMgr[16], unsigned long adamID, char **dk);

/* NfcRKVnxuKZy04KWbdFu71Ou and _ZN17SVPastisDecryptor13decryptSampleEPKhRKjPj
 * are declared by import.h (included above with IMPORT_H_NO_DATA_DEFS). */

/* cached account info (in main.c) */
extern char *g_storefront_id;
extern char *g_dev_token;
extern char *g_music_token;

/* ── Module state ───────────────────────────────────────────────────────────*/

static int             g_init_result = 0;  /* 0 = success, -1 = failed */
static pthread_mutex_t g_init_mutex  = PTHREAD_MUTEX_INITIALIZER;

/* Owned strings and callback gate. Paths and device backing are never freed or
 * overwritten: Vendor quiescence is unknown. Only one bind is allowed per exec.
 * Submitted string objects also remain rooted until process exit because the
 * setters' retention contract is unknown. This is bounded retention, not erase. */
#include "credential_owner.h"
#define OWNER_PATH_MAX 4095
#define OWNER_DEVICE_MAX 1024
#define OWNER_USER_MAX 256
#define OWNER_PASSWORD_MAX 255
#define OWNER_PAYLOAD_MAX 32
struct drm_credential_payload {
    struct drm_credential_payload *next;
    struct drm_response_owner storage;
    drm_auth_cb_t auth;
    void *ud;
    int published;
};
static struct {
    pthread_mutex_t lock;
    pthread_cond_t drained;
    unsigned callbacks, payloads;
    int bound, revoked, credentials;
    char base[OWNER_PATH_MAX + 1], lib64[OWNER_PATH_MAX + 1];
    char device[OWNER_DEVICE_MAX + 1], parts[OWNER_DEVICE_MAX + 1];
    char user[OWNER_USER_MAX + 1], password[OWNER_PASSWORD_MAX + 1];
    drm_auth_cb_t auth;
    void *auth_ud;
    drm_state_cb_t state;
    void *state_ud;
    struct drm_credential_payload *retained;
} owner = { .lock = PTHREAD_MUTEX_INITIALIZER, .drained = PTHREAD_COND_INITIALIZER };
static _Thread_local unsigned owner_callback_depth;
static void owner_zero(void *data, size_t n) {
    volatile unsigned char *p = data;
    while (n--) *p++ = 0;
}
static void owner_erase_credentials(void) {
    owner.credentials = 0;
    owner_zero(owner.user, sizeof(owner.user));
    owner_zero(owner.password, sizeof(owner.password));
    amPasswordLen = 0;
}
static int owner_string(const char *s, size_t max) {
    return s && strnlen(s, max + 1) <= max;
}
int drm_owner_begin(const drm_lib_config_t *in, drm_lib_config_t *out) {
    static const char default_device[] = "Music/4.9/Android/10/Samsung S9/7663313/en-US/en-US/dc28071e981c439e";
    if (!in || !out) return -1;
    const char *device = in->device_info && in->device_info[0] ? in->device_info : default_device;
    if (!owner_string(in->base_dir, OWNER_PATH_MAX) || !in->base_dir[0] ||
        (in->lib64_dir && !owner_string(in->lib64_dir, OWNER_PATH_MAX)) ||
        !owner_string(device, OWNER_DEVICE_MAX)) return -1;
    if ((in->username != NULL) != (in->password != NULL)) return -1;
    if (in->username && (!owner_string(in->username, OWNER_USER_MAX) || !in->username[0] ||
        !owner_string(in->password, OWNER_PASSWORD_MAX) || !in->password[0])) return -1;
    unsigned fields = 1;
    for (const char *p = device; *p; ++p) if (*p == '/') {
        if (p == device || p[-1] == '/' || !p[1]) return -1;
        fields++;
    }
    if (fields != 9) return -1;
    pthread_mutex_lock(&owner.lock);
    if (owner.bound) { pthread_mutex_unlock(&owner.lock); return -1; }
    owner.bound = 1;
    strcpy(owner.base, in->base_dir);
    if (in->lib64_dir) strcpy(owner.lib64, in->lib64_dir);
    strcpy(owner.device, device); strcpy(owner.parts, device);
    owner.auth = in->auth_cb; owner.auth_ud = in->auth_ud;
    owner.state = in->state_cb; owner.state_ud = in->state_ud;
    if (in->username) {
        strcpy(owner.user, in->username); strcpy(owner.password, in->password);
        owner.credentials = 1;
    }
    amUsername = owner.user; amPassword = owner.password;
    amPasswordLen = strlen(owner.password);
    memset(&args_info, 0, sizeof(args_info));
    args_info.base_dir_arg = owner.base; args_info.base_dir_given = 1;
    args_info.device_info_arg = owner.device;
    args_info.device_info_given = in->device_info && in->device_info[0];
    char *part = owner.parts;
    for (unsigned i = 0; i < 9; ++i) {
        device_infos[i] = part;
        char *slash = strchr(part, '/');
        if (slash) { *slash = 0; part = slash + 1; }
    }
    *out = *in;
    out->base_dir = owner.base;
    out->lib64_dir = in->lib64_dir ? owner.lib64 : NULL;
    out->device_info = owner.device;
    out->username = in->username ? owner.user : NULL;
    out->password = in->password ? owner.password : NULL;
    pthread_mutex_unlock(&owner.lock);
    return 0;
}
int drm_owner_active(void) {
    pthread_mutex_lock(&owner.lock);
    int yes = owner.bound && !owner.revoked;
    pthread_mutex_unlock(&owner.lock); return yes;
}
void drm_owner_fail(void) {
    pthread_mutex_lock(&owner.lock);
    if (owner.bound) {
        owner.revoked = 1; owner_erase_credentials();
        owner.auth = NULL; owner.auth_ud = NULL;
        owner.state = NULL; owner.state_ud = NULL;
    }
    pthread_mutex_unlock(&owner.lock);
}
void drm_owner_cancel(void) {
    pthread_mutex_lock(&owner.lock);
    if (owner.bound) {
        owner.revoked = 1;
        owner_erase_credentials();
        owner.auth = NULL; owner.auth_ud = NULL;
        owner.state = NULL; owner.state_ud = NULL;
        /* A callback may cancel itself; its static owner/payload remains live.
         * Other callers wait for all entered users before releasing userdata. */
        while (owner_callback_depth == 0 && owner.callbacks != 0)
            pthread_cond_wait(&owner.drained, &owner.lock);
    }
    pthread_mutex_unlock(&owner.lock);
}
int drm_owner_complete(void) {
    pthread_mutex_lock(&owner.lock);
    int yes = owner.bound && !owner.revoked;
    owner_erase_credentials();
    owner.auth = NULL; owner.auth_ud = NULL;
    pthread_mutex_unlock(&owner.lock); return yes;
}
int drm_owner_state(const char *state) {
    pthread_mutex_lock(&owner.lock);
    if (!owner.bound || owner.revoked) { pthread_mutex_unlock(&owner.lock); return 0; }
    drm_state_cb_t cb = owner.state; void *ud = owner.state_ud;
    if (cb) { owner.callbacks++; owner_callback_depth++; }
    pthread_mutex_unlock(&owner.lock);
    if (cb) {
        cb(state, ud);
        pthread_mutex_lock(&owner.lock);
        owner.callbacks--; owner_callback_depth--;
        pthread_cond_broadcast(&owner.drained);
        pthread_mutex_unlock(&owner.lock);
    }
    return drm_owner_active();
}
struct drm_credential_payload *drm_credential_enter(void) {
    pthread_mutex_lock(&owner.lock);
    if (!owner.bound || owner.revoked || !owner.credentials ||
        owner.payloads + owner.callbacks >= OWNER_PAYLOAD_MAX) {
        if (owner.credentials && owner.payloads + owner.callbacks >= OWNER_PAYLOAD_MAX) {
            owner.revoked = 1; owner_erase_credentials();
        }
        pthread_mutex_unlock(&owner.lock); return NULL;
    }
    struct drm_credential_payload *p = calloc(1, sizeof(*p));
    if (!p) { owner.revoked = 1; owner_erase_credentials(); pthread_mutex_unlock(&owner.lock); return NULL; }
    strcpy(p->storage.user, owner.user); strcpy(p->storage.password, owner.password);
    p->auth = owner.auth; p->ud = owner.auth_ud;
    owner.callbacks++; owner_callback_depth++;
    pthread_mutex_unlock(&owner.lock); return p;
}
int drm_credential_code(struct drm_credential_payload *p) {
    char code[16] = {0};
    if (!p->auth || !drm_owner_active()) return -1;
    p->auth("2fa", code, (int)sizeof(code), p->ud);
    size_t n = strnlen(code, sizeof(code));
    int valid = n == 6 && drm_owner_active();
    for (size_t i = 0; valid && i < n; ++i) valid = code[i] >= '0' && code[i] <= '9';
    if (valid) {
        size_t bare = strnlen(p->storage.password, sizeof(p->storage.password));
        if (bare > OWNER_PASSWORD_MAX) { owner_zero(code, sizeof(code)); return -1; }
        memcpy(p->storage.password + bare, code, 6); p->storage.password[bare + 6] = 0;
    }
    owner_zero(code, sizeof(code));
    return valid ? 0 : -1;
}
int drm_credential_publish(struct drm_credential_payload *p) {
    pthread_mutex_lock(&owner.lock);
    int valid = owner.bound && !owner.revoked && owner.credentials;
    if (valid) {
        p->storage.user_value = new_std_string(p->storage.user);
        p->storage.password_value = new_std_string(p->storage.password);
        p->auth = NULL; p->ud = NULL;
        p->published = 1; p->next = owner.retained; owner.retained = p; owner.payloads++;
    }
    pthread_mutex_unlock(&owner.lock); return valid;
}
struct drm_response_owner *drm_credential_storage(struct drm_credential_payload *p) { return &p->storage; }
struct shared_ptr *drm_credential_response(struct drm_credential_payload *p) { return &p->storage.response; }
union std_string *drm_credential_username(struct drm_credential_payload *p) { return &p->storage.user_value; }
union std_string *drm_credential_password(struct drm_credential_payload *p) { return &p->storage.password_value; }
void drm_credential_leave(struct drm_credential_payload *p) {
    int published = p->published;
    pthread_mutex_lock(&owner.lock);
    owner.callbacks--; owner_callback_depth--;
    pthread_cond_broadcast(&owner.drained);
    pthread_mutex_unlock(&owner.lock);
    if (!published) { owner_zero(p, sizeof(*p)); free(p); }
}

/* ── drm_lib_init ───────────────────────────────────────────────────────────*/

int drm_lib_init(const drm_lib_config_t *cfg)
{
    drm_lib_config_t owned;
    if (drm_owner_begin(cfg, &owned) != 0) return -1;
    cfg = &owned;
    pthread_mutex_lock(&g_init_mutex);

    /* Callbacks live only behind the owning gate, never in mutable globals. */

    /* The wrapper keeps its own handles for symbol forwarding.  Initialize
     * those handles here; hybris_init_libs() is idempotent, so this reuses the
     * namespace already opened by the backend rather than reloading it. */
    if (cfg->lib64_dir && cfg->lib64_dir[0]) {
        setenv("HYBRIS_ANDROID_LIB64", cfg->lib64_dir, 1);
        if (hybris_init_libs(cfg->lib64_dir) != 0) {
            g_init_result = -1;
        drm_owner_cancel();
            pthread_mutex_unlock(&g_init_mutex);
            return -1;
        }
    }

    /* args_info/device backing and credentials are owned by drm_owner_begin. */
    offlineFlag = cfg->offline_only ? 1 : 0;

    /*
     * Run the exact same sequence as main() up to RUNNING state.
     * hybris_init_libs() was already called by hybris_ctor.c's __attribute__((constructor)).
     */
#ifdef _WIN32
    /* q.so rewrites normal guest TLS accesses to its Windows TLS backend;
     * leave the host TEB installed while entering the wrapper. */
    hybris_guest_fs_leave();
#endif
    drm_init_internal();
    hybris_init_callbacks();

    reqCtx = drm_init_ctx();
    if (reqCtx.obj == NULL) {
        fprintf(stderr, "[drm_lib] drm_init_ctx failed\n");
        g_init_result = -1;
        drm_owner_cancel();
#ifdef _WIN32
        hybris_guest_fs_leave();
#endif
        pthread_mutex_unlock(&g_init_mutex);
        return -1;
    }

    /* Login if credentials supplied */
    extern uint8_t login(struct shared_ptr ctx);
    if (cfg->username && cfg->password) {
        if (!login(reqCtx)) {
            fprintf(stderr, "[drm_lib] login failed\n");
            g_init_result = -1;
        drm_owner_cancel();
#ifdef _WIN32
            hybris_guest_fs_leave();
#endif
            pthread_mutex_unlock(&g_init_mutex);
            return -1;
        }
    }

    if (!drm_owner_active()) {
        g_init_result = -1; drm_owner_cancel(); pthread_mutex_unlock(&g_init_mutex); return -1;
    }

    /* Lease */
    DRM_DEBUG("[drm_lib] lease ctor\n");
    _ZN22SVPlaybackLeaseManagerC2ERKNSt6__ndk18functionIFvRKiEEERKNS1_IFvRKNS0_10shared_ptrIN17storeservicescore19StoreErrorConditionEEEEEE(
        leaseMgr, &endLeaseCallback, &pbErrCallback);
    uint8_t autom = 1;
    DRM_DEBUG("[drm_lib] refreshLeaseAutomatically\n");
    _ZN22SVPlaybackLeaseManager25refreshLeaseAutomaticallyERKb(leaseMgr, &autom);
    DRM_DEBUG("[drm_lib] requestLease\n");
    _ZN22SVPlaybackLeaseManager12requestLeaseERKb(leaseMgr, &autom);
    DRM_DEBUG("[drm_lib] requestLease returned\n");

    DRM_DEBUG("[drm_lib] FHinstance\n");
    FHinstance = _ZN21SVFootHillSessionCtrl8instanceEv();
    DRM_DEBUG("[drm_lib] start_recovery_thread\n");
    start_recovery_thread();
    DRM_DEBUG("[drm_lib] offline_available\n");

    offlineFlag = offline_available();
    DRM_DEBUG("[drm_lib] offline=%d, get_storefront_id\n", offlineFlag);

    /* Cache account tokens */
    g_storefront_id = get_account_storefront_id(reqCtx);
    if (!g_storefront_id) {
        fprintf(stderr, "[drm_lib] failed to get storefront ID\n");
        g_init_result = -1;
        drm_owner_cancel();
#ifdef _WIN32
        hybris_guest_fs_leave();
#endif
        pthread_mutex_unlock(&g_init_mutex);
        return -1;
    }
    DRM_DEBUG("[drm_lib] storefront_id=%s, get_dev_token\n", g_storefront_id);
    g_dev_token = get_dev_token(reqCtx);
#ifdef _WIN32
    if (!g_dev_token && cfg->base_dir && !g_music_token) {
        /* The device-token endpoint can be unavailable while an authenticated
         * Music token from the app session is still usable. Try that cache
         * before failing init. */
        char tok_path[1024];
        snprintf(tok_path, sizeof(tok_path), "%s/MUSIC_TOKEN", cfg->base_dir);
        FILE *tf = fopen(tok_path, "r");
        if (tf) {
            char buf[8192];
            size_t n = fread(buf, 1, sizeof(buf) - 1, tf);
            fclose(tf);
            buf[n] = '\0';
            while (n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r' || buf[n-1] == ' ')) buf[--n] = '\0';
            if (n > 0) {
                g_music_token = strdup(buf);
                fprintf(stderr, "[drm_lib] dev token unavailable; using cached MUSIC_TOKEN (%zu bytes)\n", n);
            }
        }
    }
    if (!g_dev_token && !g_music_token) {
        fprintf(stderr, "[drm_lib] failed to get dev token and no cached MUSIC_TOKEN is available\n");
        g_init_result = -1;
        drm_owner_cancel();
#ifdef _WIN32
        hybris_guest_fs_leave();
#endif
        pthread_mutex_unlock(&g_init_mutex);
        return -1;
    }
    if (g_dev_token) {
        DRM_DEBUG("[drm_lib] dev_token ok, get_music_token\n");
        g_music_token = get_music_user_token(get_guid(), g_dev_token, reqCtx);
    }
#else
    if (!g_dev_token) {
        fprintf(stderr, "[drm_lib] failed to get dev token\n");
        g_init_result = -1;
        drm_owner_cancel();
        pthread_mutex_unlock(&g_init_mutex);
        return -1;
    }
    DRM_DEBUG("[drm_lib] dev_token ok, get_music_token\n");
    g_music_token = get_music_user_token(get_guid(), g_dev_token, reqCtx);
#endif
    if (!g_music_token && cfg->base_dir) {
        /* createMusicToken failed (session may be expired) — fall back to the
         * MUSIC_TOKEN stored in drm/files/ by the web-session path. */
        char tok_path[1024];
        snprintf(tok_path, sizeof(tok_path), "%s/MUSIC_TOKEN", cfg->base_dir);
        FILE *tf = fopen(tok_path, "r");
        if (tf) {
            char buf[8192];
            size_t n = fread(buf, 1, sizeof(buf) - 1, tf);
            fclose(tf);
            buf[n] = '\0';
            while (n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r' || buf[n-1] == ' ')) buf[--n] = '\0';
            if (n > 0) {
                g_music_token = strdup(buf);
                DRM_DEBUG("[drm_lib] music_token: using stored token from disk (%zu bytes)\n", n);
            }
        }
    }
    if (!g_music_token) {
        fprintf(stderr, "[drm_lib] failed to get music token\n");
        g_init_result = -1;
        drm_owner_cancel();
#ifdef _WIN32
        hybris_guest_fs_leave();
#endif
        pthread_mutex_unlock(&g_init_mutex);
        return -1;
    }
    DRM_DEBUG("[drm_lib] music_token ok\n");

    if (cfg->base_dir) {
        DRM_DEBUG("[drm_lib] write_storefront_id\n");
        write_storefront_id();
        DRM_DEBUG("[drm_lib] write_music_token\n");
        write_music_token();
    }
    DRM_DEBUG("[drm_lib] calling state_cb RUNNING\n");

    if (!drm_owner_complete()) {
        g_init_result = -1; drm_owner_cancel(); pthread_mutex_unlock(&g_init_mutex); return -1;
    }
    if (!drm_owner_state("RUNNING")) {
        g_init_result = -1;
        drm_owner_cancel();
        pthread_mutex_unlock(&g_init_mutex);
        return -1;
    }

    g_init_result = 0;
#ifdef _WIN32
    hybris_guest_fs_leave();
#endif
    pthread_mutex_unlock(&g_init_mutex);
    return 0;
}

/* ── drm_lib_login ──────────────────────────────────────────────────────────*/

int drm_lib_login(const char *username, const char *password)
{
    /* Same-process credential replacement has no safe Vendor lifetime contract.
     * Fresh credentials must enter through full init in a new B1 Engine. */
    (void)username; (void)password;
    return -1;
}

/* A C++ exception unwinding out of drm_lib_init (or a SVError recovered by longjmp) skips the unlock calls, which would
 * hang the next init and drm_lib_shutdown on g_init_mutex. The exception barrier calls this when init did not return
 * normally, on the thread that ran it; the trylock keeps it a no-op if the mutex is somehow free. Adapted from PR #1. */
void drm_lib_abort_init(void)
{
    g_init_result = -1;
    pthread_mutex_trylock(&g_init_mutex); /* held by the aborted init, or free: either way it ends unlocked */
    pthread_mutex_unlock(&g_init_mutex);
    drm_owner_state("FAILED");
    drm_owner_cancel();
}

/* ── drm_lib_shutdown ───────────────────────────────────────────────────────*/

void drm_lib_shutdown(void)
{
    /* No Vendor resource release: revoke callbacks and drain entered users.
     * Do not take g_init_mutex here: a challenge may cancel its own init. */
    drm_owner_cancel();
}

/* ── drm_lib_get_m3u8 ───────────────────────────────────────────────────────*/

char *drm_lib_get_m3u8(unsigned long adam_id)
{
    const char *url;
    if (offlineFlag)
        url = get_m3u8_method_download(reqCtx, adam_id, NULL);
    else
        url = get_m3u8_method_play(leaseMgr, adam_id, NULL);

    if (!url)
        return NULL;
    char *ret = strdup(url);
    free((void *)url);
    return ret;
}

/* ── drm_lib_get_account ────────────────────────────────────────────────────*/

char *drm_lib_get_account(void)
{
    if (!g_storefront_id || !g_dev_token || !g_music_token)
        return NULL;

    /* Build JSON: {"storefront_id":"…","dev_token":"…","music_token":"…"} */
    size_t len = strlen(g_storefront_id) + strlen(g_dev_token) +
                 strlen(g_music_token) + 80;
    char *buf = malloc(len);
    if (!buf)
        return NULL;
    snprintf(buf, len,
        "{\"storefront_id\":\"%s\",\"dev_token\":\"%s\",\"music_token\":\"%s\"}",
        g_storefront_id, g_dev_token, g_music_token);
    return buf;
}

/* ── drm_lib_get_mv ─────────────────────────────────────────────────────────*/

int drm_lib_get_mv(unsigned long adam_id, char **out_url, char **out_dk,
                   int *out_has_itun)
{
    char *dk = NULL;
    const char *url = get_progressive_method_play(leaseMgr, adam_id, &dk);
    if (!url) {
        url = get_m3u8_method_play(leaseMgr, adam_id, &dk);
    }
    if (!url)
        return -1;

    *out_url = strdup(url);
    free((void *)url);
    *out_dk = dk ? strdup(dk) : NULL;
    if (dk) free(dk);

    pthread_mutex_lock(&g_itun_mutex);
    *out_has_itun = (g_itun_decryptor.obj != NULL && g_itun_adam_id == adam_id) ? 1 : 0;
    pthread_mutex_unlock(&g_itun_mutex);

    return 0;
}

/* ── drm_lib_open_kd_ctx ────────────────────────────────────────────────────*/

void *drm_lib_open_kd_ctx(const char *adam_id, const char *uri)
{
    return getKdContext(adam_id, uri);
}

/* ── drm_lib_decrypt ────────────────────────────────────────────────────────*/

int drm_lib_decrypt(void *kd_ctx, uint8_t *sample, uint32_t size)
{
    if (!kd_ctx || !sample)
        return -1;
    /* getKdContext() returns the key-context slot, not the context itself:
     * handle() in main.c (the proven TCP path) calls the decryptor with
     * *kdContext. Passing kd_ctx directly decrypts with the wrong context and
     * yields garbage samples. */
    void *ctx = *(void **)kd_ctx;
    if (!ctx)
        return -1;
    NfcRKVnxuKZy04KWbdFu71Ou(ctx, (uint32_t)5, sample, sample, (size_t)size);
    return 0;
}

/* ── drm_lib_decrypt_itun ───────────────────────────────────────────────────*/

int drm_lib_decrypt_itun(unsigned long adam_id, uint8_t *sample,
                          uint32_t in_size, uint32_t *out_size)
{
    pthread_mutex_lock(&g_itun_mutex);
    void *dec_obj   = g_itun_decryptor.obj;
    unsigned long dec_adam = g_itun_adam_id;
    pthread_mutex_unlock(&g_itun_mutex);

    if (!dec_obj) {
        fprintf(stderr, "[drm_lib] itun: no decryptor (call drm_lib_get_mv first)\n");
        return -1;
    }
    if (dec_adam != adam_id) {
        fprintf(stderr, "[drm_lib] itun: adamId mismatch: have %lu, got %lu\n",
                dec_adam, adam_id);
        return -1;
    }

    uint32_t out_len = 0;
    _ZN17SVPastisDecryptor13decryptSampleEPKhRKjPj(dec_obj, sample, &in_size, &out_len);
    *out_size = out_len;
    return 0;
}

/* ── drm_lib_is_recovery_active ─────────────────────────────────────────────*/

int drm_lib_is_recovery_active(void)
{
    extern int is_recovery_active(void);
    return is_recovery_active();
}

/* ── drm_lib_debug_enabled ──────────────────────────────────────────────────*/

int drm_lib_debug_enabled(void)
{
    static int cached = -1;
    if (cached < 0) cached = getenv("ADAM_DRM_DEBUG") != NULL;
    return cached;
}
