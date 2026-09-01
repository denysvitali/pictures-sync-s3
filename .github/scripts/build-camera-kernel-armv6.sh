#!/usr/bin/env bash

set -euo pipefail

KERNEL_REPOSITORY="${KERNEL_REPOSITORY:-https://github.com/gokrazy-community/kernel-rpi-os-32.git}"
KERNEL_REF="${KERNEL_REF:-586f0d5ec5c043d2ea2eb14dd7f12c4a6722a0b7}"
BUILD_DIR="${BUILD_DIR:-$PWD/.camera-kernel-build}"
OUTPUT_DIR="${OUTPUT_DIR:-$PWD/dist/camera-kernel-armv6}"
PATCH_FILE="${PATCH_FILE:-$PWD/.github/patches/kernel-enable-camera-filesystems.patch}"

rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
mkdir -p "$BUILD_DIR" "$OUTPUT_DIR"

git clone --filter=blob:none --no-checkout "$KERNEL_REPOSITORY" "$BUILD_DIR/kernel"
git -C "$BUILD_DIR/kernel" fetch --depth=1 origin "$KERNEL_REF"
git -C "$BUILD_DIR/kernel" checkout --detach FETCH_HEAD
git -C "$BUILD_DIR/kernel" submodule update --init --depth=1
git -C "$BUILD_DIR/kernel" apply --check "$PATCH_FILE"
git -C "$BUILD_DIR/kernel" apply "$PATCH_FILE"

(
  cd "$BUILD_DIR/kernel"
  go run ./cmd/compile
)

cp "$BUILD_DIR/kernel/go.mod" "$OUTPUT_DIR/go.mod"
cp "$BUILD_DIR/kernel/go.sum" "$OUTPUT_DIR/go.sum"
cp -a "$BUILD_DIR/kernel/dist" "$OUTPUT_DIR/dist"

test -s "$OUTPUT_DIR/dist/vmlinuz"
test -f "$OUTPUT_DIR/dist/bcm2708-rpi-b-plus.dtb"
test -f "$OUTPUT_DIR/dist/bcm2711-rpi-4-b.dtb"

echo "Built FAT/exFAT-enabled kernel package at $OUTPUT_DIR"
