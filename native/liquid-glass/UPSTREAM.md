# Upstream

Vendored from https://github.com/hicccc77/electron-liquid-glass at commit `0684fe7fd74855e4b13bd3e8d7b6db40f91e132a` (version 0.3.0), MIT licensed
(see LICENSE). The npm package ships only a prebuilt binary, so the source is built here instead; the sources
were reviewed before vendoring (no networking, file, process or hook APIs; the only dynamic lookup is
`RtlGetVersion` for the OS build number).

Local differences from upstream: `package.json` is Adam's (private, no `install` script, build tools as
dev dependencies). Everything under `src/`, `index.js`, `index.d.ts` and `binding.gyp` is unmodified unless
listed here.

Build (Windows, needs Visual Studio Build Tools with the C++ workload and Python):

    cd native/liquid-glass && npm install && npm run prebuilds   # -> prebuilds/win32-x64/*.node

`scripts/build-windows.ps1` does this and ships the result in the installer.
