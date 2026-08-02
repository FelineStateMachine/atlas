package format

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/golden/capture/canon"
)

// The fixtures this gate reads, and the shapes they are committed in.
//
// The row types below mirror golden/capture/bundles: an extraction is only a
// comparison instrument if a reader can write it again, so the gate re-emits
// each inventory through the same canon package the capture used and compares
// whole files rather than field by field. A row type that drifts from the
// capture's shows up immediately as a re-emission that does not match.

const (
	// fixturesDir is golden/fixtures, relative to this package.
	fixturesDir = "../fixtures"

	// registryDirEnv points at an installed library of .atlas files. Unset,
	// the registry half of the gate does not run; the always-on half needs
	// nothing but the committed extractions.
	registryDirEnv = "ATLAS_REGISTRY_DIR"

	// cityDirEnv and cityDirDefault are golden/capture/capture.sh's own
	// convention for the fixture volume the library never held: the public
	// proof city was built for the fixture, and is read from where it was
	// built. A volume whose FIXTURES.json entry carries builtFor is one of
	// those; nothing here carries a list of slugs.
	cityDirEnv     = "ATLAS_GOLDEN_CITY_DIR"
	cityDirDefault = "../../dist/bundles"

	// locationsNote and packedForm are constants of the extraction format,
	// copied from golden/capture/bundles so the gate can write the header
	// again rather than trusting the one it is checking.
	locationsNote = "the packed payload's own order, which is the order the viewer " +
		"reads it in; owner indexes the payload's collections array"
	packedForm = "ATLASLOC v3"
)

// fixtureSet is the part of FIXTURES.json the gate reads: which volumes are in
// the set, and which build of each one every extraction was taken from.
type fixtureSet struct {
	Volumes []fixtureVolume `json:"volumes"`
}

type fixtureVolume struct {
	Slug          string        `json:"slug"`
	Title         string        `json:"title"`
	File          string        `json:"file"`
	FileBytes     int64         `json:"fileBytes"`
	FileSHA256    string        `json:"fileSha256"`
	Stamp         string        `json:"stamp"`
	Stamp12       string        `json:"stamp12"`
	CreatedAt     string        `json:"createdAt"`
	Revision      int           `json:"revision"`
	FormatVersion int           `json:"formatVersion"`
	Conventions   int           `json:"conventions"`
	Counts        fixtureCounts `json:"counts"`
	Fixture       string        `json:"fixture"`

	// BuiltFor, when present, says this volume was built for the fixture
	// rather than found installed, and records how. It is what sends the
	// gate to the city directory for the file.
	BuiltFor json.RawMessage `json:"builtFor,omitempty"`
}

type fixtureCounts struct {
	Entries int `json:"entries"`
	Worlds  int `json:"worlds"`
	Tiles   int `json:"tiles"`
	Icons   int `json:"icons"`
	Points  int `json:"points"`
	Paths   int `json:"paths"`
	Areas   int `json:"areas"`
}

// dir is where this volume's extractions live.
func (v fixtureVolume) dir() string { return filepath.Join(fixturesDir, filepath.FromSlash(v.Fixture)) }

// volumeHeader is the part of a volume.json extraction the gate reads: the
// hash of every archive part, the archive's entry order, and the pyramids the
// tile inventories are inventories of.
type volumeHeader struct {
	Slug       string            `json:"slug"`
	File       string            `json:"file"`
	FileBytes  int64             `json:"fileBytes"`
	FileSHA256 string            `json:"fileSha256"`
	Stamp      string            `json:"stamp"`
	Counts     fixtureCounts     `json:"counts"`
	Worlds     []worldHeader     `json:"worlds"`
	Pyramids   []pyramidHeader   `json:"pyramids"`
	PartHashes map[string]string `json:"partHashes"`
	EntryOrder struct {
		First  []string `json:"first"`
		SHA256 string   `json:"sha256"`
	} `json:"entryOrder"`
}

type worldHeader struct {
	Slug        string   `json:"slug"`
	Points      int      `json:"points"`
	Paths       int      `json:"paths"`
	Areas       int      `json:"areas"`
	Collections int      `json:"collections"`
	Lenses      []string `json:"lenses"`
	TextEntries int      `json:"textEntries"`
}

