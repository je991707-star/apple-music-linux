/*
 * libcorefp_wrapper.c - Wrapper for libCoreFP.so FairPlay functions
 * 
 * This wrapper loads libCoreFP.so dynamically and provides a clean API
 * for KID extraction from SPC and IV handling (always zero for CBCS).
 */

#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <dlfcn.h>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wpedantic"

#include "libcorefp_wrapper.h"
#include "libcorefp.h"

/* ── Global state ───────────────────────────────────────────────────────────*/

static struct {
    void *lib_handle;
    int initialized;
    void *fp_ctx;  // FairPlay context (from dku592fbFAj)
    
    /* Function pointers - using int64_t instead of __int64 for GCC compatibility */
    int64_t (*dku)(int64_t ctx);
    int64_t (*x46)(int op, int64_t in, int64_t out);
    int64_t (*ylc)(int selector, int64_t ctx, int in, int out, int64_t len, int64_t flag, int64_t reserved);
    int64_t (*win)(int selector, int64_t ctx, int64_t in, int64_t out, int len, int64_t iv_hi, int64_t iv_lo);
} g_libcorefp = {0};

/* ── Initialize libCoreFP.so ────────────────────────────────────────────────*/

int libcorefp_init(const char *lib_path) {
    if (g_libcorefp.initialized) {
        return 0;  // Already initialized
    }
    
    /* Load the library */
    g_libcorefp.lib_handle = dlopen(lib_path, RTLD_NOW);
    if (!g_libcorefp.lib_handle) {
        fprintf(stderr, "[libcorefp] dlopen failed: %s\n", dlerror());
        return -1;
    }
    
    /* Resolve export functions */
    void *tmp;
    tmp = dlsym(g_libcorefp.lib_handle, "dku592fbFAj");
    g_libcorefp.dku = (int64_t (*)(int64_t))tmp;
    tmp = dlsym(g_libcorefp.lib_handle, "X46O5IeS");
    g_libcorefp.x46 = (int64_t (*)(int, int64_t, int64_t))tmp;
    tmp = dlsym(g_libcorefp.lib_handle, "YlCJ3lg");
    g_libcorefp.ylc = (int64_t (*)(int, int64_t, int, int, int64_t, int64_t, int64_t))tmp;
    tmp = dlsym(g_libcorefp.lib_handle, "WIn9UJ86JKdV4dM");
    g_libcorefp.win = (int64_t (*)(int, int64_t, int64_t, int64_t, int, int64_t, int64_t))tmp;
    
    if (!g_libcorefp.dku || !g_libcorefp.x46 || !g_libcorefp.ylc) {
        fprintf(stderr, "[libcorefp] failed to resolve exports\n");
        dlclose(g_libcorefp.lib_handle);
        return -1;
    }
    
    /* Initialize FairPlay context (loads RSA key internally) */
    g_libcorefp.fp_ctx = calloc(1, sizeof(fp_context_t));
    if (!g_libcorefp.fp_ctx) {
        fprintf(stderr, "[libcorefp] failed to allocate context\n");
        dlclose(g_libcorefp.lib_handle);
        return -1;
    }
    
    if (g_libcorefp.dku((int64_t)g_libcorefp.fp_ctx) != 0) {
        fprintf(stderr, "[libcorefp] dku592fbFAj failed\n");
        free(g_libcorefp.fp_ctx);
        dlclose(g_libcorefp.lib_handle);
        return -1;
    }
    
    g_libcorefp.initialized = 1;
    fprintf(stderr, "[libcorefp] initialized successfully\n");
    return 0;
}

/* ── Shutdown ───────────────────────────────────────────────────────────────*/

void libcorefp_shutdown(void) {
    if (!g_libcorefp.initialized) {
        return;
    }
    
    if (g_libcorefp.fp_ctx) {
        free(g_libcorefp.fp_ctx);
        g_libcorefp.fp_ctx = NULL;
    }
    
    if (g_libcorefp.lib_handle) {
        dlclose(g_libcorefp.lib_handle);
        g_libcorefp.lib_handle = NULL;
    }
    
    g_libcorefp.initialized = 0;
    fprintf(stderr, "[libcorefp] shutdown\n");
}

/* ── Generate SPC and extract KID ───────────────────────────────────────────*/

