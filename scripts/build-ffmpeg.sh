#!/usr/bin/env bash
# build-ffmpeg.sh — regenerates third_party/ffmpeg, the prebuilt minimal FFmpeg that every
# package bundles. Normal builds do NOT run this: the result is committed (Git LFS) and the
# packaging scripts just copy it. Run it only to upgrade FFmpeg or change the component list.
#
# It is FFmpeg 9.0.2 built from the official source, inside the freedesktop SDK, as an LGPL build, with only what the app
# uses, in two roles:
#   - bin/ffmpeg  the engine's music-video remux, export and tagging runs
#   - lib/        libavcodec/libavformat/libavutil... — the shared libraries VLC's bundled
#                 libavcodec plugin loads (ALAC and AAC decode). The plugin was linked against
#                 these exact sonames (libavcodec.so.63, libavutil.so.61, libavformat.so.63),
#                 which no other prebuilt FFmpeg provides.
# No external codec libraries are linked (--disable-autodetect), so there is nothing else to
# bundle. HEVC-to-H.264 transcoding needs libx264 and is therefore not available; the app
# stream-copies H.264, which is what it asks Apple for.
#
# It compiles inside the freedesktop SDK (org.freedesktop.Sdk 25.08: glibc 2.42, gcc 15, nasm) rather
# than on the host. A build made on a rolling distro links its brand-new glibc symbols (an Arch
# build needed GLIBC_2.43/2.44) and then cannot load in the Flatpak runtime or on any older
# system. FFMPEG_NATIVE=1 builds on the host instead, for a host-only result.
#
# Needs: flatpak with the org.freedesktop.Sdk//25.08 runtime, patchelf, curl, tar, xz.

set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
. "$REPO/scripts/ffmpeg-config.sh"   # pinned source + the component list, shared with build-ffmpeg-windows.sh
VERSION=$FFMPEG_VERSION SHA256=$FFMPEG_SHA256 URL=$FFMPEG_URL
WORK="$REPO/.ffmpeg-build"
OUT="$REPO/third_party/ffmpeg"
JOBS="$(nproc 2>/dev/null || echo 4)"

die() { echo "Error: $*" >&2; exit 1; }
for t in curl tar xz sha256sum patchelf; do command -v "$t" >/dev/null 2>&1 || die "'$t' is required"; done

mkdir -p "$WORK"
TARBALL="$WORK/ffmpeg-$VERSION.tar.xz"
[ -f "$TARBALL" ] || curl -fL --retry 3 -o "$TARBALL" "$URL"
echo "$SHA256  $TARBALL" | sha256sum -c - >/dev/null || die "checksum mismatch for $TARBALL"

rm -rf "$WORK/src" && mkdir -p "$WORK/src"
tar -xf "$TARBALL" -C "$WORK/src" --strip-components=1
cd "$WORK/src"

STAGE="$WORK/stage"
rm -rf "$STAGE"

CONFIGURE_FLAGS=(--prefix="$STAGE" "${FFMPEG_COMMON_FLAGS[@]}" --enable-pic)

# quoted so a prefix containing spaces survives the trip into the sandbox
BUILD_CMD="./configure $(printf '%q ' "${CONFIGURE_FLAGS[@]}") && make -j$JOBS && make install"
if [ "${FFMPEG_NATIVE:-0}" = 1 ]; then
    echo "Building on the host (FFMPEG_NATIVE=1): the result needs this machine's glibc."
    ./configure "${CONFIGURE_FLAGS[@]}" --disable-x86asm && make -j"$JOBS" && make install
else
    command -v flatpak >/dev/null 2>&1 || die "flatpak is required (or set FFMPEG_NATIVE=1)"
    flatpak info org.freedesktop.Sdk//25.08 >/dev/null 2>&1 \
        || die "install the SDK first: flatpak install flathub org.freedesktop.Sdk//25.08"
    flatpak run --filesystem="$WORK" --cwd="$WORK/src" --command=bash org.freedesktop.Sdk//25.08 -c "$BUILD_CMD"
fi

rm -rf "$OUT"
mkdir -p "$OUT/bin" "$OUT/lib"
cp "$STAGE/bin/ffmpeg" "$OUT/bin/"
cp -a "$STAGE"/lib/lib*.so.* "$OUT/lib/"   # soname links and real files; no unversioned dev links
strip --strip-unneeded "$OUT/bin/ffmpeg" "$OUT"/lib/*.so.* 2>/dev/null || true
# Find the bundled libraries next to the binary, and each library next to its siblings,
# whatever LD_LIBRARY_PATH says.
patchelf --set-rpath '$ORIGIN/../lib' "$OUT/bin/ffmpeg"
for f in "$OUT"/lib/*.so.*.*; do patchelf --set-rpath '$ORIGIN' "$f"; done
cp COPYING.LGPLv2.1 "$OUT/COPYING.LGPLv2.1"
cat > "$OUT/README.md" <<EOT
# Prebuilt minimal FFmpeg

FFmpeg $VERSION, built from $URL
(sha256 $SHA256) by scripts/build-ffmpeg.sh as an LGPL v2.1 build with no external codec
libraries. The corresponding source is that tarball; the exact configure flags are in the script.
Do not edit these files: regenerate them with the script.
EOT
echo ""
echo "Done: $OUT"
du -sh "$OUT"/bin/ffmpeg "$OUT"/lib | cat
