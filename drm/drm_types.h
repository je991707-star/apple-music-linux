/*
 * drm_types.h — types for the flat C API in drm_linux.c.
 *
 * engine/core/drm/adamplay_backend.go declares matching types in its cgo
 * preamble; keep the two in sync.
 */

#pragma once

#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L
#endif

#include <stdint.h>
#include <stddef.h>

/*
 * Called when credentials or a 2FA code is needed. challenge_type is
 * "credentials" (write "username:password") or "2fa" (write the 6-digit code).
 */
typedef void (*drm_auth_callback_t)(
    const char *challenge_type,
    char *output_buffer,
    int buffer_size,
    void *user_data);

/* Called on state transitions; state_name is one of the DRM_STATE_* values. */
typedef void (*drm_state_callback_t)(
    const char *state_name,
    void *user_data);

struct drm_config {
    const char *base_directory;  /* contains mpl_db/ and the session files */
    const char *lib64_directory; /* rootfs/system/lib64 */
    const char *username;        /* NULL to reuse the saved session */
    const char *password;        /* NULL to reuse the saved session */
    const char *device_info;     /* 9-field slash-separated string, or NULL */
    int offline_only;            /* 1 = download method, 0 = play method */
    drm_auth_callback_t auth_callback;
    void *auth_user_data;
    drm_state_callback_t state_callback;
    void *state_user_data;
};

#define DRM_STATE_STARTING              "STARTING"
#define DRM_STATE_LOGIN                 "LOGIN"
#define DRM_STATE_WAITING_2FA           "WAITING_2FA"
#define DRM_STATE_INITIALIZING_FAIRPLAY "INITIALIZING_FAIRPLAY"
#define DRM_STATE_RUNNING               "RUNNING"
#define DRM_STATE_FAILED                "FAILED"
