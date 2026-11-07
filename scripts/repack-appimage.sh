#!/usr/bin/env bash
# repack-appimage.sh — rebuild an electron-builder AppImage on the static type2 runtime.
#
# electron-builder's bundled runtime dlopen()s libfuse.so.2 and needs the host's libc, so the AppImage
# does not start on systems without libfuse2 (every current Ubuntu/Fedora default). The type2 runtime is
# statically linked and falls back to extract-and-run, so it needs neither. Both tools are pinned to a
# release and checked against its published SHA-256 before use.
#
# Usage: scripts/repack-appimage.sh IN.AppImage OUT.AppImage
set -euo pipefail

IN="${1:?usage: repack-appimage.sh IN.AppImage OUT.AppImage}"
OUT="${2:?usage: repack-appimage.sh IN.AppImage OUT.AppImage}"

RUNTIME_URL="https://github.com/AppImage/type2-runtime/releases/download/20251108/runtime-x86_64"
RUNTIME_SHA256="2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d"
TOOL_URL="https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-x86_64.AppImage"
TOOL_SHA256="ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fetch() { # url sha256 dest
    curl -fsSL --retry 3 -o "$3" "$1"
    echo "$2  $3" | sha256sum -c --status || { echo "error: checksum mismatch for $1" >&2; exit 1; }
}
fetch "$RUNTIME_URL" "$RUNTIME_SHA256" "$work/runtime"
fetch "$TOOL_URL" "$TOOL_SHA256" "$work/appimagetool"
chmod +x "$work/appimagetool"

# --appimage-extract needs no FUSE; run the tool the same way so CI containers need none either.
( cd "$work" && "$(cd "$(dirname "$IN")" && pwd)/$(basename "$IN")" --appimage-extract >/dev/null )
ARCH=x86_64 "$work/appimagetool" --appimage-extract-and-run --no-appstream \
    --runtime-file "$work/runtime" "$work/squashfs-root" "$OUT"
chmod +x "$OUT"
echo "repacked → $OUT"
