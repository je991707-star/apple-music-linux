/*
 * fp_ckc_acquisition.h - Content Key Acquisition Module
 * 
 * Version: 1.0
 * Date: 2026-10-10
 * 
 * Clean-room implementation of streaming-based CKC acquisition from Apple's
 * license server. This module sits on top of the core FairPlay infrastructure
 * and handles HTTP-based key delivery with CKC parsing.
 */

#ifndef FP_CKC_ACQUISITION_H
#define FP_CKC_ACQUISITION_H

#include "fp_ckc_parser.h"
#include <stdint.h>
#include <stddef.h>
#include <pthread.h>

#ifdef __cplusplus
extern "C" {
#endif

/* =============================================================================
 * Acquisition State Machine (REQ-046)
 * ============================================================================= */

/**
 * States through which an acquisition passes.
 */
typedef enum {
    FP_CKC_ACQ_STATE_IDLE        = 0,   /* Not actively acquiring */
    FP_CKC_ACQ_STATE_REQUESTING  = 1,   /* HTTP request in progress */
    FP_CKC_ACQ_STATE_PARSED      = 2,   /* CKC parsed successfully */
    FP_CKC_ACQ_STATE_EXPIRED     = 3,   /* Key has expired (renewal path) */
    FP_CKC_ACQ_STATE_ERROR       = 4,   /* Error state */
} fp_ckc_acquisition_state_t;

/* =============================================================================
 * Acquisition Context Structure (REQ-001)
 * ============================================================================= */

/**
 * Content Key Acquisition Context
 * 
 * Manages the lifecycle of a streaming key acquisition from Apple's license server.
 * Handles HTTP requests, CKC parsing, and state transitions.
 */
typedef struct fp_ckc_acquire_ctx {
    char *asset_id;              /* Asset identifier (for caching) */
    char *media_uri;             /* Media URI being acquired */
    
    uint8_t aes_key[16];         /* Decryption key from CKC */
    uint8_t base_iv[16];         /* Base IV for sample derivation */
    
    uint64_t expires_at;         /* Unix timestamp of expiration (REQ-036) */
    uint32_t lease_duration;     /* Lease duration in seconds (REQ-037) */
    
    char *session_id;            /* Session ID from CKC (optional) */
    char *renewal_url;           /* Renewal URL for extension (optional) */
    
    fp_ckc_t ckck;               /* Parsed CKC structure */
    
    pthread_mutex_t lock;        /* Thread safety for concurrent access */
    int refcount;                /* Reference count for caching */
    
    fp_ckc_acquisition_state_t state;  /* Current acquisition state */
} fp_ckc_acquire_ctx_t;

typedef fp_ckc_acquire_ctx_t *fp_ckc_acquire_handle_t;

/* =============================================================================
 * Public API: Acquisition Lifecycle
 * ============================================================================= */

/**
 * Create a new CKC acquisition context.
 * 
 * @param asset_id Asset identifier string (for caching)
 * @param media_uri Media URI being acquired
 * @param out_context Output: newly created acquisition context
 * @return FP_OK on success, error code otherwise
 */
fp_error_t fp_ckc_acquire_create(
    const char *asset_id,
    const char *media_uri,
    fp_ckc_acquire_handle_t *out_context
);

/**
 * Destroy an acquisition context.
 * 
 * Frees all allocated memory and resets state. Safe to call multiple times.
 */
void fp_ckc_acquire_destroy(fp_ckc_acquire_handle_t context);

/**
 * Get the current acquisition state.
 */
fp_ckc_acquisition_state_t fp_ckc_acquire_get_state(const fp_ckc_acquire_handle_t context);

/* =============================================================================
 * Public API: CKC Parsing & Extraction (REQ-001 through REQ-006)
 * ============================================================================= */

/**
 * Parse a CKC binary blob from the license response.
 * 
 * Implements the acquisition workflow:
 * 1. CKC binary already extracted from JSON response
 * 2. Parse binary structure (including IV field - REQ-001)
 * 
 * @param context Acquisition context
 * @param ckc_data Binary CKC data (already base64-decoded)
 * @param ckc_size Size of CKC binary data
 * @return FP_OK on success, error code otherwise
 */
fp_error_t fp_ckc_acquire_parse(
    fp_ckc_acquire_handle_t context,
    const uint8_t *ckc_data,
    size_t ckc_size
);

/**
 * Extract the decryption key from the acquisition.
 * 
 * Handles both plaintext (16 bytes) and RSA-wrapped (256 bytes) keys (REQ-002).
 */
fp_error_t fp_ckc_acquire_extract_key(
    const fp_ckc_acquire_handle_t context,
    uint8_t *out_aes_key,
    size_t key_size
);

/**
 * Get the base IV for sample-level decryption.
 * 
 * The base IV is used with per-sample number derivation (REQ-003).
 */
fp_error_t fp_ckc_acquire_get_base_iv(
    const fp_ckc_acquire_handle_t context,
    uint8_t *out_iv,
    size_t iv_size
);

/**
 * Check if the acquired key has expired.
 */
bool fp_ckc_acquire_is_expired(const fp_ckc_acquire_handle_t context);

/* =============================================================================
 * Public API: HTTP Helpers (from fp_http_client)
 * ============================================================================= */

/**
 * Perform an authenticated POST request to acquire a license.
 * 
 * @param context Acquisition context
 * @param storage_path Path to credentials directory (drm/files/)
 * @param media_uri Media URI for the asset
 * @param adam_id Asset ID (optional, extracted from media_uri if NULL)
 * @param auth_token Bearer token from credentials (optional, auto-read if NULL)
 * @param storefront_id Storefront identifier (optional, auto-read if NULL)
 * @param device_guid Device GUID (optional)
 * @return FP_OK on success, error code otherwise
 */
fp_error_t fp_ckc_acquire_from_server(
    fp_ckc_acquire_handle_t context,
    const char *storage_path,
    const char *media_uri,
    const char *adam_id,
    const char *auth_token,
    const char *storefront_id,
    const char *device_guid
);

/**
 * Perform a renewal request for an existing key.
 */
fp_error_t fp_ckc_acquire_renew(
    fp_ckc_acquire_handle_t context,
    const char *session_id,
    const char *auth_token
);

/* =============================================================================
 * Public API: Decryption with Sample IV Derivation (REQ-003)
 * ============================================================================= */

/**
 * Decrypt a sample using the acquired key.
 * 
 * Implements per-sample IV derivation from base IV and sample number (REQ-003).
 * This is the core of CBCS (CBC Subtle) decryption.
 * 
 * @param context Acquisition context with parsed CKC
 * @param data Encrypted sample data (in-place modification)
 * @param size Size of encrypted data
 * @param sample_number Sample index for IV derivation
 * @return FP_OK on success, error code otherwise
 */
fp_error_t fp_ckc_acquire_decrypt_sample_at(
    const fp_ckc_acquire_handle_t context,
    uint8_t *data,
    size_t size,
    uint64_t sample_number
);

/**
 * Decrypt a full segment using the acquired key.
 * 
 * For streaming where multiple samples are grouped together.
 */
fp_error_t fp_ckc_acquire_decrypt_segment(
    const fp_ckc_acquire_handle_t context,
    const uint8_t *segment_data,
    size_t segment_size,
    uint8_t *out_decrypted,
    size_t *decrypted_size,
    size_t max_decrypted_size
);

/* =============================================================================
 * Public API: Key Expiration & Renewal (REQ-004 through REQ-005)
 * ============================================================================= */

/**
 * Check if the acquisition should trigger a renewal.
 * 
 * Returns true when current time is within the renewal_offset window.
 */
bool fp_ckc_acquire_should_renew(const fp_ckc_acquire_handle_t context);

/**
 * Get time until key expiration in seconds.
 */
uint32_t fp_ckc_acquire_time_until_expiry(const fp_ckc_acquire_handle_t context);

/* =============================================================================
 * Public API: State Management
 * ============================================================================= */

/**
 * Set the acquisition state manually (for debugging).
 */
void fp_ckc_acquire_set_state(fp_ckc_acquire_handle_t context, fp_ckc_acquisition_state_t state);

/**
 * Get human-readable state name.
 */
const char *fp_ckc_acquire_get_state_name(const fp_ckc_acquire_handle_t context);

/* =============================================================================
 * Public API: Utility Functions
 * ============================================================================= */

/**
 * Initialize an acquisition context (zero all fields).
 */
void fp_ckc_acquire_init(fp_ckc_acquire_handle_t context);

/**
 * Get the raw CKC data for debugging/logging.
 */
fp_error_t fp_ckc_acquire_get_raw_ckc(
    const fp_ckc_acquire_handle_t context,
    uint8_t *out_data,
    size_t *out_size
);

/* =============================================================================
 * Public API: Callbacks (for async operations)
 * ============================================================================= */

/**
 * License callback function type.
 */
typedef void (*fp_ckc_acquisition_callback)(
    fp_error_t error,
    const fp_ckc_acquire_handle_t context,
    void *user_data
);

/**
 * Set a callback for acquisition completion.
 */
void fp_ckc_acquire_set_callback(
    fp_ckc_acquire_handle_t context,
    fp_ckc_acquisition_callback callback,
    void *user_data
);

/* =============================================================================
 * Public API: Statistics
 * ============================================================================= */

/**
 * Acquisition statistics structure.
 */
typedef struct {
    uint64_t requests_made;
    uint64_t requests_success;
    uint64_t requests_failed;
    uint64_t samples_decrypted;
    uint64_t bytes_processed;
} fp_ckc_acquisition_stats_t;

/**
 * Get acquisition statistics.
 */
void fp_ckc_acquire_get_stats(
    const fp_ckc_acquire_handle_t context,
    fp_ckc_acquisition_stats_t *stats
);

/* =============================================================================
 * Public API: Logging Integration
 * ============================================================================= */

/**
 * Log level for the acquisition module.
 */
typedef enum {
    FP_CKC_ACQ_LOG_ERROR = 0,
    FP_CKC_ACQ_LOG_WARN  = 1,
    FP_CKC_ACQ_LOG_INFO  = 2,
    FP_CKC_ACQ_LOG_DEBUG = 3
} fp_ckc_acquisition_log_level_t;

/**
 * Set the log level for acquisition events.
 */
void fp_ckc_acquire_set_log_level(fp_ckc_acquisition_log_level_t level);

/* =============================================================================
 * Public API: Convenience Functions
 * ============================================================================= */

/**
 * Quick acquire from a binary CKC blob.
 */
fp_error_t fp_ckc_acquire_quick(
    const char *asset_id,
    const uint8_t *ckc_data,
    size_t ckc_size,
    fp_ckc_acquire_handle_t *out_context
);

/**
 * Quick decrypt with inline CKC parsing.
 */
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
);

/* =============================================================================
 * Public API: Version Info
 * ============================================================================= */

/**
 * Get the acquisition module version.
 */
const char *fp_ckc_acquire_version(void);

#ifdef __cplusplus
}
#endif

#endif /* FP_CKC_ACQUISITION_H */
