package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// The corpus: committed extractions of two real builds -- the proof city and
// the planet -- taken from .atlas files the reference implementation actually
// wrote. Every byte compared here was produced by the implementation this one
// replaces, which is what holds the reader and writer to real bytes without a
// library anywhere in sight. A corpus file that is missing is a failure, never
// a skip: the corpus is committed, so its absence means the checkout is
// broken, not that there is nothing to judge.
//
// What the corpus cannot carry is the archives themselves -- rasters and all,
// they are hundreds of megabytes -- so the checks that needed a real .atlas in
// hand (part hashes against the wire, entry order, tiles stored uncompressed)
// live in a separate non-gating command that points at an installed library.

const corpusRoot = "../../testdata/corpus/bundles"

// corpusSlugs are the two volumes the corpus holds, by name rather than by
// listing, so a corpus directory that lost one fails loudly.
var corpusSlugs = []string{"bend-or", "mars"}

func corpusDir(slug string) string {
	return filepath.Join(corpusRoot, slug)
}

// corpusHeader is the part of a volume.json extraction these tests read: the
// identity the build carried, the counts, and the pyramids the tile
// inventories are inventories of.
type corpusHeader struct {
	Slug          string            `json:"slug"`
	Title         string            `json:"title"`
	File          string            `json:"file"`
	FileBytes     int64             `json:"fileBytes"`
	FormatVersion int               `json:"formatVersion"`
	Conventions   int               `json:"conventions"`
	Stamp         string            `json:"stamp"`
	Stamp12       string            `json:"stamp12"`
	CreatedAt     string            `json:"createdAt"`
	Revision      int               `json:"revision"`
	Counts        corpusCounts      `json:"counts"`
	Worlds        []corpusWorld     `json:"worlds"`
	Pyramids      []corpusPyramid   `json:"pyramids"`
	PartHashes    map[string]string `json:"partHashes"`
}

type corpusCounts struct {
	Entries int `json:"entries"`
	Worlds  int `json:"worlds"`
	Tiles   int `json:"tiles"`
	Icons   int `json:"icons"`
	Points  int `json:"points"`
	Paths   int `json:"paths"`
	Areas   int `json:"areas"`
}

type corpusWorld struct {
	Slug        string `json:"slug"`
	Points      int    `json:"points"`
	Paths       int    `json:"paths"`
	Areas       int    `json:"areas"`
	Collections int    `json:"collections"`
	TextEntries int    `json:"textEntries"`
}

type corpusPyramid struct {
	Pyramid   string `json:"pyramid"`
	Tiles     int    `json:"tiles"`
	Bytes     int64  `json:"bytes"`
	Content   string `json:"contentRollup"`
	Pixels    string `json:"pixelRollup"`
	Inventory string `json:"inventory"`
}

// locationsHeader is what a locations extraction says about the payload it
// was unpacked from: the real archive's packed bytes, by size and by digest.
type locationsHeader struct {
	World        string `json:"world"`
	Packed       string `json:"packed"`
	PackedSHA256 string `json:"packedSha256"`
	PackedBytes  int    `json:"packedBytes"`
	Count        int    `json:"count"`
}

// locationRow is one packed location as the extraction spells it.
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

type tileRow struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func readCorpusFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the corpus is incomplete: %v", err)
	}
	return data
}

