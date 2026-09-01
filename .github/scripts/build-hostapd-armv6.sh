#!/usr/bin/env bash

set -euo pipefail

HOSTAPD_VERSION="${HOSTAPD_VERSION:-2.11}"
HOSTAPD_SHA256="${HOSTAPD_SHA256:-2b3facb632fd4f65e32f4bf82a76b4b72c501f995a4f62e330219fe7aed1747a}"
OUTPUT_DIR="${OUTPUT_DIR:-$PWD/dist/hostapd-armv6}"
BUILD_DIR="${BUILD_DIR:-$PWD/.hostapd-build}"
LIBNL_VERSION="${LIBNL_VERSION:-3.11.0}"
SYSROOT_DIR="$BUILD_DIR/sysroot"

sudo apt-get update
sudo apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  autoconf \
  automake \
  bison \
  flex \
  gcc-arm-linux-gnueabi \
  git \
  libtool \
  make \
  pkg-config

rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
mkdir -p "$BUILD_DIR" "$OUTPUT_DIR"

# Build libnl for ARMv6 first. Ubuntu's armhf packages target ARMv7 and cannot
# execute on the original Raspberry Pi, so all target code is cross-compiled
# with the soft-float armel toolchain and linked statically.
git clone --depth=1 --branch "libnl${LIBNL_VERSION//./_}" \
  https://github.com/thom311/libnl.git "$BUILD_DIR/libnl"
(
  cd "$BUILD_DIR/libnl"
  ./autogen.sh
  ./configure \
    --host=arm-linux-gnueabi \
    --prefix="$SYSROOT_DIR" \
    --disable-shared \
    --enable-static \
    CFLAGS="-Os -march=armv6"
  make -j"$(nproc)"
  make install
)

archive="$BUILD_DIR/hostapd-${HOSTAPD_VERSION}.tar.gz"
curl -fsSL "https://w1.fi/releases/hostapd-${HOSTAPD_VERSION}.tar.gz" -o "$archive"
printf '%s  %s\n' "$HOSTAPD_SHA256" "$archive" | sha256sum -c -

tar -C "$BUILD_DIR" -xzf "$archive"
cd "$BUILD_DIR/hostapd-${HOSTAPD_VERSION}/hostapd"

cp defconfig .config
cat >> .config <<'EOF'
CONFIG_DRIVER_NL80211=y
CONFIG_LIBNL32=y
CONFIG_IEEE80211N=y
CONFIG_IEEE80211AC=y
CONFIG_IEEE80211AX=y
CFLAGS += -Os -march=armv6
LDFLAGS += -static
EOF

make -j"$(nproc)" \
  CC=arm-linux-gnueabi-gcc \
  PKG_CONFIG_LIBDIR="$SYSROOT_DIR/lib/pkgconfig" \
  hostapd

install -m 0755 hostapd "$OUTPUT_DIR/hostapd"
file "$OUTPUT_DIR/hostapd"

if ! file "$OUTPUT_DIR/hostapd" | grep -q 'ARM, EABI5'; then
  echo "Error: built hostapd is not a 32-bit ARM EABI5 binary"
  exit 1
fi

if ! file "$OUTPUT_DIR/hostapd" | grep -q 'statically linked'; then
  echo "Error: built hostapd is not static; dynamic libraries would be missing on gokrazy"
  exit 1
fi

echo "HOSTAPD_BINARY=$OUTPUT_DIR/hostapd" >> "${GITHUB_ENV:-/dev/null}"
echo "hostapd_binary=$OUTPUT_DIR/hostapd" >> "${GITHUB_OUTPUT:-/dev/null}"
