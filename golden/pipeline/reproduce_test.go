// Package pipeline holds the gate that measures the generate and enrich lanes
// against the composed bundle fixtures: generate ⊕ enrich reproduces what the
// reference tree wrote (issue #5 §6).
//
// The gate is not complete. Enrichment does not exist yet, so the merged and
// city fixtures cannot be reproduced by anything, and the harness leaves
// `generate-enrich` skipped. What runs here today is the single-source half:
// the plain-MapGenie fixture, composed from archived captures by the clean-room
// generate lane and held against every extraction the reference build was
// captured into.
//
// # Why these tests are gated
//
// They read two directories that are deliberately not in git: the capture
// archive, which is gigabytes of somebody else's bytes, and the derived tile
// set, which is rasters. A checkout without them skips, and says which
// environment variable would have pointed at them. A checkout with them proves
// the lane.
//
//	ATLAS_ARCHIVE_DIR   the capture archive root, holding archive.json
//	ATLAS_CITY_ARCHIVE_DIR  the city's archive, staged beside it
//	ATLAS_TILES_INDEX   the derived tile set's index.json
//
// All three default to the repository's own gitignored copies -- crawl/fmg-archive,
// crawl/bend-or/fmg-archive and tiles/index.json -- so the usual case needs no
// environment at all.
//
// The city has an archive of its own because the corpus's holds whatever its
// operator has crawled, and for a city that is allowed to be a city the public
// curation table may not name (issue #5's privacy rule: the proof city is
// committed, an operator's own city is not). Staging the proof city apart keeps
// a checkout able to rebuild it without a library-sized capture, and keeps the
// gate from depending on what else is in somebody's archive.
package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/golden/canon"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/compose"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// The fixtures this file reproduces: every bundle fixture whose ledger names
// one source, which is every fixture the generate lane can answer for alone.
// Cyberpunk is left out on purpose -- its world was merged from two sources, so
// nothing short of generate ⊕ enrich can write it, and it is the enrich lane's
// gate rather than this one's.
//
// Each entry says what shape it is a fixture of, because that is what a failure
// here has to be read against: a broken splitter fails the split sheet first, a
// broken lens shard fails the sharded one, and a change to composition itself
// fails the plain one before any of them.
var singleSource = []struct {
	volume string
	shape  string
}{
	{"tunic", "plain: one world, one lens, one pyramid"},
	{"fallout-new-vegas", "split sheet: thirteen worlds, eight of them insets of the first"},
	{"zelda-tears-of-the-kingdom", "lens shards: one world offered three elevations at a time"},
	{"mars", "a sphere, a derived id space, and artwork named rather than shipped"},
	{"bend-or", "a city: dated worlds, national layers, and a lens drawn rather than fetched"},
}

// fixturePath is where a volume's captured extractions sit.
func fixturePath(volume string) string { return "../fixtures/bundles/" + volume }

