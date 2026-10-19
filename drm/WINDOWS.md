# Windows/MSYS2 build

The DRM loader has a Windows branch for a MinGW-built `libhybris-core.dll`.
It uses `LoadLibraryA`/`GetProcAddress` for the core module and keeps the
Android ELF libraries on disk; those libraries are opened by the core's
`android_dlopen` export. The Linux build continues to embed the core in a
memfd.

From an MSYS2 **UCRT64** shell, install the local toolchain if needed and run:

```sh
pacman -S --needed mingw-w64-ucrt-x86_64-gcc \
  mingw-w64-ucrt-x86_64-winpthreads make
make MINGW_CC=gcc MINGW_CXX=g++ windows-build
```

`windows-loader-check` is the credential-free compiler-only check. The full
`windows-build` target produces `build-windows/libdrm_client.dll`; it also does
not load Android libraries, contact Apple services, or require account files.

At runtime, place these artifacts in the Windows DRM runtime directory:

```text
libdrm_client.dll
libhybris-core.dll       # Windows build from libhybris-win
hybris-linker/q.so       # Windows Q linker plugin from libhybris-win
rootfs/system/lib64/     # Android x86-64 FairPlay libraries
files/                   # per-user session state, never committed
```

The Windows loader calls `LoadLibraryA`/`GetProcAddress` for the core module.
`hybris_backend_init(..., hybris_core_path)` can supply an explicit core path;
when it is `NULL`, the loader uses `HYBRIS_CORE_PATH`, then
`libhybris-core.dll`. Android libraries remain on disk and are opened through
the core's `android_dlopen` export.
