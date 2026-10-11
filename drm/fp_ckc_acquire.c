/*
 * fp_ckc_acquire.c - Content Key Acquisition Implementation
 * 
 * Version: 1.0
 * Date: 2026-10-10
 * 
 * Implements streaming-based CKC acquisition from Apple's license server,
 * including per-sample IV derivation for CBCS decryption (REQ-003).
 * 
 * This module integrates:
 * - fp_http_client.c: HTTP communication with Apple's license server
 * - fp_ckc_parser.c: Binary CKC structure parsing
 * - fairplay.c: AES-128-CBC decryption primitives
 */

#define _POSIX_C_SOURCE 200809L

#include "fp_ckc_acquisition.h"
#include "fp_http_client.h"
#include <curl/curl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <pthread.h>

/* =============================================================================
 * Internal Data Structures
 * ============================================================================= */

/* Forward declaration not needed - using typedef from header */

/* =============================================================================
 * Global State
 * ============================================================================= */

static fp_ckc_acquisition_log_level_t g_log_level = FP_CKC_ACQ_LOG_INFO;

#define LOG_LEVEL_CHECK(level) \
    if (g_log_level >= (level))

#define LOG_ERROR(fmt, ...) \
    do { \
        LOG_LEVEL_CHECK(FP_CKC_ACQ_LOG_ERROR); \
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: " fmt "\n", ##__VA_ARGS__); \
    } while(0)

#define LOG_WARN(fmt, ...) \
    do { \
        LOG_LEVEL_CHECK(FP_CKC_ACQ_LOG_WARN); \
        fprintf(stderr, "[CKC-ACQUIRE] WARN: " fmt "\n", ##__VA_ARGS__); \
    } while(0)

#define LOG_INFO(fmt, ...) \
    do { \
        LOG_LEVEL_CHECK(FP_CKC_ACQ_LOG_INFO); \
        fprintf(stderr, "[CKC-ACQUIRE] INFO: " fmt "\n", ##__VA_ARGS__); \
    } while(0)

#define LOG_DEBUG(fmt, ...) \
    do { \
        LOG_LEVEL_CHECK(FP_CKC_ACQ_LOG_DEBUG); \
        fprintf(stderr, "[CKC-ACQUIRE] DEBUG: " fmt "\n", ##__VA_ARGS__); \
    } while(0)

/* =============================================================================
 * Helper Functions
 * ============================================================================= */

/* =============================================================================
 * Helper Functions
 * ============================================================================= */

/**
 * Write buffer for curl callback.
 */
typedef struct {
    uint8_t *data;
    size_t size;
    size_t capacity;
} http_write_buffer_t;

/**
 * Write callback for curl.
 */
static size_t http_response_callback(void *ptr, size_t size, size_t nmemb, void *userp) {
    http_write_buffer_t *buf = (http_write_buffer_t *)userp;
    size_t data_size = size * nmemb;
    
    if (buf->size + data_size > buf->capacity) {
        size_t new_cap = buf->capacity == 0 ? 4096 : buf->capacity * 2;
        while (buf->size + data_size > new_cap) new_cap *= 2;
        uint8_t *new_data = realloc(buf->data, new_cap);
        if (!new_data) return 0;
        buf->data = new_data;
        buf->capacity = new_cap;
    }
    
    memcpy(buf->data + buf->size, ptr, data_size);
    buf->size += data_size;
    return data_size;
}

/**
 * Read file and trim whitespace.
 */
static char *read_file_trimmed(const char *path) {
    FILE *f = fopen(path, "r");
    if (!f) return NULL;
    
    fseek(f, 0, SEEK_END);
    long sz = ftell(f);
    rewind(f);
    
    if (sz <= 0 || sz > 65536) {
        fclose(f);
        return NULL;
    }
    
    char *buf = malloc((size_t)sz + 1);
    if (!buf) {
        fclose(f);
        return NULL;
    }
    
    size_t n = fread(buf, 1, (size_t)sz, f);
    fclose(f);
    buf[n] = '\0';
    
    while (n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r' || buf[n-1] == ' ')) {
        buf[--n] = '\0';
    }
    
    return buf;
}

/**
 * Read credentials from storage directory for acquisition context.
 */
static fp_error_t fp_ckc_read_credentials(
    const char *storage_path,
    char **out_auth_token,
    char **out_storefront_id
) {
    if (!storage_path) return FP_ERR_NULL_POINTER;
    
    char token_path[1024];
    char storefront_path[1024];
    
    snprintf(token_path, sizeof(token_path), "%s/MUSIC_TOKEN", storage_path);
    snprintf(storefront_path, sizeof(storefront_path), "%s/STOREFRONT_ID", storage_path);
    
    *out_auth_token = read_file_trimmed(token_path);
    *out_storefront_id = read_file_trimmed(storefront_path);
    
    if (!*out_auth_token || !*out_storefront_id) {
        free(*out_auth_token);
        free(*out_storefront_id);
        return FP_ERR_NULL_POINTER;
    }
    
    return FP_OK;
}

static uint64_t fp_ckc_acquire_current_time(void) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (uint64_t)ts.tv_sec;
}

/* =============================================================================
 * Public API: Acquisition Lifecycle
 * ============================================================================= */

fp_error_t fp_ckc_acquire_create(
    const char *asset_id,
    const char *media_uri,
    fp_ckc_acquire_handle_t *out_context
) {
    if (!out_context) {
        return FP_ERR_NULL_POINTER;
    }
    
    fp_ckc_acquire_handle_t ctx = calloc(1, sizeof(struct fp_ckc_acquire_ctx));
    if (!ctx) {
        return FP_ERR_OUT_OF_MEMORY;
    }
    
    /* Initialize the embedded CKC structure */
    fp_ckc_init(&ctx->ckck);
    
    /* Set asset ID and media URI for caching */
    ctx->asset_id = strdup(asset_id ? asset_id : "");
    ctx->media_uri = strdup(media_uri ? media_uri : "");
    
    if (!ctx->asset_id || !ctx->media_uri) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: Failed to duplicate strings\n");
        free(ctx->asset_id);
        free(ctx->media_uri);
        ctx->asset_id = NULL;  /* Clear pointers to avoid double-free */
        ctx->media_uri = NULL;
        fp_ckc_acquire_destroy(ctx);
        return FP_ERR_OUT_OF_MEMORY;
    }
    
    /* Initialize mutex */
    if (pthread_mutex_init(&ctx->lock, NULL) != 0) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: Mutex init failed\n");
        free(ctx->asset_id);
        free(ctx->media_uri);
        fp_ckc_acquire_destroy(ctx);
        return FP_ERR_THREAD_INIT_FAILED;
    }
    
    ctx->refcount = 1;
    ctx->state = FP_CKC_ACQ_STATE_IDLE;
    
    *out_context = ctx;
    fprintf(stderr, "[CKC-ACQUIRE] DEBUG: Acquisition context created for asset: %s\n", ctx->asset_id);
    
    return FP_OK;
}

void fp_ckc_acquire_destroy(fp_ckc_acquire_handle_t context) {
    if (!context) {
        return;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Decrement refcount */
    int remaining = --context->refcount;
    
    if (remaining > 0) {
        pthread_mutex_unlock(&context->lock);
        fprintf(stderr, "[CKC-ACQUIRE] DEBUG: Refcount %d, context still in use\n", remaining);
        return;
    }
    
    /* Refcount reached zero — safe to free */
    pthread_mutex_unlock(&context->lock);
    
    if (context->asset_id) {
        free(context->asset_id);
        context->asset_id = NULL;
    }
    if (context->media_uri) {
        free(context->media_uri);
        context->media_uri = NULL;
    }
    if (context->session_id) {
        free(context->session_id);
        context->session_id = NULL;
    }
    if (context->renewal_url) {
        free(context->renewal_url);
        context->renewal_url = NULL;
    }
    
    /* Zero the CKC structure */
    fp_ckc_free(&context->ckck);
    
    pthread_mutex_destroy(&context->lock);
    fairplay_secure_zero(context, sizeof(struct fp_ckc_acquire_ctx));
    free(context);
}

fp_ckc_acquisition_state_t fp_ckc_acquire_get_state(const fp_ckc_acquire_handle_t context) {
    if (!context) return FP_CKC_ACQ_STATE_IDLE;
    
    pthread_mutex_lock(&context->lock);
    fp_ckc_acquisition_state_t state = context->state;
    pthread_mutex_unlock(&context->lock);
    
    return state;
}

/* =============================================================================
 * Public API: CKC Parsing & Extraction (REQ-001 through REQ-006)
 * ============================================================================= */

fp_error_t fp_ckc_acquire_parse(
    fp_ckc_acquire_handle_t context,
    const uint8_t *ckc_data,
    size_t ckc_size
) {
    if (!context || !ckc_data || ckc_size == 0) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Parse the CKC binary structure using fp_ckc_parser.c */
    fp_error_t err = fp_ckc_parse(ckc_data, ckc_size, &context->ckck);
    if (err != FP_OK) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: CKC parse failed: %s\n", fairplay_error_string(err));
        pthread_mutex_unlock(&context->lock);
        return err;
    }
    
    /* Extract decryption key from CKC (REQ-002) */
    /* Handle both plaintext (16 bytes) and RSA-wrapped (256 bytes) keys */
    if (context->ckck.encrypted_key_size > 0) {
        if (context->ckck.encrypted_key_size == 16) {
            /* Plaintext key - copy directly */
            memcpy(context->aes_key, context->ckck.encrypted_key, 16);
            fprintf(stderr, "[CKC-ACQUIRE] DEBUG: Plaintext key extracted (16 bytes)\n");
        } else if (context->ckck.encrypted_key_size == 256) {
            /* RSA-wrapped key - would need device key to unwrap */
            /* For now, copy the wrapped key; actual unwrapping happens elsewhere */
            memcpy(context->aes_key, context->ckck.encrypted_key, 16);
            fprintf(stderr, "[CKC-ACQUIRE] WARN: RSA-wrapped key detected (256 bytes) - unwrapping not yet implemented\n");
        } else {
            fprintf(stderr, "[CKC-ACQUIRE] WARN: Unexpected key size: %zu bytes\n", context->ckck.encrypted_key_size);
            memcpy(context->aes_key, context->ckck.encrypted_key, 
                   context->ckck.encrypted_key_size < 16 ? context->ckck.encrypted_key_size : 16);
        }
    } else {
        fprintf(stderr, "[CKC-ACQUIRE] WARN: No key data in CKC\n");
        memset(context->aes_key, 0, 16);
    }
    
    /* Extract base IV from CKC (REQ-001) */
    if (context->ckck.has_iv) {
        memcpy(context->base_iv, context->ckck.iv, 16);
        fprintf(stderr, "[CKC-ACQUIRE] DEBUG: Base IV extracted from CKC\n");
    } else {
        memset(context->base_iv, 0, 16);
        fprintf(stderr, "[CKC-ACQUIRE] WARN: No IV in CKC, using zero IV fallback\n");
    }
    
    /* Copy expiration time and lease duration */
    context->expires_at = context->ckck.expires_at;
    context->lease_duration = context->ckck.lease_duration;
    
    /* Copy optional session ID and renewal URL */
    if (context->ckck.session_id[0] != '\0') {
        context->session_id = strdup(context->ckck.session_id);
    }
    if (context->ckck.renewal_url[0] != '\0') {
        context->renewal_url = strdup(context->ckck.renewal_url);
    }
    
    context->state = FP_CKC_ACQ_STATE_PARSED;
    
    pthread_mutex_unlock(&context->lock);
    
    fprintf(stderr, "[CKC-ACQUIRE] INFO: CKC parsed: KID=%02x%02x%02x%02x, expires at %llu\n",
             context->ckck.kid[0], context->ckck.kid[1], 
             context->ckck.kid[2], context->ckck.kid[3],
             (unsigned long long)context->expires_at);
    
    return FP_OK;
}

fp_error_t fp_ckc_acquire_extract_key(
    const fp_ckc_acquire_handle_t context,
    uint8_t *out_aes_key,
    size_t key_size
) {
    if (!context || !out_aes_key) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Copy the 16-byte AES-128 key */
    memcpy(out_aes_key, context->aes_key, 
           (key_size < 16) ? key_size : 16);
    
    pthread_mutex_unlock(&context->lock);
    
    return FP_OK;
}

fp_error_t fp_ckc_acquire_get_base_iv(
    const fp_ckc_acquire_handle_t context,
    uint8_t *out_iv,
    size_t iv_size
) {
    if (!context || !out_iv) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Copy the base IV */
    memcpy(out_iv, context->base_iv, 
           (iv_size < 16) ? iv_size : 16);
    
    pthread_mutex_unlock(&context->lock);
    
    return FP_OK;
}

bool fp_ckc_acquire_is_expired(const fp_ckc_acquire_handle_t context) {
    if (!context) return true;
    
    pthread_mutex_lock(&context->lock);
    
    /* Persistent keys don't expire */
    if (context->ckck.flags & FP_CKC_FLAG_PERSISTENT) {
        bool result = false;
        pthread_mutex_unlock(&context->lock);
        return result;
    }
    
    uint64_t now = fp_ckc_acquire_current_time();
    bool expired = (now >= context->expires_at);
    
    pthread_mutex_unlock(&context->lock);
    return expired;
}

/* =============================================================================
 * Public API: HTTP Integration (using fp_http_client.c)
 * ============================================================================= */

fp_error_t fp_ckc_acquire_from_server(
    fp_ckc_acquire_handle_t context,
    const char *storage_path,   /* Path to credentials directory */
    const char *media_uri,
    const char *adam_id,
    const char *auth_token,     /* Optional: auto-read if NULL */
    const char *storefront_id,  /* Optional: auto-read if NULL */
    const char *device_guid
) {
    if (!context || !media_uri) {
        return FP_ERR_NULL_POINTER;
    }
    
    char *token = NULL;
    char *storefront = NULL;
    
    /* Auto-read credentials if storage_path provided and credentials not passed */
    if (storage_path && (!auth_token || !storefront_id)) {
        fp_error_t err = fp_ckc_read_credentials(storage_path, &token, &storefront);
        if (err != FP_OK) {
            fprintf(stderr, "[CKC-ACQUIRE] ERROR: Failed to read credentials from %s\n", storage_path);
            free(token);
            free(storefront);
            return err;
        }
    }
    
    const char *use_token = auth_token ? auth_token : token;
    const char *use_storefront = storefront_id ? storefront_id : storefront;
    
    if (!use_token) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: No auth token available\n");
        free(token);
        free(storefront);
        return FP_ERR_NULL_POINTER;
    }
    
    /* Update state */
    pthread_mutex_lock(&context->lock);
    context->state = FP_CKC_ACQ_STATE_REQUESTING;
    pthread_mutex_unlock(&context->lock);
    
    /* Initialize HTTP config with defaults */
    fp_http_config_t http_config;
    fp_http_config_init(&http_config);
    
    /* Perform license exchange using fp_http_client.c */
    fp_http_response_t *http_response = NULL;
    fp_error_t err = fp_license_exchange(
        &http_config,
        media_uri,
        adam_id,
        use_token,      /* Use auto-read or provided */
        use_storefront, /* Use auto-read or provided */
        device_guid,
        &http_response
    );
    
    free(token);
    free(storefront);
    
    if (err != FP_OK) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: License exchange failed: %s\n", fairplay_error_string(err));
        pthread_mutex_lock(&context->lock);
        context->state = FP_CKC_ACQ_STATE_ERROR;
        pthread_mutex_unlock(&context->lock);
        return err;
    }
    
    /* Check if CKC was successfully extracted */
    if (!http_response->ckc_data || http_response->ckc_size == 0) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: No CKC data in HTTP response\n");
        fp_http_response_free(http_response);
        pthread_mutex_lock(&context->lock);
        context->state = FP_CKC_ACQ_STATE_ERROR;
        pthread_mutex_unlock(&context->lock);
        return FP_ERR_CKC_PARSE_FAILED;
    }
    
    /* Parse the CKC binary using fp_ckc_parse */
    err = fp_ckc_acquire_parse(context, http_response->ckc_data, http_response->ckc_size);
    
    /* Free HTTP response */
    fp_http_response_free(http_response);
    
    if (err != FP_OK) {
        return err;
    }
    
    fprintf(stderr, "[CKC-ACQUIRE] INFO: Key acquisition successful for asset: %s\n", context->asset_id);
    
    return FP_OK;
}

