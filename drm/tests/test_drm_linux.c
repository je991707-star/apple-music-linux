/* Standalone test for drm_linux.c. Build+run:
 *   gcc -std=gnu11 -fsanitize=address,undefined -g -I. -o /tmp/t_drm tests/test_drm_linux.c drm_linux.c -lpthread && /tmp/t_drm */
#include "../drm_types.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

char *drm_get_account(void);
int drm_init(const struct drm_config *cfg);
void drm_shutdown(void);

static void write_file(const char *dir, const char *name, const char *body, size_t n)
{
    char p[4096];
    snprintf(p, sizeof p, "%s/%s", dir, name);
    FILE *f = fopen(p, "w");
    assert(f);
    fwrite(body, 1, n, f);
    fclose(f);
}

static void test_account_json_escaped(void)
{
    char dir[] = "/tmp/drmtestXXXXXX";
    assert(mkdtemp(dir));
    write_file(dir, "MUSIC_TOKEN", "ab\"c\\d\n", 7);
    write_file(dir, "STOREFRONT_ID", "us\n", 3);
    struct drm_config cfg;
    memset(&cfg, 0, sizeof cfg);
    cfg.base_directory = dir;
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
    char dir[] = "/tmp/drmtestXXXXXX";
    assert(mkdtemp(dir));
    char *big = malloc(70000);
    memset(big, 'a', 70000);
    write_file(dir, "MUSIC_TOKEN", big, 70000);
    free(big);
    struct drm_config cfg;
    memset(&cfg, 0, sizeof cfg);
    cfg.base_directory = dir;
    drm_init(&cfg);
    assert(drm_get_account() == NULL);
    drm_shutdown();
}

int main(void)
{
    test_account_json_escaped();
    test_oversized_file_rejected();
    puts("drm_linux: all tests passed");
    return 0;
}
