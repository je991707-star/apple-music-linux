#!/usr/bin/env bash
# build-ffmpeg-windows.sh — regenerates third_party/ffmpeg-win, the prebuilt minimal FFmpeg the Windows
# build bundles (resources\ffmpeg\bin\ffmpeg.exe). Normal builds do NOT run this: the result is committed
# and scripts/build-windows.ps1 just copies it. Run it only to upgrade FFmpeg or change the component list
# (scripts/ffmpeg-config.sh, shared with the Linux build).
#
# The same LGPL v2.1 build as Linux with no external codec libraries; the engine's music-video remux, export
# and tagging runs use it. HEVC-to-H.264 transcoding needs libx264 and is therefore not available; the app
# stream-copies H.264.
#
# Run it from an MSYS2 UCRT64 shell with: gcc make nasm diffutils curl tar xz and zlib, e.g.
#   pacman -S --needed mingw-w64-ucrt-x86_64-{gcc,make,nasm,zlib} diffutils curl tar xz

set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
. "$REPO/scripts/ffmpeg-config.sh"
WORK="$REPO/.ffmpeg-build-win"
OUT="$REPO/third_party/ffmpeg-win"
JOBS="$(nproc 2>/dev/null || echo 4)"

die() { echo "Error: $*" >&2; exit 1; }
for t in curl tar xz sha256sum gcc make nasm objdump; do command -v "$t" >/dev/null 2>&1 || die "'$t' is required (run from an MSYS2 UCRT64 shell)"; done
[ "${MSYSTEM:-}" = UCRT64 ] || die "run this from an MSYS2 UCRT64 shell (MSYSTEM=UCRT64), not '${MSYSTEM:-none}'"

mkdir -p "$WORK"
TARBALL="$WORK/ffmpeg-$FFMPEG_VERSION.tar.xz"
[ -f "$TARBALL" ] || curl -fL --retry 3 -o "$TARBALL" "$FFMPEG_URL"
echo "$FFMPEG_SHA256  $TARBALL" | sha256sum -c - >/dev/null || die "checksum mismatch for $TARBALL"

rm -rf "$WORK/src" && mkdir -p "$WORK/src"
tar -xf "$TARBALL" -C "$WORK/src" --strip-components=1
cd "$WORK/src"

STAGE="$WORK/stage"
rm -rf "$STAGE"
./configure --prefix="$STAGE" --target-os=mingw32 --arch=x86_64 --extra-ldflags=-static-libgcc \
    "${FFMPEG_COMMON_FLAGS[@]}"
make -j"$JOBS"
make install

rm -rf "$OUT"
mkdir -p "$OUT/bin"
cp "$STAGE/bin/ffmpeg.exe" "$STAGE"/bin/*.dll "$OUT/bin/"
strip --strip-unneeded "$OUT"/bin/*.exe "$OUT"/bin/*.dll 2>/dev/null || true

# Bundle every non-system DLL the binaries import (the MSYS2 runtime ones, such as zlib), so the result runs
# on a machine without MSYS2. System DLLs (kernel32, ucrt, ...) are left to Windows.
bundled() { ls "$OUT/bin" | tr 'A-Z' 'a-z'; }
changed=1
while [ "$changed" = 1 ]; do
    changed=0
    for f in "$OUT"/bin/*.exe "$OUT"/bin/*.dll; do
        for dep in $(objdump -p "$f" | sed -n 's/^\s*DLL Name: //p'); do
            lower=$(echo "$dep" | tr 'A-Z' 'a-z')
            bundled | grep -qx "$lower" && continue
            src="/ucrt64/bin/$dep"
            if [ -f "$src" ]; then cp "$src" "$OUT/bin/"; changed=1; echo "bundled $dep"; fi
        done
    done
done

cp COPYING.LGPLv2.1 "$OUT/COPYING.LGPLv2.1"
cat > "$OUT/README.md" <<EOT
# Prebuilt minimal FFmpeg (Windows)

FFmpeg $FFMPEG_VERSION, built from $FFMPEG_URL
(sha256 $FFMPEG_SHA256) by scripts/build-ffmpeg-windows.sh as an LGPL v2.1 build with no external codec
libraries. The corresponding source is that tarball; the exact configure flags are in scripts/ffmpeg-config.sh.
The DLLs are the shared libraries ffmpeg.exe loads; they must stay next to it.
Do not edit these files: regenerate them with the script.
EOT
echo ""
echo "Done: $OUT"
du -sh "$OUT"/bin | cat
"$OUT/bin/ffmpeg.exe" -hide_banner -version | head -1