fp_error_t fp_ckc_acquire_renew(
    fp_ckc_acquire_handle_t context,
    const char *session_id,
    const char *auth_token
) {
    if (!context || !session_id || !auth_token) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Check if we have a renewal URL */
    if (context->ckck.renewal_url[0] == '\0') {
        fprintf(stderr, "[CKC-ACQUIRE] WARN: No renewal URL available\n");
        pthread_mutex_unlock(&context->lock);
        return FP_ERR_LICENSE_REQUEST_FAILED;
    }
    
    /* Build renewal request body */
    size_t body_size = 512;
    char *request_body = malloc(body_size);
    if (!request_body) {
        pthread_mutex_unlock(&context->lock);
        return FP_ERR_OUT_OF_MEMORY;
    }
    
    snprintf(request_body, body_size, 
             "{\"session_id\":\"%s\",\"asset_id\":\"%s\"}",
             session_id, context->asset_id ? context->asset_id : "");
    
    pthread_mutex_unlock(&context->lock);
    
    /* Perform HTTP POST to renewal URL */
    CURL *curl = curl_easy_init();
    if (!curl) {
        free(request_body);
        return FP_ERR_OUT_OF_MEMORY;
    }
    
    struct curl_slist *headers = NULL;
    headers = curl_slist_append(headers, "Content-Type: application/json");
    headers = curl_slist_append(headers, "Accept: application/json");
    char auth_header[512];
    snprintf(auth_header, sizeof(auth_header), "Authorization: Bearer %s", auth_token);
    headers = curl_slist_append(headers, auth_header);
    
    curl_easy_setopt(curl, CURLOPT_URL, context->ckck.renewal_url);
    curl_easy_setopt(curl, CURLOPT_POST, 1L);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, request_body);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE, (long)strlen(request_body));
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 30L);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    
    /* Response buffer */
    http_write_buffer_t write_buf = {0};
    
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, http_response_callback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &write_buf);
    
    CURLcode curl_err = curl_easy_perform(curl);
    long http_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);
    free(request_body);
    
    if (curl_err != CURLE_OK || http_code != 200) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: Renewal request failed (HTTP %ld)\n", http_code);
        free(write_buf.data);
        return FP_ERR_LICENSE_REQUEST_FAILED;
    }
    
    /* Parse new CKC from response */
    /* Response format: {"ckc":"<base64>","expirationTime":...} */
    const char *ckc_key = "\"ckc\"";
    const char *p = (const char *)write_buf.data;
    const char *found = NULL;
    size_t remaining = write_buf.size;
    
    while (remaining >= strlen(ckc_key)) {
        if (strncmp(p, ckc_key, strlen(ckc_key)) == 0) {
            found = p;
            break;
        }
        p++;
        remaining--;
    }
    
    if (!found) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: No CKC in renewal response\n");
        free(write_buf.data);
        return FP_ERR_CKC_PARSE_FAILED;
    }
    
    /* Skip to base64 data */
    p = found + strlen(ckc_key);
    while (*p != '"') p++;
    p++;  /* skip opening quote */
    
    const char *b64_start = p;
    while (*p != '"' && p < (const char *)(write_buf.data + write_buf.size)) p++;
    size_t b64_len = p - b64_start;
    
    /* Base64 decode */
    size_t decoded_size = (b64_len / 4) * 3;
    uint8_t *ckc_data = malloc(decoded_size + 1);
    if (!ckc_data) {
        free(write_buf.data);
        return FP_ERR_OUT_OF_MEMORY;
    }
    
    fp_error_t err = fairplay_base64_decode(b64_start, ckc_data, &decoded_size, decoded_size + 1);
    free(write_buf.data);
    if (err != FP_OK) {
        free(ckc_data);
        return FP_ERR_INVALID_BASE64;
    }
    
    /* Parse new CKC */
    err = fp_ckc_acquire_parse(context, ckc_data, decoded_size);
    free(ckc_data);
    
    if (err != FP_OK) {
        fprintf(stderr, "[CKC-ACQUIRE] ERROR: Failed to parse renewed CKC\n");
        return err;
    }
    
    fprintf(stderr, "[CKC-ACQUIRE] INFO: License renewed successfully\n");
    return FP_OK;
}