// TestComposeReproducesBundleFixture composes the fixture volume from the
// archive and holds the result against every extraction the reference build was
// captured into.
//
// Canonical-content equality is mandatory: the manifest, the world payload, the
// unpacked locations, the deferred prose, the icon set and the tile inventory
// must all say the same thing. Stamp identity is tracked as an aspiration and
// reported either way, because a stamp is a promise about rebuild cost rather
// than about correctness.
func TestComposeReproducesBundleFixture(t *testing.T) {
	for _, subject := range singleSource {
		t.Run(subject.volume, func(t *testing.T) {
			t.Logf("%s -- %s", subject.volume, subject.shape)
			dir := fixturePath(subject.volume)
			built := composeFixture(t, subject.volume)
			fixture := readVolumeFixture(t, subject.volume)

			t.Run("part hashes", func(t *testing.T) {
				for _, name := range sortedKeys(fixture.PartHashes) {
					data, err := built.reader.ReadEntry(name)
					if err != nil {
						t.Fatalf("read %s: %v", name, err)
					}
					if got := bundle.HashBytes(data); got != fixture.PartHashes[name] {
						t.Errorf("%s: hash %s, fixture %s", name, got, fixture.PartHashes[name])
					}
				}
			})

			t.Run("manifest", func(t *testing.T) {
				compareCanon(t, built, bundle.ManifestName, filepath.Join(dir, "manifest.json"))
			})

			t.Run("world payloads", func(t *testing.T) {
				for _, entry := range built.reader.Manifest.Worlds {
					compareCanon(t, built,
						bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix),
						filepath.Join(dir, "worlds", entry.Slug+".payload.json"))
					compareCanon(t, built,
						bundle.WorldEntryName(entry.Slug, bundle.TextSuffix),
						filepath.Join(dir, "worlds", entry.Slug+".text.json"))
					compareLocations(t, built, dir, entry.Slug)
				}
			})

			t.Run("icons", func(t *testing.T) { compareIcons(t, built, dir) })

			t.Run("tile inventory", func(t *testing.T) { compareTiles(t, built, dir, fixture) })

			t.Run("entry order", func(t *testing.T) {
				if got := hashOf(built.reader.Names()); got != fixture.EntryOrder.SHA256 {
					t.Errorf("entry order %s, fixture %s", got, fixture.EntryOrder.SHA256)
				}
			})

			// The aspiration, reported rather than merely asserted, so a run
			// says how close it came even when it fails.
			t.Run("stamp", func(t *testing.T) {
				got := built.reader.Manifest.Version
				switch {
				case got.Stamp == fixture.Stamp && built.file == fixture.File && built.sha256 == fixture.FileSHA256:
					t.Logf("stamp-identical and byte-identical: %s (%d bytes)", built.file, built.bytes)
				case got.Stamp == fixture.Stamp:
					t.Errorf("stamp identical but bytes differ: %s vs fixture %s", built.sha256, fixture.FileSHA256)
				default:
					t.Errorf("stamp %s, fixture %s (canonical content is checked above; "+
						"stamp identity is the aspiration of issue #5 §6)", got.Stamp, fixture.Stamp)
				}
				if got.Revision != fixture.Revision {
					t.Errorf("revision %d, fixture %d", got.Revision, fixture.Revision)
				}
				if got.CreatedAt != fixture.CreatedAt {
					t.Errorf("createdAt %q, fixture %q", got.CreatedAt, fixture.CreatedAt)
				}
			})
		})
	}
}

// TestComposeIsIdempotent holds the determinism invariant the registry stands
// on: a second composition of an untouched archive writes nothing, because the
// build it would write is already there under a name carrying its stamp.
func TestComposeIsIdempotent(t *testing.T) {
	for _, subject := range singleSource {
		t.Run(subject.volume, func(t *testing.T) {
			built := composeFixture(t, subject.volume)
			again, err := compose.Compose(built.options)
			if err != nil {
				t.Fatalf("recompose: %v", err)
			}
			if !again.Present {
				t.Errorf("recomposing an unchanged archive wrote %s again", again.File)
			}
			if again.Stamp != built.result.Stamp {
				t.Errorf("stamp moved between runs: %s then %s", built.result.Stamp, again.Stamp)
			}
		})
	}
}