func readCorpusJSON(t *testing.T, path string, into any) {
	t.Helper()
	if err := json.Unmarshal(readCorpusFile(t, path), into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func readCorpusManifest(t *testing.T, slug string) (bundle.Manifest, []byte) {
	t.Helper()
	committed := readCorpusFile(t, filepath.Join(corpusDir(slug), "manifest.json"))
	var manifest bundle.Manifest
	if err := json.Unmarshal(committed, &manifest); err != nil {
		t.Fatalf("%s manifest: %v", slug, err)
	}
	return manifest, committed
}

func readCorpusHeader(t *testing.T, slug string) corpusHeader {
	t.Helper()
	var header corpusHeader
	readCorpusJSON(t, filepath.Join(corpusDir(slug), "volume.json"), &header)
	return header
}

// corpusRows reads the row array of an inventory-shaped extraction.
func corpusRows(t *testing.T, path, key string, into any) {
	t.Helper()
	var document map[string]json.RawMessage
	readCorpusJSON(t, path, &document)
	body, held := document[key]
	if !held {
		t.Fatalf("%s carries no %q array", path, key)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("%s %s: %v", path, key, err)
	}
}

// canonicalJSON writes raw JSON in the one shape the corpus is committed in:
// keys sorted, numbers carried as the source wrote them, no HTML escaping,
// two-space indentation, one trailing newline. It mirrors the canon the
// capture used, so a re-encoding that matches a corpus file matches the
// extraction and not merely something equivalent to it.
func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
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

// firstDiff reports where two documents first differ, in a form a reader can
// act on; a corpus manifest is small but a payload is not.
func firstDiff(got, want []byte) string {
	limit := min(len(got), len(want))
	for at := range limit {
		if got[at] != want[at] {
			return fmt.Sprintf("byte %d differs (%d bytes against %d):\n got %s\nwant %s",
				at, len(got), len(want), excerpt(got, at), excerpt(want, at))
		}
	}
	if len(got) != len(want) {
		return fmt.Sprintf("one is a prefix of the other: %d bytes against %d, first difference at %d",
			len(got), len(want), limit)
	}
	return "identical"
}

func excerpt(data []byte, at int) string {
	start := max(at-60, 0)
	end := min(at+60, len(data))
	return strings.ReplaceAll(string(data[start:end]), "\n", "\\n")
}

func locationsOf(rows []locationRow) []bundle.Location {
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
	return locations
}

// A corpus manifest must survive the trip through this package's types: every
// key read, every key written again, nothing dropped, nothing reordered, no
// number reformatted. The manifest's encoded bytes feed the stamp of every
// bundle ever built, so a schema that has quietly moved is the single most
// expensive thing that could be wrong in this package, and this is the check
// that catches it without an archive in hand.
func TestCorpusManifestsReEncodeExactly(t *testing.T) {
	for _, slug := range corpusSlugs {
		t.Run(slug, func(t *testing.T) {
			manifest, committed := readCorpusManifest(t, slug)
			if err := manifest.Validate(); err != nil {
				t.Fatalf("the corpus manifest does not validate: %v", err)
			}
			encoded, err := bundle.MarshalManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			again, err := canonicalJSON(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, committed) {
				t.Errorf("re-encoding the manifest does not reproduce the extraction:\n%s",
					firstDiff(again, committed))
			}
		})
	}
}

// The file a build is written under is derived from its manifest alone, and
// volume.json records the name the real build actually carried. This is the
// stamp's short form, the capture-day rule and the naming order checked end
// to end, with no bundle in hand.
func TestCorpusManifestsDeriveTheirIdentity(t *testing.T) {
	for _, slug := range corpusSlugs {
		t.Run(slug, func(t *testing.T) {
			manifest, _ := readCorpusManifest(t, slug)
			header := readCorpusHeader(t, slug)

			if derived := bundle.VersionedFileName(manifest); derived != header.File {
				t.Errorf("this build would be named %s, and it is named %s", derived, header.File)
			}
			if short := bundle.ShortStamp(manifest.Version.Stamp); short != header.Stamp12 {
				t.Errorf("short stamp is %s, and volume.json says %s", short, header.Stamp12)
			}
			if day := bundle.CaptureDay(manifest.Version.CreatedAt); !strings.Contains(header.File, day) {
				t.Errorf("capture day %q does not appear in %s", day, header.File)
			}

			for _, field := range []struct {
				name       string
				got, wants any
			}{
				{"slug", manifest.Volume.Slug, header.Slug},
				{"title", manifest.Volume.Title, header.Title},
				{"stamp", manifest.Version.Stamp, header.Stamp},
				{"createdAt", manifest.Version.CreatedAt, header.CreatedAt},
				{"revision", manifest.Version.Revision, header.Revision},
				{"formatVersion", manifest.FormatVersion, header.FormatVersion},
				{"conventions", manifest.Conventions, header.Conventions},
				{"worlds", len(manifest.Worlds), header.Counts.Worlds},
			} {
				if field.got != field.wants {
					t.Errorf("manifest %s is %v, and volume.json says %v", field.name, field.got, field.wants)
				}
			}

			var points, paths, areas int
			for _, world := range manifest.Worlds {
				points += world.Points
				paths += world.Paths
				areas += world.Areas
			}
			if points != header.Counts.Points || paths != header.Counts.Paths || areas != header.Counts.Areas {
				t.Errorf("the manifest promises %d points, %d paths and %d areas; volume.json counts %d, %d and %d",
					points, paths, areas, header.Counts.Points, header.Counts.Paths, header.Counts.Areas)
			}
		})
	}
}

// Every world's unpacked locations are packed again by this package, and the
// result must be the real payload: the extraction records the packed bytes
// the archive carried by size and by SHA-256, so a packing that matches
// reproduces bytes written by another implementation. And back again, so the
// codec is checked in both directions rather than only in the one the capture
// ran.
func TestCorpusLocationsRepackToTheRecordedPayload(t *testing.T) {
	for _, slug := range corpusSlugs {
		t.Run(slug, func(t *testing.T) {
			manifest, _ := readCorpusManifest(t, slug)
			header := readCorpusHeader(t, slug)
			for _, entry := range manifest.Worlds {
				path := filepath.Join(corpusDir(slug), "worlds", entry.Slug+".locations.json")
				var recorded locationsHeader
				readCorpusJSON(t, path, &recorded)
				var rows []locationRow
				corpusRows(t, path, "locations", &rows)

				if want := fmt.Sprintf("%s v%d", bundle.LocationMagic, bundle.LocationVersion); recorded.Packed != want {
					t.Errorf("world %s: the extraction is packed as %q and this package writes %q",
						entry.Slug, recorded.Packed, want)
				}
				if len(rows) != entry.Points || len(rows) != recorded.Count {
					t.Errorf("world %s: the extraction holds %d locations, its header counts %d, and the manifest promises %d points",
						entry.Slug, len(rows), recorded.Count, entry.Points)
				}

				packed := bundle.PackLocations(locationsOf(rows))
				if len(packed) != recorded.PackedBytes {
					t.Errorf("world %s: repacking weighs %d bytes and the extraction recorded %d",
						entry.Slug, len(packed), recorded.PackedBytes)
				}
				if hash := bundle.HashBytes(packed); hash != recorded.PackedSHA256 {
					t.Errorf("world %s: repacking hashes to %s and the extraction recorded %s",
						entry.Slug, hash, recorded.PackedSHA256)
				}
				// The packed payload is a stamped part, and volume.json carries
				// the part hash the capture took off the archive itself. The
				// three records must agree, or the corpus is not an oracle.
				part := bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix)
				if header.PartHashes[part] != recorded.PackedSHA256 {
					t.Errorf("world %s: volume.json hashes %s as %s and the extraction says %s",
						entry.Slug, part, header.PartHashes[part], recorded.PackedSHA256)
				}

				back, err := bundle.UnpackLocations(packed)
				if err != nil {
					t.Errorf("world %s: %v", entry.Slug, err)
					continue
				}
				if !reflect.DeepEqual(back, locationsOf(rows)) {
					t.Errorf("world %s: unpacking the repacked payload does not reproduce the extraction's rows",
						entry.Slug)
				}
				if again := bundle.PackLocations(back); !bytes.Equal(again, packed) {
					t.Errorf("world %s: packing what was unpacked writes %d bytes from %d",
						entry.Slug, len(again), len(packed))
				}
			}
		})
	}
}

// The extractions are assembled back into an archive -- the corpus manifest,
// the corpus payloads, the locations packed from the corpus rows, an entry
// per icon and one tile per level of every pyramid the inventories name --
// and the result is opened and validated.
//
// This is the one corpus check that reaches the parts of this package a
// manifest alone cannot: the writer, the reader, the offline scan, the
// per-kind counts against the payload, the geometry rules, and every
// conventions rule in format/semconv, run against real captured payloads. The
// icon and tile bodies are stand-ins, because nothing validation asks about
// them concerns their bytes; their names are the real build's own.
func TestCorpusExtractionsReassembleIntoAValidBundle(t *testing.T) {
	for _, slug := range corpusSlugs {
		t.Run(slug, func(t *testing.T) {
			header := readCorpusHeader(t, slug)
			path := reassembleCorpus(t, slug, header)
			reader, err := bundle.Open(path)
			if err != nil {
				t.Fatalf("the reassembled bundle does not open: %v", err)
			}
			defer reader.Close()

			if err := reader.Validate(); err != nil {
				t.Errorf("the reassembled bundle does not validate: %v", err)
			}
			if name := bundle.VersionedFileName(reader.Manifest); name != header.File {
				t.Errorf("reopened, this build would be named %s rather than %s", name, header.File)
			}

			// The manifest went through the writer and came back through the
			// reader; it must still canonicalize to the extraction.
			encoded, err := bundle.MarshalManifest(reader.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			again, err := canonicalJSON(encoded)
			if err != nil {
				t.Fatal(err)
			}
			committed := readCorpusFile(t, filepath.Join(corpusDir(slug), "manifest.json"))
			if !bytes.Equal(again, committed) {
				t.Errorf("the manifest read back out of the archive does not match the extraction:\n%s",
					firstDiff(again, committed))
			}
		})
	}
}

// reassembleCorpus writes an archive out of one volume's committed
// extractions and returns its path.
func reassembleCorpus(t *testing.T, slug string, header corpusHeader) string {
	t.Helper()
	manifest, _ := readCorpusManifest(t, slug)

	path := filepath.Join(t.TempDir(), slug+bundle.Extension)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatalf("the corpus manifest is not one a writer accepts: %v", err)
	}

	for _, entry := range manifest.Worlds {
		worlds := filepath.Join(corpusDir(slug), "worlds")
		payload := readCorpusFile(t, filepath.Join(worlds, entry.Slug+".payload.json"))
		text := readCorpusFile(t, filepath.Join(worlds, entry.Slug+".text.json"))

		var rows []locationRow
		corpusRows(t, filepath.Join(worlds, entry.Slug+".locations.json"), "locations", &rows)
		packed := bundle.PackLocations(locationsOf(rows))

		must(t, writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix), payload))
		must(t, writer.AddStored(bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix), bytes.NewReader(packed)))
		must(t, writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.TextSuffix), text))
	}

	var icons []iconRow
	corpusRows(t, filepath.Join(corpusDir(slug), "icons.json"), "icons", &icons)
	for _, icon := range icons {
		must(t, writer.AddDeflated(icon.Name, []byte("<svg/>")))
	}

	// One tile per level of every pyramid. Validation asks whether a level
	// holds anything, never what; a whole pyramid would be thousands of
	// entries to answer a yes-or-no question.
	written := map[string]bool{}
	for _, pyramid := range header.Pyramids {
		var tiles []tileRow
		corpusRows(t, filepath.Join(corpusDir(slug), filepath.FromSlash(pyramid.Inventory)), "tiles", &tiles)
		levels := map[string]bool{}
		for _, tile := range tiles {
			level, _, held := strings.Cut(tile.Name, "/")
			if !held || levels[level] {
				continue
			}
			levels[level] = true
			name := bundle.TilesPrefix + pyramid.Pyramid + "/" + tile.Name
			if written[name] {
				continue
			}
			written[name] = true
			must(t, writer.AddStored(name, strings.NewReader("raster")))
		}
	}

	must(t, writer.Close())
	must(t, file.Close())
	return path
}

