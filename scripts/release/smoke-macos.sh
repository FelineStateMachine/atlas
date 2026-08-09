#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: smoke-macos.sh <package.zip> <version> <repo-root>" >&2
  exit 2
fi

package=$1
version=$2
root=$3
version_number=${version#v}
mac_version=0.0.0
if [[ "$version_number" =~ ^[0-9]+(\.[0-9]+){0,2} ]]; then
  mac_version=${BASH_REMATCH[0]}
fi
stage=$(mktemp -d)
app_pid=""
cleanup() {
  if [[ -n "$app_pid" ]]; then kill "$app_pid" >/dev/null 2>&1 || true; fi
  rm -rf "$stage"
}
trap cleanup EXIT

ditto -x -k "$package" "$stage/unpacked"
app="$stage/unpacked/Atlas.app"
executable="$app/Contents/MacOS/Atlas"
test -x "$executable"
codesign --verify --deep --strict --verbose=2 "$app"
[[ "$(plutil -extract CFBundleIdentifier raw "$app/Contents/Info.plist")" == dev.felinestatemachine.atlas ]]
[[ "$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist")" == "$mac_version" ]]
plutil -extract UTExportedTypeDeclarations.0.UTTypeIdentifier raw "$app/Contents/Info.plist" \
  | grep -Fx dev.felinestatemachine.atlas.volume
"$executable" --version | grep -Fx "Atlas $version"

go run "$root/cmd/atlas" build -cache "$stage/cache" -bundles "$stage/source" \
  "$root/examples/sample-region.atlas-project"
artifact=$(find "$stage/source" -maxdepth 1 -type f -name 'sample-region-*.atlas' -print -quit)
test -n "$artifact"
mkdir -p "$stage/library" "$stage/data"
ATLAS_BUNDLES_DIR="$stage/library" ATLAS_DATA_DIR="$stage/data" \
  "$executable" "$artifact" >"$stage/atlas.log" 2>&1 &
app_pid=$!
for _ in $(seq 1 60); do
  if find "$stage/library" -maxdepth 1 -type f -name 'sample-region-*.atlas' -print -quit | grep -q .; then
    break
  fi
  kill -0 "$app_pid" 2>/dev/null || { cat "$stage/atlas.log" >&2; exit 1; }
  sleep 1
done
installed=$(find "$stage/library" -maxdepth 1 -type f -name 'sample-region-*.atlas' -print -quit)
test -n "$installed"
cmp "$artifact" "$installed"
