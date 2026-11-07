/* Minimal stand-in for Dobby: the R1 key-server hook is not used in the
 * in-process build, so all hook installs report failure. */
#ifndef AML_DOBBY_STUB_H
#define AML_DOBBY_STUB_H
#include <stdint.h>

typedef struct {
    struct {
        union {
            struct { uint64_t rax, rbx, rcx, rdx, rsi, rdi, rbp, rsp,
                              r8, r9, r10, r11, r12, r13, r14, r15; } regs;
        };
    } general;
} DobbyRegisterContext;

static inline int DobbyHook(void *a, void *b, void **c) { (void)a; (void)b; (void)c; return -1; }
static inline int DobbyInstrument(void *a, void (*cb)(void *, DobbyRegisterContext *)) { (void)a; (void)cb; return -1; }
#endif
