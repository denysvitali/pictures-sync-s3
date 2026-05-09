#!/usr/bin/env bash

set -euo pipefail

E2FSPROGS_VERSION="${E2FSPROGS_VERSION:-1.47.2}"
E2FSPROGS_SHA256="${E2FSPROGS_SHA256:-2347e3654a05dd2bb482ba78cb1f1f7b6e7dd9bedfff1654d8b619bb5210f927}"
OUTPUT_DIR="${OUTPUT_DIR:-$PWD/dist/e2fsprogs-arm64}"
BUILD_DIR="${BUILD_DIR:-$PWD/.e2fsprogs-build}"

sudo apt-get update
sudo apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  gcc \
  make \
  pkg-config

rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
mkdir -p "$BUILD_DIR" "$OUTPUT_DIR"

archive="$BUILD_DIR/e2fsprogs-${E2FSPROGS_VERSION}.tar.gz"
curl -fsSL "https://mirrors.edge.kernel.org/pub/linux/kernel/people/tytso/e2fsprogs/v${E2FSPROGS_VERSION}/e2fsprogs-${E2FSPROGS_VERSION}.tar.gz" -o "$archive"
printf '%s  %s\n' "$E2FSPROGS_SHA256" "$archive" | sha256sum -c -

tar -C "$BUILD_DIR" -xzf "$archive"
cd "$BUILD_DIR/e2fsprogs-${E2FSPROGS_VERSION}"

# Build with static linking; disable optional features that pull in libs we
# don't need on a gokrazy device (no NLS, no fuse, etc.).
./configure \
  --prefix=/usr \
  --disable-nls \
  --disable-fuse2fs \
  --disable-libuuid \
  --disable-libblkid \
  --disable-uuidd \
  --disable-tls
make -j"$(nproc)" LDFLAGS=-all-static

install -m 0755 misc/mke2fs "$OUTPUT_DIR/mke2fs"
file "$OUTPUT_DIR/mke2fs"

if ! file "$OUTPUT_DIR/mke2fs" | grep -q 'ARM aarch64'; then
  echo "Error: built mke2fs is not an arm64 binary"
  exit 1
fi

if ! file "$OUTPUT_DIR/mke2fs" | grep -q 'statically linked'; then
  echo "Error: built mke2fs is not static; dynamic libraries would be missing on gokrazy"
  exit 1
fi

echo "MKE2FS_BINARY=$OUTPUT_DIR/mke2fs" >> "${GITHUB_ENV:-/dev/null}"
echo "mke2fs_binary=$OUTPUT_DIR/mke2fs" >> "${GITHUB_OUTPUT:-/dev/null}"
