/*
 * libcorefp_wrapper.h - Wrapper API for libCoreFP.so FairPlay functions
 */

#ifndef LIBCOREFP_WRAPPER_H
#define LIBCOREFP_WRAPPER_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ── Initialization ────────────────────────────────────────────────────────*/

/*
 * Initialize libCoreFP.so wrapper.
 * Loads the library and initializes the FairPlay context.
 * 
 * lib_path: Path to libCoreFP.so (e.g., "libCoreFP.so" or full path)
 * 
 * Returns: 0 on success, -1 on error
 */
int libcorefp_init(const char *lib_path);

/*
 * Shutdown libCoreFP.so wrapper.
 * Frees resources and unloads the library.
 */
void libcorefp_shutdown(void);

/*
 * Check if libCoreFP.so wrapper is initialized.
 * 
 * Returns: 1 if initialized, 0 otherwise
 */
int libcorefp_is_initialized(void);

/* ── SPC Generation and KID Extraction ─────────────────────────────────────*/

/*
 * Generate SPC from device info and extract the KID.
 * 
 * device_info: Device identifier/info bytes
 * device_info_len: Length of device_info
 * kid_out: Output buffer for KID (must be at least 20 bytes)
 * kid_len_out: Output for actual KID length (will be 20)
 * 
 * Returns: 0 on success, -1 on error
 */
int libcorefp_generate_spc_and_extract_kid(
    const uint8_t *device_info,
    uint32_t device_info_len,
    uint8_t kid_out[20],
    uint32_t *kid_len_out);

/* ── CKC Processing ────────────────────────────────────────────────────────*/

/*
 * Process CKC (Content Key Container) to get decryption key context.
 * 
 * ckc_data: Base64-decoded CKC bytes from license server
 * ckc_len: Length of CKC data
 * key_ctx_out: Output for key context pointer
 * 
 * Returns: 0 on success, -1 on error
 */
int libcorefp_process_ckc(
    const uint8_t *ckc_data,
    uint32_t ckc_len,
    void **key_ctx_out);

/* ── Decryption ────────────────────────────────────────────────────────────*/

/*
 * Decrypt a sample using the key context from CKC processing.
 * CBCS uses zero IV, so this is the simpler version.
 * 
 * key_ctx: Key context from libcorefp_process_ckc()
 * data: Input/output buffer for sample data (decrypted in-place)
 * len: Sample length (must be multiple of 16)
 * 
 * Returns: 0 on success, -1 on error
 */
int libcorefp_decrypt_sample(void *key_ctx, uint8_t *data, uint32_t len);

/* ── IV Handling ───────────────────────────────────────────────────────────*/

/*
 * Get zero IV for CBCS decryption.
 * Apple Music CBCS always uses zero IV.
 * 
 * iv_out: Output buffer for IV (must be at least 16 bytes)
 * 
 * Returns: 0 always
 */
int libcorefp_get_cbcv_iv(uint8_t iv_out[16]);

#ifdef __cplusplus
}
#endif

#endif /* LIBCOREFP_WRAPPER_H */
