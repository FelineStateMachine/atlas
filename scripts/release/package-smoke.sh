#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: package-smoke.sh <dist> <version> <repo-root>" >&2
  exit 2
fi

dist=$1
version=$2
root=$3
smoke=$(mktemp -d)
trap 'rm -rf "$smoke"' EXIT

unzip -Z1 "$dist/Atlas-$version-darwin-arm64.zip" | grep -Fx 'Atlas.app/Contents/Info.plist'
unzip -Z1 "$dist/Atlas-$version-darwin-arm64.zip" | grep -Fx 'Atlas.app/Contents/MacOS/Atlas'
dpkg-deb --info "$dist/Atlas-$version-linux-amd64.deb" >/dev/null
dpkg-deb --contents "$dist/Atlas-$version-linux-amd64.deb" | grep -F 'usr/share/applications/dev.felinestatemachine.Atlas.desktop'
dpkg-deb --contents "$dist/Atlas-$version-linux-amd64.deb" | grep -F 'usr/share/mime/packages/dev.felinestatemachine.atlas.xml'

tar -xzf "$dist/atlas-cli-$version-linux-amd64.tar.gz" -C "$smoke"
cli=$(find "$smoke" -type f -name atlas -perm -u+x -print -quit)
[[ -n "$cli" ]]
"$cli" --help >/dev/null
"$cli" --version | grep -F "$version"

cache="$smoke/cache"
online="$smoke/online"
offline="$smoke/offline"
mkdir -p "$cache" "$online" "$offline"
"$cli" build -cache "$cache" -bundles "$online" "$root/examples/sample-region.atlas-project"
"$cli" build -offline -cache "$cache" -bundles "$offline" "$root/examples/sample-region.atlas-project"
online_atlas=$(find "$online" -maxdepth 1 -type f -name '*.atlas' -print -quit)
offline_atlas=$(find "$offline" -maxdepth 1 -type f -name '*.atlas' -print -quit)
[[ -n "$online_atlas" && -n "$offline_atlas" ]]
cmp "$online_atlas" "$offline_atlas"
"$cli" measure -bundles "$online" -json sample-region | grep -F 'sample-region'
