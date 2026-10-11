/*
 * corefp_helper.c — Wine subprocess helper for CoreFP.dll
 *
 * Compiled with winegcc; runs under Wine on Linux.  The parent Go process
 * communicates over stdin/stdout using a simple length-prefixed binary
 * protocol so no sockets or extra setup are needed.
 *
 * Frame format (both directions):
 *   [1 byte  op / status]
 *   [4 bytes payload length, little-endian]
 *   [N bytes payload]
 *
 * Request opcodes:
 *   0x01  INIT        payload: path to CoreFP.dll (NUL-terminated string)
 *   0x02  GEN_SPC     payload: raw device_info bytes
 *   0x03  PROCESS_CKC payload: raw CKC bytes
 *   0x04  DECRYPT     payload: 8-byte key_ctx (LE uint64) + ciphertext
 *   0xFF  SHUTDOWN    payload: empty
 *
 * Response status byte:
 *   0x00  OK
 *   0x01  ERROR  (payload is a NUL-terminated error string)
 *
 * GEN_SPC response payload: [20 bytes KID][remaining bytes = SPC]
 * PROCESS_CKC response payload: 8-byte key_ctx LE uint64
 * DECRYPT response payload: decrypted bytes (same length as input)
 */

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* ── CoreFP function pointer types ────────────────────────────────────── */

typedef int64_t (__cdecl *fp_dku_t)(int64_t ctx);
typedef int64_t (__cdecl *fp_x46_t)(int op, int64_t in_ptr, int64_t out_ptr);
/* YlCJ3lg: selector, key_ctx, in, out, len, flag, reserved */
typedef int64_t (__cdecl *fp_ylc_t)(int sel, int64_t kctx,
                                     int64_t in, int64_t out,
                                     int64_t len, int64_t flag, int64_t rsv);

/* Input descriptor passed to X46O5IeS */
typedef struct { const uint8_t *data; uint32_t len; uint32_t pad; } fp_in_t;

/* ── Global state ──────────────────────────────────────────────────────── */

static HMODULE   g_dll   = NULL;
static fp_dku_t  g_dku   = NULL;
static fp_x46_t  g_x46   = NULL;
static fp_ylc_t  g_ylc   = NULL;
static void     *g_ctx   = NULL;   /* dku592fbFAj context (0x200 bytes) */

/* ── I/O helpers ──────────────────────────────────────────────────────── */

static int read_exact(HANDLE h, void *buf, DWORD n) {
    DWORD got = 0, total = 0;
    while (total < n) {
        if (!ReadFile(h, (char *)buf + total, n - total, &got, NULL) || got == 0)
            return -1;
        total += got;
    }
    return 0;
}

static int write_exact(HANDLE h, const void *buf, DWORD n) {
    DWORD wrote = 0, total = 0;
    while (total < n) {
        if (!WriteFile(h, (const char *)buf + total, n - total, &wrote, NULL) || wrote == 0)
            return -1;
        total += wrote;
    }
    return 0;
}

static int read_frame(HANDLE h, uint8_t *op_out, uint8_t **payload, uint32_t *len_out) {
    uint8_t  op;
    uint32_t plen;
    if (read_exact(h, &op, 1) < 0)  return -1;
    if (read_exact(h, &plen, 4) < 0) return -1;
    /* little-endian already on x86 */
    *op_out  = op;
    *len_out = plen;
    if (plen == 0) { *payload = NULL; return 0; }
    *payload = (uint8_t *)malloc(plen);
    if (!*payload) return -1;
    if (read_exact(h, *payload, plen) < 0) { free(*payload); return -1; }
    return 0;
}

static int write_frame(HANDLE h, uint8_t status, const void *data, uint32_t dlen) {
    if (write_exact(h, &status, 1) < 0)   return -1;
    if (write_exact(h, &dlen,   4) < 0)   return -1;
    if (dlen > 0 && write_exact(h, data, dlen) < 0) return -1;
    return 0;
}

static int write_error(HANDLE h, const char *msg) {
    return write_frame(h, 0x01, msg, (uint32_t)strlen(msg) + 1);
}

/* ── Handlers ─────────────────────────────────────────────────────────── */

static int handle_init(HANDLE h, const uint8_t *payload, uint32_t plen) {
    if (g_dll) { write_error(h, "already initialised"); return 0; }
    if (plen == 0) { write_error(h, "missing dll path"); return 0; }

    char path[MAX_PATH];
    uint32_t copy = plen < MAX_PATH ? plen : MAX_PATH - 1;
    memcpy(path, payload, copy);
    path[copy] = '\0';

    g_dll = LoadLibraryA(path);
    if (!g_dll) {
        char msg[256];
        snprintf(msg, sizeof(msg), "LoadLibraryA failed: %u", (unsigned)GetLastError());
        write_error(h, msg);
        return 0;
    }

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wcast-function-type"
    g_dku = (fp_dku_t)GetProcAddress(g_dll, "dku592fbFAj");
    g_x46 = (fp_x46_t)GetProcAddress(g_dll, "X46O5IeS");
    g_ylc = (fp_ylc_t)GetProcAddress(g_dll, "YlCJ3lg");
#pragma GCC diagnostic pop

    if (!g_dku || !g_x46 || !g_ylc) {
        FreeLibrary(g_dll); g_dll = NULL;
        write_error(h, "failed to resolve exports");
        return 0;
    }

    g_ctx = calloc(1, 0x200);
    if (!g_ctx) {
        FreeLibrary(g_dll); g_dll = NULL;
        write_error(h, "calloc failed");
        return 0;
    }

    int64_t rc = g_dku((int64_t)(uintptr_t)g_ctx);
    /* dku returns void or 0 on success; treat non-zero as soft failure */
    if (rc != 0) {
        /* Some builds return void (undefined return value); continue anyway */
    }

    write_frame(h, 0x00, NULL, 0);
    return 0;
}