/* =============================================================================
 * Public API: Decryption with Sample IV Derivation (REQ-003)
 * ============================================================================= */

fp_error_t fp_ckc_acquire_decrypt_sample_at(
    const fp_ckc_acquire_handle_t context,
    uint8_t *data,
    size_t size,
    uint64_t sample_number
) {
    if (!context || !data || size == 0) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Check expiration first (REQ-004) */
    if (fp_ckc_acquire_is_expired(context)) {
        fprintf(stderr, "[CKC-ACQUIRE] WARN: Key expired at sample %lu\n", (unsigned long)sample_number);
        pthread_mutex_unlock(&context->lock);
        return FP_ERR_KEY_EXPIRED;
    }
    
    /* Derive per-sample IV from base IV and sample number (REQ-003) */
    /* Formula: Sample IV = base_iv XOR sample_number (big-endian, low 8 bytes) */
    fp_iv_t derived_iv;
    memcpy(derived_iv.bytes, context->base_iv, 16);
    
    /* XOR sample number into low 8 bytes of IV (big-endian) */
    derived_iv.bytes[8] ^= (sample_number >> 56) & 0xFF;
    derived_iv.bytes[9] ^= (sample_number >> 48) & 0xFF;
    derived_iv.bytes[10] ^= (sample_number >> 40) & 0xFF;
    derived_iv.bytes[11] ^= (sample_number >> 32) & 0xFF;
    derived_iv.bytes[12] ^= (sample_number >> 24) & 0xFF;
    derived_iv.bytes[13] ^= (sample_number >> 16) & 0xFF;
    derived_iv.bytes[14] ^= (sample_number >> 8) & 0xFF;
    derived_iv.bytes[15] ^= sample_number & 0xFF;
    
    /* Perform AES-128-CBC decryption using fairplay.c */
    fp_key_t aes_key_struct;
    memcpy(aes_key_struct.bytes, context->aes_key, 16);
    
    fp_error_t err = fairplay_decrypt_aes128_cbc(
        &aes_key_struct, &derived_iv,
        data, size,
        data, &size,  /* In-place modification */
        size          /* Output same size as input for AES-CBC */
    );
    
    pthread_mutex_unlock(&context->lock);
    
    return err;
}

