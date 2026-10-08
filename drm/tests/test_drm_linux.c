/* Standalone test for drm_linux.c. Build+run:
 *   gcc -std=gnu11 -fsanitize=address,undefined -g -I.. -o /tmp/t_drm tests/test_drm_linux.c drm_linux.c -lcrypto -lpthread && /tmp/t_drm */
#include "../drm_types.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <openssl/evp.h>

char *drm_get_account(void);
int drm_init(const struct drm_config *cfg);
void drm_shutdown(void);
drm_key_context_handle_t drm_open_key_context(const char *a, const char *u);
void drm_close_key_context(drm_key_context_handle_t kc);
int drm_set_key_context_key(drm_key_context_handle_t kc, const uint8_t *k, const uint8_t *iv);
int drm_decrypt_sample_at(drm_key_context_handle_t kc, uint8_t *d, uint32_t n, uint64_t sn);

static void write_file(const char *dir, const char *name, const char *body, size_t n)
{
    char p[4096]; snprintf(p, sizeof p, "%s/%s", dir, name);
    FILE *f = fopen(p, "w"); assert(f); fwrite(body, 1, n, f); fclose(f);
}

static void test_roundtrip_and_close(void)
{
    uint8_t key[16], iv[16] = {0}, plain[48], buf[48];
    for (int i = 0; i < 16; i++) key[i] = (uint8_t)(i * 7 + 1);
    for (int i = 0; i < 48; i++) plain[i] = (uint8_t)i;
    int outl = 0;
    EVP_CIPHER_CTX *c = EVP_CIPHER_CTX_new();
    EVP_EncryptInit_ex(c, EVP_aes_128_cbc(), NULL, key, iv);
    EVP_CIPHER_CTX_set_padding(c, 0);
    EVP_EncryptUpdate(c, buf, &outl, plain, 48);
    EVP_CIPHER_CTX_free(c);
    assert(outl == 48);

    drm_key_context_handle_t kc = drm_open_key_context("1", "skd://x");
    assert(kc);
    assert(drm_set_key_context_key(kc, key, iv) == 0);
    /* 3 whole blocks + 5 trailing bytes that must pass through untouched */
    uint8_t sample[53]; memcpy(sample, buf, 48); memset(sample + 48, 0xAA, 5);
    assert(drm_decrypt_sample_at(kc, sample, sizeof sample, 0) == 0);
    assert(memcmp(sample, plain, 48) == 0);
    for (int i = 48; i < 53; i++) assert(sample[i] == 0xAA);
    drm_close_key_context(kc);   /* ASan flags a leak or double free here */
    drm_close_key_context(NULL); /* must be a no-op */
}

static void test_account_json_escaped(void)
{
    char dir[] = "/tmp/drmtestXXXXXX"; assert(mkdtemp(dir));
    write_file(dir, "MUSIC_TOKEN", "ab\"c\\d\n", 7);
    write_file(dir, "STOREFRONT_ID", "us\n", 3);
    struct drm_config cfg; memset(&cfg, 0, sizeof cfg); cfg.base_directory = dir;
    assert(drm_init(&cfg) == 0);
    char *j = drm_get_account();
    assert(j);
    assert(strstr(j, "\"music_token\":\"ab\\\"c\\\\d\"") != NULL);
    assert(strstr(j, "\"storefront_id\":\"us\"") != NULL);
    free(j);
    drm_shutdown();
}

static void test_oversized_file_rejected(void)
{
    char dir[] = "/tmp/drmtestXXXXXX"; assert(mkdtemp(dir));
    char *big = malloc(70000); memset(big, 'a', 70000);
    write_file(dir, "MUSIC_TOKEN", big, 70000);
    free(big);
    struct drm_config cfg; memset(&cfg, 0, sizeof cfg); cfg.base_directory = dir;
    drm_init(&cfg);
    assert(drm_get_account() == NULL);
    drm_shutdown();
}

int main(void)
{
    test_roundtrip_and_close();
    test_account_json_escaped();
    test_oversized_file_rejected();
    puts("drm_linux: all tests passed");
    return 0;
}
