#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: package-desktop.sh <linux-amd64|darwin-arm64> <version> <binary> <dist>" >&2
  exit 2
fi

target=$1
version=$2
binary=$3
dist=$4
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
version_number=${version#v}
mkdir -p "$dist"

case "$target" in
  darwin-arm64)
    mac_version=0.0.0
    if [[ "$version_number" =~ ^[0-9]+(\.[0-9]+){0,2} ]]; then
      mac_version=${BASH_REMATCH[0]}
    fi
    stage=$(mktemp -d)
    trap 'rm -rf "$stage"' EXIT
    app="$stage/Atlas.app"
    mkdir -p "$app/Contents/MacOS"
    cp "$binary" "$app/Contents/MacOS/Atlas"
    sed "s/@VERSION@/$mac_version/g" "$root/packaging/macos/Info.plist" > "$app/Contents/Info.plist"
    "$root/scripts/release/sign-macos.sh" "$app" "$dist/Atlas-$version-darwin-arm64.zip"
    ;;
  linux-amd64)
    deb_version=$version_number
    if [[ ! "$deb_version" =~ ^[0-9] ]]; then
      deb_version="0~$deb_version"
    fi
    stage=$(mktemp -d)
    trap 'rm -rf "$stage"' EXIT
    mkdir -p \
      "$stage/DEBIAN" \
      "$stage/usr/bin" \
      "$stage/usr/share/applications" \
      "$stage/usr/share/mime/packages" \
      "$stage/usr/share/doc/atlas-desktop"
    cp "$binary" "$stage/usr/bin/Atlas"
    cp "$root/packaging/linux/dev.felinestatemachine.Atlas.desktop" "$stage/usr/share/applications/"
    cp "$root/packaging/linux/dev.felinestatemachine.atlas.xml" "$stage/usr/share/mime/packages/"
    sed -e "s/@VERSION@/$deb_version/g" -e "s/@ARCH@/amd64/g" \
      "$root/packaging/linux/control" > "$stage/DEBIAN/control"
    install -m 0755 "$root/packaging/linux/postinst" "$stage/DEBIAN/postinst"
    install -m 0755 "$root/packaging/linux/postrm" "$stage/DEBIAN/postrm"
    cp "$root/LICENSE" "$stage/usr/share/doc/atlas-desktop/copyright"
    chmod 0755 "$stage/usr/bin/Atlas"
    dpkg-deb --root-owner-group --build "$stage" "$dist/Atlas-$version-linux-amd64.deb"
    ;;
  *)
    echo "unsupported desktop target: $target" >&2
    exit 2
    ;;
esac