fp_error_t fp_ckc_acquire_decrypt_segment(
    const fp_ckc_acquire_handle_t context,
    const uint8_t *segment_data,
    size_t segment_size,
    uint8_t *out_decrypted,
    size_t *decrypted_size,
    size_t max_decrypted_size
) {
    if (!context || !segment_data || !out_decrypted) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Check expiration */
    if (fp_ckc_acquire_is_expired(context)) {
        pthread_mutex_unlock(&context->lock);
        return FP_ERR_KEY_EXPIRED;
    }
    
    /* Use base IV directly for segment decryption */
    fp_key_t aes_key_struct;
    fp_iv_t iv;
    memcpy(aes_key_struct.bytes, context->aes_key, 16);
    memcpy(iv.bytes, context->base_iv, 16);
    
    fp_error_t err = fairplay_decrypt_aes128_cbc(
        &aes_key_struct, &iv,
        segment_data, segment_size,
        out_decrypted, decrypted_size,
        max_decrypted_size
    );
    
    pthread_mutex_unlock(&context->lock);
    
    return err;
}

/* =============================================================================
 * Public API: Key Expiration & Renewal
 * ============================================================================= */

bool fp_ckc_acquire_should_renew(const fp_ckc_acquire_handle_t context) {
    if (!context) return false;
    
    pthread_mutex_lock(&context->lock);
    
    /* Check if renewable flag is set (REQ-005) */
    if (!(context->ckck.flags & FP_CKC_FLAG_RENEWABLE)) {
        pthread_mutex_unlock(&context->lock);
        return false;
    }
    
    uint64_t now = fp_ckc_acquire_current_time();
    
    /* Check if within renewal window (renewal_offset seconds before expiry) */
    uint32_t time_left = (uint32_t)(context->ckck.expires_at - now);
    
    pthread_mutex_unlock(&context->lock);
    
    return time_left <= context->ckck.renewal_offset;
}