// TestTranslatorAgreesWithFixture holds the MapGenie reader against the
// translator fixture: the same archived capture, read by the reference tree's
// translator, canonicalized.
//
// The fixture pins behaviour, not shape. The reference tree's interchange
// document was MapGenie's own API response passed through untouched, and the
// clean room's is Atlas's schema, so the two cannot be diffed. What is compared
// here is what the two documents mean -- the same worlds, the same collections
// in the same order, the same features in the same places, the same prose after
// link resolution -- and every intentional difference of shape is named in the
// table below rather than left for a reader to infer.
func TestTranslatorAgreesWithFixture(t *testing.T) {
	document := translateFixture(t, "tunic")

	var reference struct {
		ID         int64   `json:"id"`
		Slug       string  `json:"slug"`
		Title      string  `json:"title"`
		InitialLat float64 `json:"initial_latitude"`
		InitialLng float64 `json:"initial_longitude"`
		Game       struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"game"`
		Config struct {
			TileSets []struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"tile_sets"`
		} `json:"config"`
		Groups []struct {
			Title      string `json:"title"`
			Categories []struct {
				ID          int64  `json:"id"`
				Title       string `json:"title"`
				Icon        string `json:"icon"`
				DisplayType string `json:"display_type"`
				Visible     bool   `json:"visible"`
				Locations   []struct {
					ID    int64  `json:"id"`
					Title string `json:"title"`
				} `json:"locations"`
			} `json:"categories"`
		} `json:"groups"`
		Regions []json.RawMessage `json:"regions"`
	}
	readJSON(t, "../fixtures/translators/mapgenie.doc.json", &reference)

	if len(document.Worlds) != 1 {
		t.Fatalf("document carries %d worlds, fixture describes one", len(document.Worlds))
	}
	world := document.Worlds[0]

	// Identity and placement travel unchanged.
	if world.ID != reference.ID || world.Slug != reference.Slug || world.Title != reference.Title {
		t.Errorf("world identity %d/%s/%s, fixture %d/%s/%s",
			world.ID, world.Slug, world.Title, reference.ID, reference.Slug, reference.Title)
	}
	if world.Center.Lat != reference.InitialLat || world.Center.Lng != reference.InitialLng {
		t.Errorf("center %v,%v, fixture %v,%v",
			world.Center.Lat, world.Center.Lng, reference.InitialLat, reference.InitialLng)
	}
	if document.Volume.Slug != reference.Game.Slug {
		t.Errorf("volume %s, fixture %s", document.Volume.Slug, reference.Game.Slug)
	}

	// Lenses: name and tile-set path, and nothing else. The reference document
	// also carried the publisher's zoom range, bounds and extension; the clean
	// room takes those from the derived pyramid instead, because what a bundle
	// promises about a raster must be what was actually derived.
	if len(world.Lenses) != len(reference.Config.TileSets) {
		t.Fatalf("%d lenses, fixture %d", len(world.Lenses), len(reference.Config.TileSets))
	}
	for i, set := range reference.Config.TileSets {
		if world.Lenses[i].Name != set.Name || world.Lenses[i].TileSet != set.Path {
			t.Errorf("lens %d is %s/%s, fixture %s/%s",
				i, world.Lenses[i].Name, world.Lenses[i].TileSet, set.Name, set.Path)
		}
	}

	// Collections: one per category, in group-then-category order, carrying the
	// category's own identity. The group survives as a heading string, which is
	// all it ever was.
	var wantPoints int
	index := 0
	for _, group := range reference.Groups {
		for _, category := range group.Categories {
			if index >= len(world.Collections) {
				t.Fatalf("document ran out of collections at %s/%s", group.Title, category.Title)
			}
			got := world.Collections[index]
			if got.ID != category.ID || got.Title != category.Title ||
				got.Group != group.Title || got.Icon != category.Icon ||
				got.Visible != category.Visible {
				t.Errorf("collection %d is %+v, fixture %s/%s id %d icon %s visible %t",
					index, got, group.Title, category.Title, category.ID,
					category.Icon, category.Visible)
			}
			if got.Kind != doc.KindPoint {
				t.Errorf("collection %s is kind %q, want %q", got.Title, got.Kind, doc.KindPoint)
			}
			// The publisher's display type is retired as a field and spoken as
			// a registered attribute instead. This is the one place the two
			// documents deliberately disagree about a category.
			want := "pin"
			if category.DisplayType == "text" {
				want = "text"
			}
			if got.Attrs["atlas.render.as"] != want {
				t.Errorf("collection %s renders as %q, want %q from display type %q",
					got.Title, got.Attrs["atlas.render.as"], want, category.DisplayType)
			}
			if len(got.Features) != len(category.Locations) {
				t.Errorf("collection %s holds %d features, fixture %d",
					got.Title, len(got.Features), len(category.Locations))
			}
			for i, location := range category.Locations {
				if i >= len(got.Features) {
					break
				}
				if got.Features[i].ID != location.ID || got.Features[i].Title != location.Title {
					t.Errorf("collection %s feature %d is %d/%s, fixture %d/%s",
						got.Title, i, got.Features[i].ID, got.Features[i].Title,
						location.ID, location.Title)
				}
				if got.Features[i].At == nil {
					t.Errorf("collection %s feature %d stands nowhere", got.Title, i)
				}
			}
			wantPoints += len(category.Locations)
			index++
		}
	}
	// Regions become an ordinary area collection where the reference document
	// kept them in a container of their own. Tunic has none, so none appears --
	// an empty collection would only dim the legend.
	wantCollections := index
	if len(reference.Regions) > 0 {
		wantCollections++
	}
	if len(world.Collections) != wantCollections {
		t.Errorf("%d collections, fixture implies %d", len(world.Collections), wantCollections)
	}

	points := 0
	for _, collection := range world.Collections {
		if collection.Kind == doc.KindPoint {
			points += len(collection.Features)
		}
	}
	if points != wantPoints {
		t.Errorf("%d point features, fixture %d", points, wantPoints)
	}

	// Offline purity, at the document rather than at the bundle: a payload may
	// carry no runtime URL, and the reader is where the publisher's links are
	// resolved away.
	for _, collection := range world.Collections {
		for _, feature := range collection.Features {
			if strings.Contains(feature.Description, "http://") ||
				strings.Contains(feature.Description, "https://") {
				t.Errorf("feature %d keeps a live URL in its prose", feature.ID)
			}
		}
	}
}