static int handle_gen_spc(HANDLE h, const uint8_t *device_info, uint32_t dlen) {
    if (!g_dll) { write_error(h, "not initialised"); return 0; }

    fp_in_t in;
    in.data = device_info;
    in.len  = dlen;
    in.pad  = 0;

    /* SPC output: Apple says ≤ 4 KB; allocate generously */
    uint8_t spc_out[4096];
    memset(spc_out, 0, sizeof(spc_out));

    int64_t rc = g_x46(0, (int64_t)(uintptr_t)&in, (int64_t)(uintptr_t)spc_out);
    if (rc != 0) {
        char msg[64];
        snprintf(msg, sizeof(msg), "X46O5IeS(op=0) returned %lld", (long long)rc);
        write_error(h, msg);
        return 0;
    }

    /* First 4 bytes = total SPC length (big-endian per Apple spec) */
    uint32_t spc_len_be;
    memcpy(&spc_len_be, spc_out, 4);
    uint32_t spc_len = ((spc_len_be >> 24) & 0xFF) | ((spc_len_be >> 8) & 0xFF00) |
                       ((spc_len_be << 8) & 0xFF0000) | ((spc_len_be << 24) & 0xFF000000);
    if (spc_len == 0 || spc_len > sizeof(spc_out)) spc_len = sizeof(spc_out);

    /* Response: [20 bytes KID][spc_len bytes SPC] */
    uint8_t response[20 + 4096];
    /* KID is at spc_out[4..23] per libcorefp_spc_logic.md */
    memcpy(response,      spc_out + 4, 20);   /* KID */
    memcpy(response + 20, spc_out,     spc_len); /* full SPC including length prefix */

    write_frame(h, 0x00, response, 20 + spc_len);
    return 0;
}

static int handle_process_ckc(HANDLE h, const uint8_t *ckc, uint32_t ckc_len) {
    if (!g_dll) { write_error(h, "not initialised"); return 0; }

    fp_in_t in;
    in.data = ckc;
    in.len  = ckc_len;
    in.pad  = 0;

    uint8_t key_ctx_buf[512];
    memset(key_ctx_buf, 0, sizeof(key_ctx_buf));

    int64_t rc = g_x46(3, (int64_t)(uintptr_t)&in, (int64_t)(uintptr_t)key_ctx_buf);
    if (rc != 0) {
        char msg[64];
        snprintf(msg, sizeof(msg), "X46O5IeS(op=3) returned %lld", (long long)rc);
        write_error(h, msg);
        return 0;
    }

    /* key_ctx pointer is the first 8 bytes of the output buffer */
    uint64_t key_ctx;
    memcpy(&key_ctx, key_ctx_buf, 8);
    if (key_ctx == 0) {
        write_error(h, "key context is NULL after CKC processing");
        return 0;
    }

    write_frame(h, 0x00, &key_ctx, 8);
    return 0;
}

static int handle_decrypt(HANDLE h, const uint8_t *payload, uint32_t plen) {
    if (!g_dll) { write_error(h, "not initialised"); return 0; }
    if (plen < 8) { write_error(h, "payload too short"); return 0; }

    uint64_t key_ctx;
    memcpy(&key_ctx, payload, 8);
    const uint8_t *data = payload + 8;
    uint32_t dlen = plen - 8;

    if (dlen == 0) { write_frame(h, 0x00, NULL, 0); return 0; }

    /* Decrypt in-place copy */
    uint8_t *out = (uint8_t *)malloc(dlen);
    if (!out) { write_error(h, "malloc failed"); return 0; }
    memcpy(out, data, dlen);

    /* YlCJ3lg: selector=0, key_ctx, in, out, len, flag=0, reserved=0 */
    int64_t rc = g_ylc(0,
                       (int64_t)(uintptr_t)(void *)(uintptr_t)key_ctx,
                       (int64_t)(uintptr_t)out,
                       (int64_t)(uintptr_t)out,
                       (int64_t)dlen, 0, 0);
    if (rc != 0) {
        free(out);
        char msg[64];
        snprintf(msg, sizeof(msg), "YlCJ3lg returned %lld", (long long)rc);
        write_error(h, msg);
        return 0;
    }

    write_frame(h, 0x00, out, dlen);
    free(out);
    return 0;
}

/* ── Main loop ────────────────────────────────────────────────────────── */

int main(void) {
    HANDLE hin  = GetStdHandle(STD_INPUT_HANDLE);
    HANDLE hout = GetStdHandle(STD_OUTPUT_HANDLE);

    for (;;) {
        uint8_t  op;
        uint8_t *payload = NULL;
        uint32_t plen    = 0;

        if (read_frame(hin, &op, &payload, &plen) < 0) break;

        switch (op) {
        case 0x01: handle_init(hout, payload, plen);        break;
        case 0x02: handle_gen_spc(hout, payload, plen);     break;
        case 0x03: handle_process_ckc(hout, payload, plen); break;
        case 0x04: handle_decrypt(hout, payload, plen);     break;
        case 0xFF: free(payload); return 0;                  /* shutdown */
        default:
            write_error(hout, "unknown opcode");
            break;
        }

        free(payload);
    }
    return 0;
}
