#!/bin/sh
# capture.sh runs every capture in golden/capture in the order they depend on
# each other and leaves golden/fixtures as the tree that is committed.
#
#   golden/capture/capture.sh [fixtures-dir]
#
# It reads the installed .atlas library and the crawl archive, and writes only
# into the fixtures directory and into a working directory it removes on the
# way out. It never writes to the library.
#
# ATLAS_GOLDEN_HELD names volumes to capture but not commit, and is empty by
# default: a city curated from the gitignored internal/arcgismap/cities_local.go
# is a personal location, so a machine that has one sets this to that city's
# slug -- in the environment, never in this file -- and its fixtures land in
# golden/fixtures/private, which git leaves alone.
#
# The city fixture is the one volume not installed in the library: it is built
# for the fixture from a public proof city and captured from where it was
# built. ATLAS_GOLDEN_CITY_DIR is that directory -- the measurements are taken
# over the whole committed set, city included, so a run without it would quietly
# drop a golden and is refused instead. golden/fixtures/README.md has the four
# commands that build it. ATLAS_GOLDEN_HELD_SOURCES keeps the arcgis-hub
# translator fixture out of the public tree because the archive beside the
# library holds the personal city; the committed one came from the city archive.
set -eu

OUT=${1:-golden/fixtures}
SLUGS=${ATLAS_GOLDEN_SLUGS:-"tunic cyberpunk-2077 fallout-new-vegas zelda-tears-of-the-kingdom mars"}
CITY=${ATLAS_GOLDEN_CITY-"bend-or"}
CITY_DIR=${ATLAS_GOLDEN_CITY_DIR-"dist/bundles"}
HELD=${ATLAS_GOLDEN_HELD-""}
HELD_SOURCES=${ATLAS_GOLDEN_HELD_SOURCES-"arcgis-hub"}

if [ -n "$CITY" ]; then
	# shellcheck disable=SC2012 # the names are the pipeline's own, and sort
	CITY_FILE=$(ls "$CITY_DIR"/"$CITY"-*.atlas 2>/dev/null | tail -1 || true)
	if [ -z "$CITY_FILE" ]; then
		echo "capture: no $CITY bundle in $CITY_DIR." >&2
		echo "capture: build it (golden/fixtures/README.md, 'The city fixture')," >&2
		echo "capture: point ATLAS_GOLDEN_CITY_DIR at it, or set ATLAS_GOLDEN_CITY" >&2
		echo "capture: empty to capture the rest of the set without it." >&2
		exit 1
	fi
fi

WORK=$(mktemp -d)
SERVER=""
cleanup() {
	[ -n "$SERVER" ] && kill "$SERVER" 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

echo "capture: fixtures -> $OUT"
echo "capture: working directory $WORK"

# 1. Bundle fixtures, straight from the library's own fold, and the city from
# the directory it was built into.
# shellcheck disable=SC2086
go run ./golden/capture/bundles -out "$OUT" -private "$(echo $HELD | tr ' ' ',')" $SLUGS $HELD
if [ -n "$CITY" ]; then
	go run ./golden/capture/bundles -dir "$CITY_DIR" -out "$OUT" "$CITY"
fi

# A registry holding exactly the fixture set, so the measurements and the
# recorded catalog answer for the fixtures and for nothing else.
mkdir -p "$WORK/registry" "$WORK/registry-held" "$WORK/home"
for slug in $SLUGS; do
	path=$(go run ./golden/capture/survey -paths "$slug" 2>/dev/null)
	ln -s "$path" "$WORK/registry/$(basename "$path")"
done
if [ -n "$CITY" ]; then
	ln -s "$(cd "$(dirname "$CITY_FILE")" && pwd)/$(basename "$CITY_FILE")" \
		"$WORK/registry/$(basename "$CITY_FILE")"
fi
for slug in $HELD; do
	path=$(go run ./golden/capture/survey -paths "$slug" 2>/dev/null)
	ln -s "$path" "$WORK/registry-held/$(basename "$path")"
done

# 2. Translator fixtures: one archived capture per source, translated. The
# city's archive is a second one, holding the source the first must withhold.
# shellcheck disable=SC2086
go run ./golden/capture/translators -out "$OUT" -private "$(echo $HELD_SOURCES | tr ' ' ',')"
if [ -n "${ATLAS_GOLDEN_CITY_ARCHIVE-}" ]; then
	go run ./golden/capture/translators -archive "$ATLAS_GOLDEN_CITY_ARCHIVE" -out "$OUT"
fi

# 3. Measurement fixtures: the report verbatim, and the same numbers structured.
mkdir -p "$OUT/measure"
go run ./tools/maturity -bundles "$WORK/registry" >"$OUT/measure/maturity.txt"
go run ./golden/capture/measure -bundles "$WORK/registry" -out "$OUT"
if [ -n "$HELD" ]; then
	mkdir -p "$OUT/private/measure"
	go run ./tools/maturity -bundles "$WORK/registry-held" >"$OUT/private/measure/maturity.txt"
	go run ./golden/capture/measure -bundles "$WORK/registry-held" -out "$OUT/private"
fi

# 4. HTTP fixtures: the headless application, serving the fixture registry.
# HOME is redirected so the run leaves no inspector.url behind for another
# checkout's parity sweep to find, and the listener takes any free port so a
# running application is never in the way.
go build -tags dev -o "$WORK/atlas-headless" .
HOME="$WORK/home" ATLAS_HEADLESS=1 ATLAS_BUNDLES_DIR="$WORK/registry" \
	"$WORK/atlas-headless" >"$WORK/headless.log" 2>&1 &
SERVER=$!
URL=""
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
	URL=$(sed -n 's/^atlas: headless at //p' "$WORK/headless.log" | head -1)
	[ -n "$URL" ] && break
	sleep 1
done
if [ -z "$URL" ]; then
	echo "capture: the headless application never announced an address" >&2
	cat "$WORK/headless.log" >&2
	exit 1
fi
go run ./golden/capture/http -base "$URL" -out "$OUT"
kill "$SERVER" 2>/dev/null || true
SERVER=""

echo "capture: done"