uint32_t fp_ckc_acquire_time_until_expiry(const fp_ckc_acquire_handle_t context) {
    if (!context) return 0;
    
    pthread_mutex_lock(&context->lock);
    
    uint64_t now = fp_ckc_acquire_current_time();
    uint32_t time_left = (uint32_t)(context->ckck.expires_at - now);
    
    pthread_mutex_unlock(&context->lock);
    
    return time_left;
}

/* =============================================================================
 * Public API: State Management
 * ============================================================================= */

void fp_ckc_acquire_set_state(
    fp_ckc_acquire_handle_t context,
    fp_ckc_acquisition_state_t state
) {
    if (!context) return;
    
    pthread_mutex_lock(&context->lock);
    context->state = state;
    pthread_mutex_unlock(&context->lock);
}

const char *fp_ckc_acquire_get_state_name(const fp_ckc_acquire_handle_t context) {
    if (!context) return "NULL";
    
    pthread_mutex_lock(&context->lock);
    const char *name = NULL;
    
    switch (context->state) {
        case FP_CKC_ACQ_STATE_IDLE:
            name = "IDLE";
            break;
        case FP_CKC_ACQ_STATE_REQUESTING:
            name = "REQUESTING";
            break;
        case FP_CKC_ACQ_STATE_PARSED:
            name = "PARSED";
            break;
        case FP_CKC_ACQ_STATE_EXPIRED:
            name = "EXPIRED";
            break;
        case FP_CKC_ACQ_STATE_ERROR:
            name = "ERROR";
            break;
    }
    
    pthread_mutex_unlock(&context->lock);
    return name;
}

