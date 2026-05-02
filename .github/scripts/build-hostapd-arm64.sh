#!/usr/bin/env bash

set -euo pipefail

HOSTAPD_VERSION="${HOSTAPD_VERSION:-2.11}"
HOSTAPD_SHA256="${HOSTAPD_SHA256:-2b3facb632fd4f65e32f4bf82a76b4b72c501f995a4f62e330219fe7aed1747a}"
OUTPUT_DIR="${OUTPUT_DIR:-$PWD/dist/hostapd-arm64}"
BUILD_DIR="${BUILD_DIR:-$PWD/.hostapd-build}"

sudo apt-get update
sudo apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  gcc \
  libnl-3-dev \
  libnl-genl-3-dev \
  libssl-dev \
  make \
  pkg-config \
  zlib1g-dev

rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
mkdir -p "$BUILD_DIR" "$OUTPUT_DIR"

archive="$BUILD_DIR/hostapd-${HOSTAPD_VERSION}.tar.gz"
curl -fsSL "https://w1.fi/releases/hostapd-${HOSTAPD_VERSION}.tar.gz" -o "$archive"
printf '%s  %s\n' "$HOSTAPD_SHA256" "$archive" | sha256sum -c -

tar -C "$BUILD_DIR" -xzf "$archive"
cd "$BUILD_DIR/hostapd-${HOSTAPD_VERSION}/hostapd"

cp defconfig .config
cat >> .config <<'EOF'
CONFIG_DRIVER_NL80211=y
CONFIG_LIBNL32=y
CONFIG_TLS=openssl
CONFIG_IEEE80211N=y
CONFIG_IEEE80211AC=y
CONFIG_IEEE80211AX=y
CFLAGS += -Os
LDFLAGS += -static
EOF

make -j"$(nproc)" CC=gcc hostapd

install -m 0755 hostapd "$OUTPUT_DIR/hostapd"
file "$OUTPUT_DIR/hostapd"

if ! file "$OUTPUT_DIR/hostapd" | grep -q 'ARM aarch64'; then
  echo "Error: built hostapd is not an arm64 binary"
  exit 1
fi

if ! file "$OUTPUT_DIR/hostapd" | grep -q 'statically linked'; then
  echo "Error: built hostapd is not static; dynamic libraries would be missing on gokrazy"
  exit 1
fi

echo "HOSTAPD_BINARY=$OUTPUT_DIR/hostapd" >> "${GITHUB_ENV:-/dev/null}"
echo "hostapd_binary=$OUTPUT_DIR/hostapd" >> "${GITHUB_OUTPUT:-/dev/null}"
