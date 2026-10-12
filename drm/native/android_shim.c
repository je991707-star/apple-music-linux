/*
 * android_shim.c — maps the hybris public API used by hybris_stubs.c onto the
 * in-memory embedded loader.  hybris_stubs.c is compiled with
 * -Dandroid_dlopen=aml_android_dlopen (etc.) so these never collide with the
 * real libhybris-core symbols that are loaded into the same process.
 */
#include <string.h>
#include "../embedded_loader.h"

void *aml_android_dlopen(const char *filename, int flag)
{
    return embedded_dlopen(filename, flag);
}

void *aml_android_dlsym(void *handle, const char *symbol)
{
    return embedded_dlsym_in(handle, symbol);
}

const char *aml_android_dlerror(void)
{
    return embedded_dlerror();
}