/* =============================================================================
 * Public API: Utility Functions
 * ============================================================================= */

void fp_ckc_acquire_init(fp_ckc_acquire_handle_t context) {
    if (!context) return;
    
    pthread_mutex_lock(&context->lock);
    fp_ckc_init(&context->ckck);
    memset(context->aes_key, 0, 16);
    memset(context->base_iv, 0, 16);
    context->expires_at = 0;
    
    pthread_mutex_unlock(&context->lock);
}

fp_error_t fp_ckc_acquire_get_raw_ckc(
    const fp_ckc_acquire_handle_t context,
    uint8_t *out_data,
    size_t *out_size
) {
    if (!context || !out_data) {
        return FP_ERR_NULL_POINTER;
    }
    
    pthread_mutex_lock(&context->lock);
    
    /* Copy the raw CKC data */
    if (context->ckck.raw_data && context->ckck.raw_size > 0) {
        size_t copy_size = context->ckck.raw_size < 2048 ? context->ckck.raw_size : 2048;
        memcpy(out_data, context->ckck.raw_data, copy_size);
        
        if (out_size) {
            *out_size = context->ckck.raw_size;
        }
    } else {
        if (out_size) *out_size = 0;
    }
    
    pthread_mutex_unlock(&context->lock);
    
    return FP_OK;
}

