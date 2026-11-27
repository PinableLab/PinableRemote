#!/usr/bin/env bash
# Experimental Linux test SDK only. No Windows release artifact is produced.
set -euo pipefail
cd "$(dirname "$0")/.."
LAB="$PWD"
mkdir -p tmp sdk logs
export TMPDIR="$LAB/tmp"
if [ -x "$LAB/sdk/tools/usr/bin/cmake" ]; then
  export PATH="$LAB/sdk/tools/usr/bin:$PATH"
  export LD_LIBRARY_PATH="$LAB/sdk/tools/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
fi
archive=sdk/freerdp-3.33.1.tar.gz
if [ ! -s "$archive" ]; then
  curl --fail --silent --show-error --location https://pub.freerdp.com/releases/freerdp-3.33.1.tar.gz -o "$archive.part"
  mv "$archive.part" "$archive"
fi
printf '%s  %s\n' 4431bbf04d370545cfe2924b9d9f116b95c90c64ff156b2550bbaf82d34e7124 "$archive" | sha256sum -c -
tar -xzf "$archive" -C sdk
cmake -S sdk/freerdp-3.33.1 -B build -G Ninja \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$LAB/out" -DCMAKE_EXPORT_NO_PACKAGE_REGISTRY=ON \
  -DWITH_CLIENT=OFF -DWITH_SERVER=OFF -DWITH_PROXY=OFF -DWITH_SHADOW=OFF -DWITH_SAMPLE=OFF \
  -DWITH_CHANNELS=OFF -DWITH_X11=OFF -DWITH_SDL=OFF -DWITH_WAYLAND=OFF -DWITH_CUPS=OFF \
  -DWITH_ALSA=OFF -DWITH_PULSE=OFF -DWITH_FFMPEG=OFF -DWITH_SWSCALE=OFF -DWITH_VIDEO_FFMPEG=OFF \
  -DWITH_DSP_FFMPEG=OFF -DWITH_OPENH264=OFF -DWITH_JPEG=OFF -DWITH_PCSC=OFF -DWITH_SMARTCARD_PCSC=OFF \
  -DWITH_SMARTCARD_EMULATE=OFF -DWITH_OPUS=OFF -DWITH_KRB5=OFF -DWITH_FUSE=OFF -DWITH_WEBVIEW=OFF \
  -DWITH_WINPR_TOOLS=OFF -DWITH_MANPAGES=OFF -DWITH_AAD=OFF -DWITH_OPENSSL=ON -DWITH_IPP=OFF \
  -DWITH_UNICODE_BUILTIN=ON -DBUILD_TESTING=OFF > logs/config.log 2>&1
cmake --build build --parallel "${JOBS:-8}" > logs/build.log 2>&1
cmake --install build > logs/install.log 2>&1
