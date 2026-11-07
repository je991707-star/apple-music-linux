# Prebuilt minimal FFmpeg (Windows)

FFmpeg 9.0.2, built from https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz
(sha256 8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e) by scripts/build-ffmpeg-windows.sh as an LGPL v2.1 build with no external codec
libraries. The corresponding source is that tarball; the exact configure flags are in scripts/ffmpeg-config.sh.
The DLLs are the shared libraries ffmpeg.exe loads; they must stay next to it.
Do not edit these files: regenerate them with the script.