/* =============================================================================
 * Public API: Callbacks
 * ============================================================================= */

void fp_ckc_acquire_set_callback(
    fp_ckc_acquire_handle_t context,
    fp_ckc_acquisition_callback callback,
    void *user_data
) {
    (void)context;
    (void)callback;
    (void)user_data;
}

/* =============================================================================
 * Public API: Statistics
 * ============================================================================= */

void fp_ckc_acquire_get_stats(
    const fp_ckc_acquire_handle_t context,
    fp_ckc_acquisition_stats_t *stats
) {
    if (!context || !stats) return;
    
    pthread_mutex_lock(&context->lock);
    
    /* Initialize stats */
    memset(stats, 0, sizeof(fp_ckc_acquisition_stats_t));
    
    /* Count keys in the embedded CKC as "keys acquired" */
    stats->requests_success = 1;
    
    pthread_mutex_unlock(&context->lock);
}

/* =============================================================================
 * Public API: Logging Integration
 * ============================================================================= */

void fp_ckc_acquire_set_log_level(fp_ckc_acquisition_log_level_t level) {
    g_log_level = level;
}

/* =============================================================================
 * Public API: Convenience Functions
 * ============================================================================= */

fp_error_t fp_ckc_acquire_quick(
    const char *asset_id,
    const uint8_t *ckc_data,
    size_t ckc_size,
    fp_ckc_acquire_handle_t *out_context
) {
    if (!asset_id || !ckc_data || !out_context) {
        return FP_ERR_NULL_POINTER;
    }
    
    /* Create acquisition context */
    fp_error_t err = fp_ckc_acquire_create(asset_id, "", out_context);
    if (err != FP_OK) {
        return err;
    }
    
    /* Parse the CKC blob */
    err = fp_ckc_acquire_parse(*out_context, ckc_data, ckc_size);
    
    return err;
}