// The corpus manifests, folded as a library: each build serves its own
// volume, the derived index lists them all, and the index still speaks the
// legacy wire keys. The last one is not cosmetic -- an index that renamed
// "games" to "volumes" would be read as empty by everything that consumes it.
func TestCorpusManifestsFoldAsALibrary(t *testing.T) {
	headers := make(map[string]corpusHeader, len(corpusSlugs))
	descriptors := make([]bundle.Descriptor, 0, len(corpusSlugs))
	for _, slug := range corpusSlugs {
		manifest, _ := readCorpusManifest(t, slug)
		header := readCorpusHeader(t, slug)
		headers[slug] = header
		descriptors = append(descriptors, bundle.DescriptorOf(header.File, manifest, header.FileBytes))
	}

	winners := bundle.Fold(descriptors)
	if len(winners) != len(corpusSlugs) {
		t.Errorf("%d corpus builds folded to %d volumes", len(corpusSlugs), len(winners))
	}
	for _, slug := range corpusSlugs {
		winner, held := winners[slug]
		if !held {
			t.Errorf("%s did not survive the fold", slug)
			continue
		}
		if winner.Stamp != headers[slug].Stamp {
			t.Errorf("%s is served by %s, not by the corpus build", slug, winner.Stamp)
		}
	}
	if shadowed := bundle.Shadowed(descriptors); len(shadowed) != 0 {
		t.Errorf("%d corpus builds were shadowed, and no two are builds of one volume", len(shadowed))
	}

	index, err := bundle.MarshalIndex(bundle.BuildIndex(descriptors))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"games":`, `"maps":`} {
		if !bytes.Contains(index, []byte(key)) {
			t.Errorf("the derived index carries no %s; the listing's wire keys predate the volume vocabulary and were never renamed", key)
		}
	}
	var listing struct {
		Volumes []struct {
			Slug     string `json:"slug"`
			Versions []struct {
				File   string `json:"file"`
				Stamp  string `json:"stamp"`
				Worlds int    `json:"maps"`
			} `json:"versions"`
		} `json:"games"`
	}
	if err := json.Unmarshal(index, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Volumes) != len(corpusSlugs) {
		t.Errorf("the index lists %d volumes of %d", len(listing.Volumes), len(corpusSlugs))
	}
	for _, listed := range listing.Volumes {
		header := headers[listed.Slug]
		if len(listed.Versions) != 1 {
			t.Errorf("%s is listed with %d builds", listed.Slug, len(listed.Versions))
			continue
		}
		build := listed.Versions[0]
		if build.File != header.File || build.Stamp != header.Stamp || build.Worlds != header.Counts.Worlds {
			t.Errorf("%s is listed as %s/%s with %d worlds", listed.Slug, build.File, build.Stamp, build.Worlds)
		}
	}
}

// The extractions are read together and held to each other: the icon and tile
// inventories must sum to the rollups they carry, every header must count
// what its rows hold, and volume.json must agree with the manifest world for
// world. A corpus that disagrees with itself is not an oracle, and this is
// the check that says it still is one.
func TestCorpusInventoriesAgreeWithTheirHeaders(t *testing.T) {
	for _, slug := range corpusSlugs {
		t.Run(slug, func(t *testing.T) {
			header := readCorpusHeader(t, slug)

			iconsPath := filepath.Join(corpusDir(slug), "icons.json")
			var iconsHeader struct {
				Volume string `json:"volume"`
				Count  int    `json:"count"`
				Rollup string `json:"rollup"`
			}
			readCorpusJSON(t, iconsPath, &iconsHeader)
			var icons []iconRow
			corpusRows(t, iconsPath, "icons", &icons)
			if iconsHeader.Volume != slug || iconsHeader.Count != len(icons) || len(icons) != header.Counts.Icons {
				t.Errorf("the icon inventory holds %d rows, counts %d, and volume.json counts %d",
					len(icons), iconsHeader.Count, header.Counts.Icons)
			}
			lines := make([]string, 0, len(icons))
			for _, icon := range icons {
				lines = append(lines, icon.Name+" "+icon.SHA256)
			}
			if got := rollup(lines); got != iconsHeader.Rollup {
				t.Errorf("the icon inventory sums to %s and its header says %s", got, iconsHeader.Rollup)
			}

			var tiles int
			for _, pyramid := range header.Pyramids {
				path := filepath.Join(corpusDir(slug), filepath.FromSlash(pyramid.Inventory))
				var inventory struct {
					Pyramid string `json:"pyramid"`
					Count   int    `json:"count"`
					Bytes   int64  `json:"bytes"`
					Content string `json:"contentRollup"`
					Pixels  string `json:"pixelRollup"`
				}
				readCorpusJSON(t, path, &inventory)
				if inventory.Pyramid != pyramid.Pyramid || inventory.Count != pyramid.Tiles ||
					inventory.Bytes != pyramid.Bytes || inventory.Content != pyramid.Content ||
					inventory.Pixels != pyramid.Pixels {
					t.Errorf("pyramid %s describes itself as %d tiles weighing %d, and volume.json says %d weighing %d",
						pyramid.Pyramid, inventory.Count, inventory.Bytes, pyramid.Tiles, pyramid.Bytes)
				}

				var rows []struct {
					Name   string `json:"name"`
					Bytes  int64  `json:"bytes"`
					SHA256 string `json:"sha256"`
					Pixels string `json:"px"`
				}
				corpusRows(t, path, "tiles", &rows)
				tiles += len(rows)

				var weight int64
				content := make([]string, 0, len(rows))
				pixels := make([]string, 0, len(rows))
				for _, tile := range rows {
					weight += tile.Bytes
					content = append(content, tile.Name+" "+tile.SHA256)
					pixels = append(pixels, tile.Name+" "+tile.Pixels)
				}
				if len(rows) != pyramid.Tiles || weight != pyramid.Bytes {
					t.Errorf("pyramid %s inventories %d tiles weighing %d, and its header says %d weighing %d",
						pyramid.Pyramid, len(rows), weight, pyramid.Tiles, pyramid.Bytes)
				}
				if got := rollup(content); got != pyramid.Content {
					t.Errorf("pyramid %s sums to %s and its header says %s", pyramid.Pyramid, got, pyramid.Content)
				}
				if got := rollup(pixels); got != pyramid.Pixels {
					t.Errorf("pyramid %s pixel-sums to %s and its header says %s", pyramid.Pyramid, got, pyramid.Pixels)
				}
			}
			if tiles != header.Counts.Tiles {
				t.Errorf("the tile inventories hold %d rows and volume.json counts %d", tiles, header.Counts.Tiles)
			}

			manifest, _ := readCorpusManifest(t, slug)
			if len(header.Worlds) != len(manifest.Worlds) {
				t.Fatalf("volume.json describes %d worlds and the manifest lists %d",
					len(header.Worlds), len(manifest.Worlds))
			}
			for index, world := range header.Worlds {
				entry := manifest.Worlds[index]
				if world.Slug != entry.Slug || world.Points != entry.Points ||
					world.Paths != entry.Paths || world.Areas != entry.Areas {
					t.Errorf("volume.json describes world %s as %d/%d/%d and the manifest says %s as %d/%d/%d",
						world.Slug, world.Points, world.Paths, world.Areas,
						entry.Slug, entry.Points, entry.Paths, entry.Areas)
					continue
				}
				var payload struct {
					Collections []json.RawMessage `json:"collections"`
				}
				readCorpusJSON(t, filepath.Join(corpusDir(slug), "worlds", world.Slug+".payload.json"), &payload)
				if len(payload.Collections) != world.Collections {
					t.Errorf("world %s inlines %d collections and volume.json counts %d",
						world.Slug, len(payload.Collections), world.Collections)
				}
				var text map[string]json.RawMessage
				readCorpusJSON(t, filepath.Join(corpusDir(slug), "worlds", world.Slug+".text.json"), &text)
				if len(text) != world.TextEntries {
					t.Errorf("world %s carries %d text entries and volume.json counts %d",
						world.Slug, len(text), world.TextEntries)
				}
			}
		})
	}
}