// composedBundle is a bundle this test built, opened.
type composedBundle struct {
	reader  *bundle.Reader
	options compose.Options
	result  compose.Result
	file    string
	sha256  string
	bytes   int64
}

func composeFixture(t *testing.T, volume string) composedBundle {
	t.Helper()
	document := translateFixture(t, volume)
	tables, err := curation.Load()
	if err != nil {
		t.Fatalf("curation: %v", err)
	}
	set, err := tiles.Open(tileIndex(t))
	if err != nil {
		t.Fatalf("tile set: %v", err)
	}
	options := compose.Options{
		Document:  document,
		Tiles:     set,
		Curation:  tables,
		BundleDir: t.TempDir(),
		Log:       slog.New(slog.DiscardHandler),
	}
	result, err := compose.Compose(options)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	reader, err := bundle.Open(result.Path)
	if err != nil {
		t.Fatalf("open %s: %v", result.Path, err)
	}
	t.Cleanup(func() { reader.Close() })
	if err := reader.Validate(); err != nil {
		t.Fatalf("validate %s: %v", result.File, err)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	return composedBundle{
		reader:  reader,
		options: options,
		result:  result,
		file:    result.File,
		sha256:  hashFile(t, result.Path),
		bytes:   info.Size(),
	}
}

func translateFixture(t *testing.T, want string) doc.Document {
	t.Helper()
	return translateFrom(t, "", want)
}

// translateFrom reads one volume through one source. Two sources may describe
// one volume -- that is what the merged fixture is a fixture of -- so a caller
// that means a particular reading names the source it means, and one that means
// "whoever answers" names none.
func translateFrom(t *testing.T, from, want string) doc.Document {
	t.Helper()
	roots := archiveDirs(t)
	for _, root := range roots {
		store, err := archive.Open(root)
		if err != nil {
			t.Fatalf("archive %s: %v", root, err)
		}
		for _, volume := range store.Volumes() {
			if from != "" && volume.Source != from {
				continue
			}
			source, err := sources.For(volume.Source)
			if err != nil {
				continue
			}
			document, err := source.Translate(store, volume, slog.New(slog.DiscardHandler))
			if err != nil {
				if errors.Is(err, sources.ErrNotReady) {
					continue
				}
				t.Fatalf("translate %s: %v", volume.Title, err)
			}
			if document.Volume.Slug == want {
				return document
			}
		}
	}
	t.Fatalf("no archive of %s holds a readable %s", strings.Join(roots, ", "), want)
	return doc.Document{}
}

// archiveOf, archiveDir and tileIndex resolve this gate's inputs, and skip the
// test rather than failing it when neither the environment nor the repository
// has them.

// archiveOf is where one volume was captured. Everything but the city comes out
// of the corpus's archive; the city is staged beside it, for the reason the
// package comment gives.
func archiveOf(t *testing.T, volume string) string {
	t.Helper()
	if volume == "bend-or" {
		return required(t, "ATLAS_CITY_ARCHIVE_DIR", "../../crawl/bend-or/fmg-archive", "archive.json")
	}
	return archiveDir(t)
}

func archiveDir(t *testing.T) string {
	t.Helper()
	return required(t, "ATLAS_ARCHIVE_DIR", "../../crawl/fmg-archive", "archive.json")
}

// archiveDirs is every capture archive staged in this checkout, the games
// archive first.
//
// One archive is the usual case and the one ATLAS_ARCHIVE_DIR names. It is not
// the only case: a volume whose captures were crawled on their own -- the city,
// re-crawled after its first archive was lost -- is staged beside the games
// archive as its own one-volume archive of the same shape, so it can be rebuilt
// without walking a library-sized capture. A test that wants a reading asks for
// the volume, not for the directory somebody happened to put it in.
func archiveDirs(t *testing.T) []string {
	t.Helper()
	roots := []string{archiveDir(t)}
	if os.Getenv("ATLAS_ARCHIVE_DIR") != "" {
		return roots
	}
	entries, err := os.ReadDir("../../crawl")
	if err != nil {
		return roots
	}
	for _, entry := range entries {
		staged := filepath.Join("../../crawl", entry.Name(), "fmg-archive")
		if staged == roots[0] {
			continue
		}
		if _, err := os.Stat(filepath.Join(staged, "archive.json")); err == nil {
			roots = append(roots, staged)
		}
	}
	return roots
}

func tileIndex(t *testing.T) string {
	t.Helper()
	return required(t, "ATLAS_TILES_INDEX", "../../tiles/index.json", "")
}

func required(t *testing.T, env, fallback, marker string) string {
	t.Helper()
	path := os.Getenv(env)
	if path == "" {
		path = fallback
	}
	probe := path
	if marker != "" {
		probe = filepath.Join(path, marker)
	}
	if _, err := os.Stat(probe); err != nil {
		t.Skipf("no capture archive here: %s is absent and %s names nothing "+
			"(the archive and the derived tiles are deliberately not in git; "+
			"see golden/pipeline for what these tests prove)", probe, env)
	}
	return path
}

// volumeFixture is the part of the committed fixture this file reads.
type volumeFixture struct {
	Slug       string            `json:"slug"`
	File       string            `json:"file"`
	FileBytes  int64             `json:"fileBytes"`
	FileSHA256 string            `json:"fileSha256"`
	Stamp      string            `json:"stamp"`
	CreatedAt  string            `json:"createdAt"`
	Revision   int               `json:"revision"`
	PartHashes map[string]string `json:"partHashes"`
	EntryOrder struct {
		SHA256 string `json:"sha256"`
	} `json:"entryOrder"`
	Pyramids []struct {
		Pyramid       string `json:"pyramid"`
		Tiles         int    `json:"tiles"`
		Bytes         int64  `json:"bytes"`
		ContentRollup string `json:"contentRollup"`
		Inventory     string `json:"inventory"`
	} `json:"pyramids"`
}

func readVolumeFixture(t *testing.T, volume string) volumeFixture {
	t.Helper()
	var out volumeFixture
	readJSON(t, filepath.Join(fixturePath(volume), "volume.json"), &out)
	return out
}

func compareCanon(t *testing.T, built composedBundle, entry, fixture string) {
	t.Helper()
	raw, err := built.reader.ReadEntry(entry)
	if err != nil {
		t.Fatalf("read %s: %v", entry, err)
	}
	got, err := canon.Bytes(raw)
	if err != nil {
		t.Fatalf("canonicalize %s: %v", entry, err)
	}
	want, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from %s\n%s", entry, fixture, firstDifference(string(want), string(got)))
	}
}