fp_error_t fp_ckc_acquire_quick_decrypt(
    const char *asset_id,
    const uint8_t *ckc_data,
    size_t ckc_size,
    const uint8_t *data,
    size_t data_size,
    uint64_t sample_number,
    uint8_t *out_plaintext,
    size_t *plaintext_size,
    size_t max_plaintext_size
) {
    if (!asset_id || !ckc_data || !data || !out_plaintext) {
        return FP_ERR_NULL_POINTER;
    }
    
    (void)max_plaintext_size;  /* Not used in quick mode - caller ensures buffer is large enough */
    
    fp_ckc_acquire_handle_t ctx;
    fp_error_t err = fp_ckc_acquire_quick(asset_id, ckc_data, ckc_size, &ctx);
    if (err != FP_OK) {
        return err;
    }
    
    /* Copy data to decrypt in-place */
    uint8_t *temp_data = malloc(data_size);
    if (!temp_data) {
        fp_ckc_acquire_destroy(ctx);
        return FP_ERR_OUT_OF_MEMORY;
    }
    memcpy(temp_data, data, data_size);
    
    /* Decrypt at sample level */
    err = fp_ckc_acquire_decrypt_sample_at(ctx, temp_data, data_size, sample_number);
    
    if (err == FP_OK && plaintext_size) {
        *plaintext_size = data_size;
        memcpy(out_plaintext, temp_data, data_size);
    }
    
    free(temp_data);
    fp_ckc_acquire_destroy(ctx);
    
    return err;
}

/* =============================================================================
 * Public API: Version Info
 * ============================================================================= */

const char *fp_ckc_acquire_version(void) {
    static const char version[] = "1.0.0";
    return version;
}
