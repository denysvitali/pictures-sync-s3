#!/usr/bin/env bash

set -euo pipefail

EXFATPROGS_VERSION="${EXFATPROGS_VERSION:-1.3.2}"
EXFATPROGS_SHA256="${EXFATPROGS_SHA256:-0c5d445947df781f90ba6bfddbd323bd6324c78a51fe75380a6ce2238c3cbcce}"
OUTPUT_DIR="${OUTPUT_DIR:-$PWD/dist/exfatprogs-arm64}"
BUILD_DIR="${BUILD_DIR:-$PWD/.exfatprogs-build}"

sudo apt-get update
sudo apt-get install -y --no-install-recommends \
  autoconf \
  automake \
  ca-certificates \
  curl \
  gcc \
  libtool \
  make \
  pkg-config

rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
mkdir -p "$BUILD_DIR" "$OUTPUT_DIR"

archive="$BUILD_DIR/exfatprogs-${EXFATPROGS_VERSION}.tar.gz"
curl -fsSL "https://github.com/exfatprogs/exfatprogs/releases/download/${EXFATPROGS_VERSION}/exfatprogs-${EXFATPROGS_VERSION}.tar.gz" -o "$archive"
printf '%s  %s\n' "$EXFATPROGS_SHA256" "$archive" | sha256sum -c -

tar -C "$BUILD_DIR" -xzf "$archive"
cd "$BUILD_DIR/exfatprogs-${EXFATPROGS_VERSION}"

./configure --prefix=/usr --disable-shared --enable-static
make -j"$(nproc)" CC=gcc LDFLAGS=-static

install -m 0755 mkfs/mkfs.exfat "$OUTPUT_DIR/mkfs.exfat"
file "$OUTPUT_DIR/mkfs.exfat"

if ! file "$OUTPUT_DIR/mkfs.exfat" | grep -q 'ARM aarch64'; then
  echo "Error: built mkfs.exfat is not an arm64 binary"
  exit 1
fi

if ! file "$OUTPUT_DIR/mkfs.exfat" | grep -q 'statically linked'; then
  echo "Error: built mkfs.exfat is not static; dynamic libraries would be missing on gokrazy"
  exit 1
fi

echo "EXFAT_MKFS_BINARY=$OUTPUT_DIR/mkfs.exfat" >> "${GITHUB_ENV:-/dev/null}"
echo "exfat_mkfs_binary=$OUTPUT_DIR/mkfs.exfat" >> "${GITHUB_OUTPUT:-/dev/null}"