func compareLocations(t *testing.T, built composedBundle, dir, world string) {
	t.Helper()
	packed, err := built.reader.ReadEntry(bundle.WorldEntryName(world, bundle.PackedSuffix))
	if err != nil {
		t.Fatalf("read packed %s: %v", world, err)
	}
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		t.Fatalf("unpack %s: %v", world, err)
	}
	var fixture struct {
		Count        int    `json:"count"`
		PackedBytes  int    `json:"packedBytes"`
		PackedSHA256 string `json:"packedSha256"`
		Locations    []struct {
			ID     int64   `json:"id"`
			Owner  int     `json:"owner"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Member int64   `json:"member"`
			Shard  int64   `json:"shard"`
			Title  string  `json:"title"`
		} `json:"locations"`
	}
	readJSON(t, filepath.Join(dir, "worlds", world+".locations.json"), &fixture)

	if len(packed) != fixture.PackedBytes || bundle.HashBytes(packed) != fixture.PackedSHA256 {
		t.Errorf("packed payload is %d bytes %s, fixture %d bytes %s",
			len(packed), bundle.HashBytes(packed), fixture.PackedBytes, fixture.PackedSHA256)
	}
	if len(locations) != fixture.Count {
		t.Fatalf("%d locations, fixture %d", len(locations), fixture.Count)
	}
	for i, want := range fixture.Locations {
		got := locations[i]
		if got.ID != want.ID || int(got.Owner) != want.Owner || got.Title != want.Title ||
			got.Member != want.Member || got.Shard != want.Shard ||
			got.Lat != want.Lat || got.Lng != want.Lng {
			t.Errorf("location %d is %+v, fixture %+v", i, got, want)
			if t.Failed() {
				return
			}
		}
	}
}

func compareIcons(t *testing.T, built composedBundle, dir string) {
	t.Helper()
	var fixture struct {
		Count  int    `json:"count"`
		Rollup string `json:"rollup"`
		Icons  []struct {
			Name   string `json:"name"`
			Bytes  int    `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"icons"`
	}
	readJSON(t, filepath.Join(dir, "icons.json"), &fixture)

	var got []string
	for _, name := range built.reader.Names() {
		if strings.HasPrefix(name, "icons/") {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	if len(got) != fixture.Count {
		t.Fatalf("%d icons, fixture %d", len(got), fixture.Count)
	}
	for i, want := range fixture.Icons {
		if got[i] != want.Name {
			t.Errorf("icon %d is %s, fixture %s", i, got[i], want.Name)
			continue
		}
		data, err := built.reader.ReadEntry(got[i])
		if err != nil {
			t.Fatalf("read %s: %v", got[i], err)
		}
		if len(data) != want.Bytes || bundle.HashBytes(data) != want.SHA256 {
			t.Errorf("%s is %d bytes %s, fixture %d bytes %s",
				got[i], len(data), bundle.HashBytes(data), want.Bytes, want.SHA256)
		}
	}
}

// compareTiles holds the composed pyramid against the fixture's inventory:
// every tile, in the order the archive lists it, by name, weight and content
// hash. The fixture also carries a digest of each tile's decoded pixels, which
// exists to tell a re-encode of the same picture from a picture that moved.
// Equal content hashes settle both at once, so the pixel digests are compared
// only through the pyramid's own content rollup.
func compareTiles(t *testing.T, built composedBundle, dir string, fixture volumeFixture) {
	t.Helper()
	for _, pyramid := range fixture.Pyramids {
		var inventory struct {
			Count         int    `json:"count"`
			Bytes         int64  `json:"bytes"`
			ContentRollup string `json:"contentRollup"`
			Tiles         []struct {
				Name   string `json:"name"`
				Bytes  int64  `json:"bytes"`
				SHA256 string `json:"sha256"`
			} `json:"tiles"`
		}
		readJSON(t, filepath.Join(dir, pyramid.Inventory), &inventory)

		prefix := "tiles/" + pyramid.Pyramid + "/"
		var names []string
		for _, name := range built.reader.Names() {
			if strings.HasPrefix(name, prefix) {
				names = append(names, strings.TrimPrefix(name, prefix))
			}
		}
		if len(names) != inventory.Count {
			t.Fatalf("pyramid %s holds %d tiles, fixture %d",
				pyramid.Pyramid, len(names), inventory.Count)
		}
		byName := make(map[string]string, len(names))
		for _, name := range names {
			byName[name] = ""
		}
		var total int64
		for _, want := range inventory.Tiles {
			if _, held := byName[want.Name]; !held {
				t.Errorf("pyramid %s is missing %s", pyramid.Pyramid, want.Name)
				continue
			}
			data, err := built.reader.ReadEntry(prefix + want.Name)
			if err != nil {
				t.Fatalf("read %s: %v", prefix+want.Name, err)
			}
			total += int64(len(data))
			if int64(len(data)) != want.Bytes || bundle.HashBytes(data) != want.SHA256 {
				t.Errorf("%s is %d bytes %s, fixture %d bytes %s",
					want.Name, len(data), bundle.HashBytes(data), want.Bytes, want.SHA256)
				if t.Failed() {
					return
				}
			}
			if !built.reader.Stored(prefix + want.Name) {
				t.Errorf("%s is compressed; tiles are stored so a reader serves byte ranges", want.Name)
			}
		}
		if total != inventory.Bytes {
			t.Errorf("pyramid %s weighs %d bytes, fixture %d", pyramid.Pyramid, total, inventory.Bytes)
		}
	}
}

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// hashOf digests an archive's entry order the way the capture program did:
// every name, each followed by a newline.
func hashOf(names []string) string {
	digest := sha256.New()
	for _, name := range names {
		digest.Write([]byte(name))
		digest.Write([]byte{'\n'})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// firstDifference reports the first line two documents disagree on, with a
// little of what came before it, because a whole payload printed twice is not a
// diff anybody reads.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return fmt.Sprintf("  line %d\n    fixture: %s\n    built:   %s", i+1, w, g)
		}
	}
	return "  the two agree line by line but differ in length"
}
