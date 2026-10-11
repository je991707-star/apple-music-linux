/*
 * libCoreFP.so - Apple FairPlay Streaming Library
 * Decompiled from Apple Music Android APK (libCoreFP.so)
 * 
 * All export functions are thin wrappers around two dispatcher functions:
 * - sub_58F870: Main dispatcher (dku592fbFAj, X46O5IeS, YlCJ3lg, WIn9UJ86JKdV4dM)
 * - sub_3A6B30: Alt dispatcher (lxpgvVMLd0S7uRl, fdjkDSAFjklaf2s, JNI_OnLoad)
 * 
 * The obfuscation is just XOR/multiply wrappers - actual logic is in indirect jumps.
 * 
 * SPC Generation Flow:
 * 1. dku592fbFAj(ctx) - Initialize context, loads embedded RSA key
 * 2. X46O5IeS(0, &device_info, spc_out) - Generate SPC certificate
 * 3. Extract KID from spc_out[4:24] (20 bytes)
 * 4. X46O5IeS(3, &ckc_data, key_ctx_out) - Process CKC for decryption
 * 5. YlCJ3lg() or WIn9UJ86JKdV4dM() - Decrypt samples (IV is always zero for CBCS)
 */

#ifndef LIBCOREFP_H
#define LIBCOREFP_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ── Export function prototypes (deobfuscated from IDA) ────────────────────*/

/* dku592fbFAj - Initialize FairPlay context, loads embedded RSA key */
int64_t dku592fbFAj(int64_t ctx);

/* lxpgvVMLd0S7uRl - Get certificate (1-arg variant) */
int64_t lxpgvVMLd0S7uRl(int64_t ctx);

/* fdjkDSAFjklaf2s - Get certificate by type */
int64_t fdjkDSAFjklaf2s(int type, int64_t out);

/* X46O5IeS - SPC/CKC operation
 * op=0: Generate SPC from device info
 * op=3: Process CKC (Content Key Container) for decryption key context
 */
int64_t X46O5IeS(int op, int64_t in, int64_t out);

/* YlCJ3lg - Decrypt (6 args, CBCS with zero IV) */
int64_t YlCJ3lg(int selector, int64_t ctx, int in, int out, int64_t len, int64_t flag, int64_t reserved);

/* WIn9UJ86JKdV4dM - Decrypt with explicit IV (7 args + xmm0) */
int64_t WIn9UJ86JKdV4dM(int selector, int64_t ctx, int64_t in, int64_t out, int len, int64_t iv_hi, __int128 iv_lo);

/* JNI_OnLoad - JNI initialization */
int64_t JNI_OnLoad(int64_t vm, int64_t reserved);

/* ── Dispatcher functions (internal, not exported) ─────────────────────────*/

/* Main dispatcher - used by dku592fbFAj, X46O5IeS, YlCJ3lg, WIn9UJ86JKdV4dM */
void sub_58F870(int64_t ctx);

/* Alt dispatcher - used by lxpgvVMLd0S7uRl, fdjkDSAFjklaf2s, JNI_OnLoad */
void sub_3A6B30(int64_t ctx);

/* ── RSA Key ───────────────────────────────────────────────────────────────*/

/* 
 * Embedded RSA private key for SPC generation
 * Address: 0x189eec
 * Size: 1067 bytes
 * Format: Custom Apple format (starts with 'rSa' marker)
 * Header bytes: 72 53 61 1b ... (rSa...)
 */
extern const uint8_t g_rsa_key[1067];

/* ── Context structure (opaque, ~0x200 bytes) ──────────────────────────────*/

/* Context structure layout (from dispatcher analysis):
 * Offset 0x00: Pointer to sub-context
 * Offset 0x10: State value 1
 * Offset 0x18: State value 2
 * Offset 0x12: Counter
 * Offset 0x24: Another field
 * ... up to 0x190
 */
typedef struct {
    uint8_t data[0x200];
} fp_context_t;

/* ── SPC/CKC input structure ───────────────────────────────────────────────*/

typedef struct {
    const uint8_t *data;  // Device info (for SPC) or CKC bytes (for key context)
    uint32_t len;
    uint32_t pad;
} fp_input_t;

/* ── Usage examples ────────────────────────────────────────────────────────*/

/*
 * Complete SPC generation and decryption flow:
 * 
 * // 1. Load library and resolve exports
 * void *lib = dlopen("libCoreFP.so", RTLD_NOW);
 * fp_dku_fn dku = dlsym(lib, "dku592fbFAj");
 * fp_x46_fn x46 = dlsym(lib, "X46O5IeS");
 * fp_ylc_fn ylc = dlsym(lib, "YlCJ3lg");
 * 
 * // 2. Initialize FairPlay context (loads RSA key internally)
 * fp_context_t *ctx = calloc(1, sizeof(fp_context_t));
 * dku((int64_t)ctx);
 * 
 * // 3. Generate SPC from device info
 * uint8_t device_info[] = "...";  // Your device info/identifier
 * fp_input_t in = { .data = device_info, .len = sizeof(device_info), .pad = 0 };
 * uint8_t spc_out[1024];
 * x46(0, &in, spc_out);  // op=0 for SPC generation
 * 
 * // 4. Extract KID from SPC (20 bytes at offset 4)
 * uint8_t kid[20];
 * memcpy(kid, spc_out + 4, 20);
 * 
 * // 5. Process CKC to get decryption key context
 * uint8_t ckc_data[] = "...";  // Base64-decoded CKC from license server
 * in.data = ckc_data;
 * in.len = sizeof(ckc_data);
 * uint8_t key_ctx_buf[512];
 * x46(3, &in, key_ctx_buf);  // op=3 for CKC processing
 * 
 * // 6. Extract key context pointer
 * void *key_ctx = *(void **)key_ctx_buf;
 * 
 * // 7. Decrypt samples (CBCS uses zero IV)
 * uint8_t sample[16] = "...";  // Encrypted sample
 * ylc(0, (int64_t)key_ctx, (int64_t)sample, (int64_t)sample, 16, 0, 0);
 * 
 * // Cleanup
 * free(ctx);
 * dlclose(lib);
 */

#ifdef __cplusplus
}
#endif

#endif /* LIBCOREFP_H */