/*
 * Generate SPC from device info and extract the KID.
 * 
 * device_info: Device identifier/info bytes
 * device_info_len: Length of device_info
 * kid_out: Output buffer for KID (must be at least 20 bytes)
 * kid_len_out: Output for actual KID length
 * 
 * Returns: 0 on success, -1 on error
 */
int libcorefp_generate_spc_and_extract_kid(
    const uint8_t *device_info,
    uint32_t device_info_len,
    uint8_t kid_out[20],
    uint32_t *kid_len_out)
{
    if (!g_libcorefp.initialized || !g_libcorefp.x46) {
        fprintf(stderr, "[libcorefp] not initialized\n");
        return -1;
    }
    
    /* Prepare input structure */
    fp_input_t in = {
        .data = device_info,
        .len = device_info_len,
        .pad = 0
    };
    
    /* Output buffer for SPC */
    uint8_t spc_out[1024];
    memset(spc_out, 0, sizeof(spc_out));
    
    /* Call X46O5IeS with op=0 for SPC generation */
    int ret = g_libcorefp.x46(0, (int64_t)&in, (int64_t)spc_out);
    if (ret != 0) {
        fprintf(stderr, "[libcorefp] X46O5IeS(op=0) failed: %d\n", ret);
        return -1;
    }
    
    /* Extract KID from SPC (20 bytes at offset 4) */
    if (sizeof(spc_out) < 24) {
        fprintf(stderr, "[libcorefp] SPC too small\n");
        return -1;
    }
    
    memcpy(kid_out, spc_out + 4, 20);
    *kid_len_out = 20;
    
    fprintf(stderr, "[libcorefp] SPC generated, KID extracted (%u bytes)\n", *kid_len_out);
    return 0;
}

/* ── Process CKC and extract key context ────────────────────────────────────*/

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
    void **key_ctx_out)
{
    if (!g_libcorefp.initialized || !g_libcorefp.x46) {
        fprintf(stderr, "[libcorefp] not initialized\n");
        return -1;
    }
    
    /* Prepare input structure */
    fp_input_t in = {
        .data = ckc_data,
        .len = ckc_len,
        .pad = 0
    };
    
    /* Output buffer for key context */
    uint8_t key_ctx_buf[512];
    memset(key_ctx_buf, 0, sizeof(key_ctx_buf));
    
    /* Call X46O5IeS with op=3 for CKC processing */
    int ret = g_libcorefp.x46(3, (int64_t)&in, (int64_t)key_ctx_buf);
    if (ret != 0) {
        fprintf(stderr, "[libcorefp] X46O5IeS(op=3) failed: %d\n", ret);
        return -1;
    }
    
    /* Extract key context pointer from output */
    void *key_ctx = *(void **)key_ctx_buf;
    if (!key_ctx) {
        fprintf(stderr, "[libcorefp] key context is NULL\n");
        return -1;
    }
    
    *key_ctx_out = key_ctx;
    fprintf(stderr, "[libcorefp] CKC processed, key context = %p\n", key_ctx);
    return 0;
}

/* ── Decrypt sample using key context ───────────────────────────────────────*/

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
int libcorefp_decrypt_sample(void *key_ctx, uint8_t *data, uint32_t len) {
    if (!g_libcorefp.initialized || !g_libcorefp.ylc) {
        fprintf(stderr, "[libcorefp] not initialized\n");
        return -1;
    }
    
    if (!key_ctx || !data || len == 0) {
        fprintf(stderr, "[libcorefp] invalid arguments\n");
        return -1;
    }
    
    /* CBCS decryption with zero IV */
    int ret = g_libcorefp.ylc(0, (int64_t)(intptr_t)key_ctx, (int)(intptr_t)data, (int)(intptr_t)data, (int64_t)len, 0, 0);
    if (ret != 0) {
        fprintf(stderr, "[libcorefp] YlCJ3lg failed: %d\n", ret);
        return -1;
    }
    
    return 0;
}

/* ── Get zero IV for CBCS ───────────────────────────────────────────────────*/

/*
 * CBCS always uses zero IV. This is a convenience function.
 * 
 * iv_out: Output buffer for IV (must be at least 16 bytes)
 * 
 * Returns: 0 always
 */
int libcorefp_get_cbcv_iv(uint8_t iv_out[16]) {
    memset(iv_out, 0, 16);
    return 0;
}

/* ── Check if initialized ───────────────────────────────────────────────────*/

int libcorefp_is_initialized(void) {
    return g_libcorefp.initialized;
}
#pragma GCC diagnostic pop
