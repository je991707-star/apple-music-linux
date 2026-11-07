#!/usr/bin/env bash
# build-vlc-ffmpeg.sh — minimal FFmpeg libraries that match the system VLC's avcodec/avformat plugins.
#
# VLC's plugins link the FFmpeg that its distribution shipped (libavcodec.so.58 on Ubuntu 22.04), and only
# a library with that exact soname loads. third_party/ffmpeg is FFmpeg 9 (.so.63), which matches the VLC on
# a rolling distro but not on 22.04, where release packages must be built so they run on older systems.
# Bundling the distro's own libavcodec would drag in ~120 libraries (GTK, x265, librsvg: 145 MB), so build
# the same FFmpeg version from the distro's source package with only the audio decoders the app uses,
# LGPL, no external codec libraries (--disable-autodetect). The result sits next to VLC in the bundle.
#
# Usage: scripts/build-vlc-ffmpeg.sh OUT_DIR
# Source: the Debian/Ubuntu `ffmpeg` source package (security patches included). Set FFMPEG_SRC to an
# already-unpacked source tree, or have deb-src enabled and apt-get update done (needs dpkg-dev).
# Needs: gcc, make, nasm, patchelf, zlib1g-dev.

set -euo pipefail

OUT="${1:?usage: build-vlc-ffmpeg.sh OUT_DIR}"
mkdir -p "$OUT" && OUT="$(cd "$OUT" && pwd)"   # absolute: the build below runs in a temporary directory
PLUGINS="${ADAM_VLC_PLUGINS:-/usr/lib/x86_64-linux-gnu/vlc/plugins}"
JOBS="$(nproc 2>/dev/null || echo 4)"
for t in gcc make nasm patchelf; do command -v "$t" >/dev/null || { echo "error: '$t' is required" >&2; exit 1; }; done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

SRC="${FFMPEG_SRC:-}"
if [ -z "$SRC" ]; then
    (cd "$WORK" && apt-get source --download-only ffmpeg >/dev/null && dpkg-source -x ffmpeg_*.dsc src >/dev/null)
    SRC="$WORK/src"
fi
[ -x "$SRC/configure" ] || { echo "error: $SRC is not an FFmpeg source tree" >&2; exit 1; }

# The sonames the plugins were linked against; the build must produce exactly these.
WANT="$(ldd "$PLUGINS/codec/libavcodec_plugin.so" | grep -oE 'lib(avcodec|avutil|swresample)\.so\.[0-9]+' | sort -u)"
[ -n "$WANT" ] || { echo "error: could not read the plugin's FFmpeg sonames from $PLUGINS" >&2; exit 1; }

STAGE="$WORK/stage"
mkdir -p "$WORK/build" && cd "$WORK/build"   # out of tree: the source stays pristine
"$SRC/configure" --prefix="$STAGE" \
    --disable-autodetect --enable-zlib \
    --disable-everything --disable-doc --disable-debug --disable-static --enable-shared \
    --disable-network --disable-programs --disable-avdevice \
    --enable-pic --enable-small \
    --enable-protocol=file,pipe \
    --enable-demuxer=mov,flac,aac,mp3,wav,ac3,eac3,ogg \
    --enable-decoder=alac,aac,aac_latm,flac,mp3,mp3float,ac3,eac3,opus,vorbis,pcm_s16le,pcm_s24le,pcm_s32le,pcm_f32le \
    --enable-parser=aac,aac_latm,ac3,flac,mpegaudio,opus,vorbis >"$WORK/build.log" 2>&1 \
    && make -j"$JOBS" >>"$WORK/build.log" 2>&1 \
    && make install >>"$WORK/build.log" 2>&1 \
    || { echo "error: FFmpeg build failed; last lines of its log:" >&2; tail -30 "$WORK/build.log" >&2; exit 1; }

cp -a "$STAGE"/lib/lib*.so.* "$OUT/"
strip --strip-unneeded "$OUT"/lib*.so.*.* 2>/dev/null || true
for f in "$OUT"/lib*.so.*.*; do patchelf --set-rpath '$ORIGIN' "$f"; done

for so in $WANT; do
    [ -e "$OUT/$so" ] || { echo "error: built libraries lack $so, which the VLC plugin needs" >&2; exit 1; }
done
echo "FFmpeg for VLC → $OUT ($(du -sh "$OUT" | cut -f1)): $(ls "$OUT" | grep -E '\.so\.[0-9]+$' | tr '\n' ' ')"
