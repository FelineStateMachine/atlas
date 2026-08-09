#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: smoke-linux.sh <package.deb> <version> <repo-root>" >&2
  exit 2
fi

package=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
version=$2
root=$3
test -f "$package"
deb_version=${version#v}
if [[ ! "$deb_version" =~ ^[0-9] ]]; then
  deb_version="0~$deb_version"
fi
stage=$(mktemp -d)
app_pid=""
cleanup() {
  if [[ -n "$app_pid" ]]; then kill "$app_pid" >/dev/null 2>&1 || true; fi
  rm -rf "$stage"
}
trap cleanup EXIT

sudo apt-get install -y "$package" xvfb dbus-x11
dpkg-query -W -f='${Status}\n${Version}\n${Architecture}\n' atlas-desktop \
  | grep -Fx 'install ok installed'
dpkg-query -W -f='${Version}' atlas-desktop | grep -Fx "$deb_version"
dpkg-query -W -f='${Architecture}' atlas-desktop | grep -Fx amd64
test -x /usr/bin/Atlas
test -f /usr/share/applications/dev.felinestatemachine.Atlas.desktop
test -f /usr/share/mime/packages/dev.felinestatemachine.atlas.xml

go run "$root/cmd/atlas" build -cache "$stage/cache" -bundles "$stage/source" \
  "$root/examples/sample-region.atlas-project"
artifact=$(find "$stage/source" -maxdepth 1 -type f -name 'sample-region-*.atlas' -print -quit)
test -n "$artifact"
xdg_type=$(xdg-mime query filetype "$artifact" || true)
echo "headless xdg-mime diagnostic: $xdg_type"
# xdg-mime selects its backend from the desktop session and falls back to the
# file(1) database on a headless runner, which cannot see a package's
# shared-mime-info additions. GIO is the desktop stack this package registers.
file_type=$(gio info --attributes=standard::content-type "$artifact" \
  | sed -n 's/^  standard::content-type: //p')
echo "installed Atlas GIO MIME type: $file_type"
[[ "$file_type" == application/vnd.felinestatemachine.atlas ]] || {
  echo "unexpected Atlas MIME type: $file_type" >&2
  exit 1
}
handlers=$(gio mime application/vnd.felinestatemachine.atlas)
echo "$handlers"
grep -F 'dev.felinestatemachine.Atlas.desktop' <<<"$handlers" >/dev/null || {
  echo "Atlas desktop handler is not registered" >&2
  exit 1
}

mkdir -p "$stage/library" "$stage/data"
ATLAS_BUNDLES_DIR="$stage/library" ATLAS_DATA_DIR="$stage/data" \
  dbus-run-session -- xvfb-run -a /usr/bin/Atlas "$artifact" \
  >"$stage/atlas.log" 2>&1 &
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
