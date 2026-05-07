#!/usr/bin/env bash
# Shared helpers for Gokrazy setup/build scripts.
#
# Sourced by:
#   - setup-gokrazy.sh           (interactive SD card provisioning)
#   - scripts/build-ota-image.sh (CI / OTA image builds)
#
# Provides:
#   GOK_PACKAGES_FILE       absolute path to scripts/gok-packages.txt
#   gok_packages            array of packages to register via `gok add`
#   gok_config_packages     array of fully-qualified package paths for config.json "Packages"
#   MODULE_PATH             Go module path of this repository
#
# This file intentionally avoids `set -e`; callers manage their own shell options.

# Resolve the directory containing this helper, regardless of where it is sourced from.
_GOK_COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
GOK_PACKAGES_FILE="${_GOK_COMMON_DIR}/gok-packages.txt"

MODULE_PATH="github.com/denysvitali/pictures-sync-s3"

if [ ! -f "$GOK_PACKAGES_FILE" ]; then
  echo "Error: gok-packages.txt not found at $GOK_PACKAGES_FILE" >&2
  return 1 2>/dev/null || exit 1
fi

gok_packages=()
gok_config_packages=()

while IFS= read -r line || [ -n "$line" ]; do
  # Strip leading/trailing whitespace and comments.
  line="${line%%#*}"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [ -z "$line" ] && continue

  gok_packages+=("$line")
  if [[ "$line" == ./* ]]; then
    gok_config_packages+=("${MODULE_PATH}/${line#./}")
  else
    gok_config_packages+=("$line")
  fi
done < "$GOK_PACKAGES_FILE"

# emit_packages_json prints the package array as a JSON list (no trailing comma).
emit_packages_json() {
  local indent="${1:-    }"
  local i n=${#gok_config_packages[@]}
  for ((i = 0; i < n; i++)); do
    if [ $((i + 1)) -eq "$n" ]; then
      printf '%s"%s"\n' "$indent" "${gok_config_packages[$i]}"
    else
      printf '%s"%s",\n' "$indent" "${gok_config_packages[$i]}"
    fi
  done
}
