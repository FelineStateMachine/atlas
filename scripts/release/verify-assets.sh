#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: verify-assets.sh <dist> <version>" >&2
  exit 2
fi

dist=$1
version=$2
expected=(
  "Atlas-$version-darwin-arm64.zip"
  "Atlas-$version-linux-amd64.deb"
  "Atlas-$version-windows-amd64-setup.exe"
  "atlas-cli-$version-darwin-amd64.tar.gz"
  "atlas-cli-$version-darwin-arm64.tar.gz"
  "atlas-cli-$version-linux-amd64.tar.gz"
  "atlas-cli-$version-linux-arm64.tar.gz"
  "atlas-cli-$version-windows-amd64.zip"
)
wanted=$(mktemp)
actual=$(mktemp)
trap 'rm -f "$wanted" "$actual"' EXIT
printf '%s\n' "${expected[@]}" | sort > "$wanted"
find "$dist" -maxdepth 1 -type f ! -name SHA256SUMS -exec basename {} \; | sort > "$actual"
diff -u "$wanted" "$actual"

(
  cd "$dist"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${expected[@]}" > SHA256SUMS
    sha256sum --check SHA256SUMS
  else
    shasum -a 256 "${expected[@]}" > SHA256SUMS
    shasum -a 256 --check SHA256SUMS
  fi
)
[[ "$(wc -l < "$dist/SHA256SUMS")" -eq "${#expected[@]}" ]]