type pyramidHeader struct {
	Pyramid   string `json:"pyramid"`
	Tiles     int    `json:"tiles"`
	Bytes     int64  `json:"bytes"`
	Content   string `json:"contentRollup"`
	Pixels    string `json:"pixelRollup"`
	Inventory string `json:"inventory"`
}

// locationRow is one packed location as the extraction spells it. The field
// order is the encoding: canon writes a struct in declaration order.
type locationRow struct {
	ID     int64   `json:"id"`
	Owner  uint16  `json:"owner"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Member int64   `json:"member"`
	Shard  int64   `json:"shard"`
	Title  string  `json:"title"`
}

type iconRow struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// tileRow is one tile of an inventory. Width, Height and Pixels are read but
// never recomputed here: the format gate proves the container hands back the
// bytes it was given, and two runs that agree on a tile's SHA-256 cannot
// disagree about the picture inside it. The pixel digest earns its keep
// against a pipeline that re-encodes a tile, which is the generate-enrich
// gate's subject (issue #5 §6).
type tileRow struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	CRC32  string `json:"crc32"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"w"`
	Height int    `json:"h"`
	Pixels string `json:"px"`
}

func readFixtureSet(t *testing.T) fixtureSet {
	t.Helper()
	var set fixtureSet
	readJSON(t, filepath.Join(fixturesDir, "FIXTURES.json"), &set)
	if len(set.Volumes) == 0 {
		t.Fatal("FIXTURES.json names no volumes")
	}
	return set
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	if err := json.Unmarshal(readFile(t, path), into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func readManifest(t *testing.T, v fixtureVolume) (bundle.Manifest, []byte) {
	t.Helper()
	raw := readFile(t, filepath.Join(v.dir(), "manifest.json"))
	var manifest bundle.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("%s manifest: %v", v.Slug, err)
	}
	return manifest, raw
}

func readVolumeHeader(t *testing.T, v fixtureVolume) volumeHeader {
	t.Helper()
	var header volumeHeader
	readJSON(t, filepath.Join(v.dir(), "volume.json"), &header)
	return header
}

// rowsOf reads the row array of an inventory-shaped extraction, leaving the
// header alone: the header is recomputed rather than read, so that comparing a
// re-emission to the committed file checks the header's figures too.
func rowsOf(t *testing.T, path, key string, into any) {
	t.Helper()
	var document map[string]json.RawMessage
	readJSON(t, path, &document)
	body, held := document[key]
	if !held {
		t.Fatalf("%s carries no %q array", path, key)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("%s %s: %v", path, key, err)
	}
}

// emitLocations writes the locations extraction of one world from the
// locations themselves: it packs them, hashes the packing, and lays the whole
// document out the way golden/capture/bundles did. Everything the header
// claims -- the packed payload's size and digest, the count -- is recomputed,
// so a document that matches the committed one matches on all of it.
func emitLocations(world string, rows []locationRow) ([]byte, []byte, error) {
	locations := make([]bundle.Location, len(rows))
	for index, row := range rows {
		locations[index] = bundle.Location{
			ID:     row.ID,
			Title:  row.Title,
			Lat:    row.Lat,
			Lng:    row.Lng,
			Member: row.Member,
			Shard:  row.Shard,
			Owner:  row.Owner,
		}
	}
	packed := bundle.PackLocations(locations)
	header := map[string]any{
		"world":        world,
		"packed":       packedForm,
		"packedSha256": bundle.HashBytes(packed),
		"packedBytes":  len(packed),
		"count":        len(rows),
		"note":         locationsNote,
	}
	document, err := canon.Rows(header, "locations", anyRows(rows))
	if err != nil {
		return nil, nil, fmt.Errorf("world %s: %w", world, err)
	}
	return packed, document, nil
}

// emitIcons writes an icons extraction from the rows, rollup included.
func emitIcons(volume string, rows []iconRow) ([]byte, error) {
	digest := sha256.New()
	for _, row := range rows {
		digest.Write([]byte(row.Name + " " + row.SHA256 + "\n"))
	}
	header := map[string]any{
		"volume": volume,
		"count":  len(rows),
		"rollup": hex.EncodeToString(digest.Sum(nil)),
	}
	return canon.Rows(header, "icons", anyRows(rows))
}

func anyRows[T any](rows []T) []any {
	out := make([]any, len(rows))
	for index, row := range rows {
		out[index] = row
	}
	return out
}

// rollup is the digest an inventory summarizes itself with: one "name hash"
// line per row, in the inventory's own order.
func rollup(lines []string) string {
	digest := sha256.New()
	for _, line := range lines {
		digest.Write([]byte(line + "\n"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// diffBytes reports where two documents first differ, in a form a reader can
// act on. A fixture is often megabytes; printing both whole helps nobody.
func diffBytes(got, want []byte) string {
	if len(got) == len(want) {
		for at := range got {
			if got[at] != want[at] {
				return fmt.Sprintf("byte %d of %d differs:\n got %s\nwant %s",
					at, len(want), excerpt(got, at), excerpt(want, at))
			}
		}
		return "identical"
	}
	limit := min(len(got), len(want))
	for at := range limit {
		if got[at] != want[at] {
			return fmt.Sprintf("byte %d differs (%d bytes against %d):\n got %s\nwant %s",
				at, len(got), len(want), excerpt(got, at), excerpt(want, at))
		}
	}
	return fmt.Sprintf("one is a prefix of the other: %d bytes against %d, first difference at %d",
		len(got), len(want), limit)
}

func excerpt(data []byte, at int) string {
	start := max(at-60, 0)
	end := min(at+60, len(data))
	return strings.ReplaceAll(string(data[start:end]), "\n", "\\n")
}

// The registry half of the gate.

// registryDir reports the library to check against, or skips. A directory that
// holds none of the fixture builds is not the library the fixtures came from,
// and skipping says so; a directory holding some of them is that library with
// pieces missing, which is a failure rather than a skip.
func registryDir(t *testing.T) (string, fixtureSet) {
	t.Helper()
	dir := os.Getenv(registryDirEnv)
	if dir == "" {
		t.Skipf("set %s to a bundles directory to check the fixtures against their real .atlas files", registryDirEnv)
	}
	set := readFixtureSet(t)
	var held int
	for _, volume := range set.Volumes {
		if _, err := os.Stat(filepath.Join(sourceDir(dir, volume), volume.File)); err == nil {
			held++
		}
	}
	if held == 0 {
		t.Skipf("%s holds none of the %d fixture builds, so it is not the library they were captured from",
			dir, len(set.Volumes))
	}
	return dir, set
}

// sourceDir is where one fixture build is read from. Nearly always the
// library; for a volume built for the fixture rather than found installed, the
// directory it was built into. This is capture.sh's own rule, and the reason
// the gate does not need to know the city's name.
func sourceDir(dir string, v fixtureVolume) string {
	if len(v.BuiltFor) == 0 {
		return dir
	}
	if city := os.Getenv(cityDirEnv); city != "" {
		return city
	}
	return cityDirDefault
}

// openFixture opens one fixture build out of the registry.
func openFixture(t *testing.T, dir string, v fixtureVolume) *bundle.Reader {
	t.Helper()
	from := sourceDir(dir, v)
	path := filepath.Join(from, v.File)
	if _, err := os.Stat(path); err != nil {
		if len(v.BuiltFor) > 0 {
			t.Skipf("%s: %s was built for the fixture and is not in %s; build it (golden/fixtures/README.md) or point %s at it",
				v.Slug, v.File, from, cityDirEnv)
			return nil
		}
		t.Errorf("%s: %s is missing from the library the other fixtures are in", v.Slug, v.File)
		return nil
	}
	reader, err := bundle.Open(path)
	if err != nil {
		t.Errorf("%s: %v", v.Slug, err)
		return nil
	}
	t.Cleanup(func() { reader.Close() })
	return reader
}

// pyramidOf is the tiles/<pyramid>/ prefix an inventory's rows hang under.
func pyramidOf(name string) string { return bundle.TilesPrefix + name + "/" }

// sortedKeys is a stable listing, for error messages that name what is missing
// rather than how many things are.
func sortedKeys[V any](held map[string]V) []string {
	out := make([]string, 0, len(held))
	for name := range held {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
