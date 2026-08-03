package main

// The included Earth volume, held to its promises: the committed bundle is a
// valid, ordinary format-v3 volume; startup installs it through the format's
// own registry rules; and the installed file serves through the same
// application paths any imported bundle serves through. Every test here reads
// the embedded asset -- the same bytes a release build ships -- so what is
// judged is what a first launch actually opens.

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
)

// includedEarth reads the one embedded bundle. There being exactly one is part
// of the contract: the embed pattern would happily carry a stray second build,
// and two included Earths would be two answers to what a first launch shows.
func includedEarth(t *testing.T) []byte {
	t.Helper()
	names, err := fs.Glob(included, "included/*.atlas")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("the executable embeds %d bundles %v, want exactly the one Earth", len(names), names)
	}
	data, err := included.ReadFile(names[0])
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openIncluded(t *testing.T) *bundle.Reader {
	t.Helper()
	data := includedEarth(t)
	reader, err := bundle.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// earthPayload is as much of the world payload as these tests read.
type earthPayload struct {
	Lenses []struct {
		Name       string   `json:"name"`
		Tiles      string   `json:"tiles"`
		MinZoom    int      `json:"minZoom"`
		MaxZoom    int      `json:"maxZoom"`
		FullZoom   int      `json:"fullZoom"`
		SourceZoom int      `json:"sourceZoom"`
		Formats    []string `json:"formats"`
		Bounds     *struct {
			X, Y, Width, Height int
		} `json:"bounds"`
	} `json:"lenses"`
	Collections []struct {
		Title     string            `json:"title"`
		Group     string            `json:"group"`
		Kind      string            `json:"kind"`
		IconAsset string            `json:"iconAsset"`
		Attrs     map[string]string `json:"attrs"`
		Features  []json.RawMessage `json:"features"`
	} `json:"collections"`
	Attrs       map[string]string `json:"attrs"`
	Merged      []struct {
		Source string `json:"source"`
		Slug   string `json:"slug"`
		Origin bool   `json:"origin"`
	} `json:"merged"`
}

func readEarthPayload(t *testing.T, reader *bundle.Reader) earthPayload {
	t.Helper()
	data, err := reader.ReadEntry(bundle.WorldEntryName("earth", bundle.WorldSuffix))
	if err != nil {
		t.Fatal(err)
	}
	var payload earthPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// TestIncludedEarthIsAValidOrdinaryBundle: the committed built-in passes the
// format's whole validation surface and says exactly what the included volume
// is specified to say -- format 3, conventions 2, volume earth, one world
// earth, raster-only.
func TestIncludedEarthIsAValidOrdinaryBundle(t *testing.T) {
	reader := openIncluded(t)
	if err := reader.Validate(); err != nil {
		t.Fatalf("the committed built-in does not validate: %v", err)
	}
	m := reader.Manifest
	if m.FormatVersion != 3 || m.Conventions != 2 {
		t.Errorf("format %d conventions %d, want 3 and 2", m.FormatVersion, m.Conventions)
	}
	if m.Volume.Slug != "earth" || m.Volume.Title != "Earth" {
		t.Errorf("volume %s/%s", m.Volume.Slug, m.Volume.Title)
	}
	if len(m.Worlds) != 1 || m.Worlds[0].Slug != "earth" || m.Worlds[0].Title != "Earth" {
		t.Fatalf("worlds %+v, want exactly the one earth", m.Worlds)
	}
	if m.Worlds[0].Points != 202 || m.Worlds[0].Paths != 0 || m.Worlds[0].Areas != 177 {
		t.Errorf("the manifest counts %+v; the demo carries 202 capitals and 177 countries", m.Worlds[0])
	}
	if m.Version.CreatedAt != "2026-08-03T16:21:07Z" {
		t.Errorf("createdAt %s is not the pinned capture time", m.Version.CreatedAt)
	}
}

// TestIncludedEarthDeclaresTheWholeSphere: one Blue Marble lens over the
// complete 8192×4096 Earth surface, the demo's capitals and countries, and
// exactly the spherical equirectangular declarations the analysis contracts
// recognise.
func TestIncludedEarthDeclaresTheWholeSphere(t *testing.T) {
	reader := openIncluded(t)
	payload := readEarthPayload(t, reader)

	if len(payload.Collections) != 7 {
		t.Fatalf("%d collections, want six continents of capitals and the countries", len(payload.Collections))
	}
	for _, held := range payload.Collections[:6] {
		if held.Group != "Capitals" || held.Kind != "point" || held.IconAsset == "" {
			t.Errorf("capitals collection %q arrives as %s/%s with icon %q",
				held.Title, held.Group, held.Kind, held.IconAsset)
		}
	}
	countries := payload.Collections[6]
	if countries.Title != "Countries" || countries.Kind != "area" ||
		countries.Attrs[semconv.KeyLabelPolicy] != semconv.LabelQuiet || len(countries.Features) != 177 {
		t.Errorf("the ground arrives as %q %s (%d features, %v)",
			countries.Title, countries.Kind, len(countries.Features), countries.Attrs)
	}
	if len(payload.Lenses) != 1 {
		t.Fatalf("%d lenses, want the one Blue Marble", len(payload.Lenses))
	}
	lens := payload.Lenses[0]
	if lens.Name != "Blue Marble" {
		t.Errorf("lens %q", lens.Name)
	}
	if lens.MinZoom != 0 || lens.MaxZoom != 6 || lens.FullZoom != 6 || lens.SourceZoom != 6 {
		t.Errorf("zooms min %d max %d full %d source %d, want 0/6/6/6",
			lens.MinZoom, lens.MaxZoom, lens.FullZoom, lens.SourceZoom)
	}
	if len(lens.Formats) != 7 {
		t.Errorf("%d formats for 7 levels", len(lens.Formats))
	}
	for _, format := range lens.Formats {
		if format != "jpg" {
			t.Errorf("a level is %q, want jpg throughout", format)
		}
	}
	if lens.Bounds == nil || *lens.Bounds != struct{ X, Y, Width, Height int }{0, 0, 8192, 4096} {
		t.Errorf("bounds %+v, want the complete 0,0,8192,4096 surface", lens.Bounds)
	}

	want := map[string]string{
		semconv.KeyGeometrySurface:     "sphere",
		semconv.KeyGeometryProjection:  "equirect",
		semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
		semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
		semconv.KeyGeometryBody:        "earth",
		semconv.KeyGeometryRadiusKM:    "6371.0088",
	}
	if len(payload.Attrs) != len(want) {
		t.Errorf("the world carries %d attributes %v, want %d", len(payload.Attrs), payload.Attrs, len(want))
	}
	for key, value := range want {
		if payload.Attrs[key] != value {
			t.Errorf("world says %s=%q, want %q", key, payload.Attrs[key], value)
		}
	}

	if len(payload.Merged) != 1 || !payload.Merged[0].Origin ||
		payload.Merged[0].Source != "NASA Earth Observatory" ||
		payload.Merged[0].Slug != "nasa-blue-marble" {
		t.Errorf("provenance ledger %+v; the credit is part of the artifact", payload.Merged)
	}
}

// TestIncludedEarthPyramidOpensAtEveryLevel: every advertised level holds its
// whole half-height window of stored, decodable JPEG tiles.
func TestIncludedEarthPyramidOpensAtEveryLevel(t *testing.T) {
	reader := openIncluded(t)
	perLevel := map[string]int{}
	for _, name := range reader.Names() {
		if !strings.HasPrefix(name, "tiles/earth/") {
			continue
		}
		parts := strings.Split(name, "/")
		perLevel[parts[2]]++
		if !reader.Stored(name) {
			t.Fatalf("%s is compressed; format v3 stores tiles", name)
		}
	}
	wantPerLevel := map[string]int{"0": 1, "1": 2, "2": 8, "3": 32, "4": 128, "5": 512, "6": 2048}
	for level, want := range wantPerLevel {
		if perLevel[level] != want {
			t.Errorf("level %s holds %d tiles, want %d", level, perLevel[level], want)
		}
	}
	// One tile of every level decodes as the JPEG its name claims.
	for _, name := range []string{
		"tiles/earth/0/0/0.jpg", "tiles/earth/1/1/0.jpg", "tiles/earth/2/3/1.jpg",
		"tiles/earth/3/7/3.jpg", "tiles/earth/4/15/7.jpg", "tiles/earth/5/31/15.jpg",
		"tiles/earth/6/63/31.jpg",
	} {
		data, err := reader.ReadEntry(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("%s does not decode: %v", name, err)
		}
	}
	if !reader.Stored(bundle.WorldEntryName("earth", bundle.PackedSuffix)) {
		t.Error("the packed payload is compressed; format v3 stores it")
	}
}

// TestIncludedEarthCarriesNoRuntimeURL is offline purity, asserted directly on
// the shipped bytes over and above Validate's own scan.
func TestIncludedEarthCarriesNoRuntimeURL(t *testing.T) {
	reader := openIncluded(t)
	for _, name := range []string{
		bundle.WorldEntryName("earth", bundle.WorldSuffix),
		bundle.WorldEntryName("earth", bundle.TextSuffix),
	} {
		data, err := reader.ReadEntry(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, scheme := range []string{"http://", "https://"} {
			if bytes.Contains(data, []byte(scheme)) {
				t.Errorf("%s carries %q", name, scheme)
			}
		}
	}
}

// TestInstallIncludedIsIdempotentAndOverwritesNothing: a first launch installs
// one versioned Earth file; a second launch changes nothing; and a distinct
// Earth build already in the library stays exactly where it is, side by side.
func TestInstallIncludedIsIdempotentAndOverwritesNothing(t *testing.T) {
	library := t.TempDir()
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a first launch installed %d files", len(entries))
	}
	installed := filepath.Join(library, entries[0].Name())
	if !strings.HasPrefix(entries[0].Name(), "earth-20260803-") || !strings.HasSuffix(entries[0].Name(), ".atlas") {
		t.Errorf("installed as %s, want the versioned earth name", entries[0].Name())
	}
	if _, err := bundle.Describe(installed); err != nil {
		t.Fatalf("the installed file does not describe: %v", err)
	}
	before, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}

	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a second launch grew the library to %d files", len(entries))
	}
	after, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Error("a second launch touched the installed file")
	}

	// A distinct Earth build -- another capture day, another stamp -- is not
	// overwritten and not deleted; the registry fold decides which one serves.
	other := writeDistinctEarth(t, library)
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a launch removed the other Earth build: %v", err)
	}
	entries, err = os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("the library holds %d files; two Earth builds sit side by side", len(entries))
	}
}

// writeDistinctEarth states a second, minimal Earth build through the format's
// real writer, dated after the included one so the fold prefers it.
func writeDistinctEarth(t *testing.T, dir string) string {
	t.Helper()
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Volume:        bundle.Volume{Slug: "earth", Title: "Earth"},
		Version:       bundle.Version{Stamp: "ffff", CreatedAt: "2026-09-01T00:00:00Z"},
		TileGrid:      bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
		Worlds: []bundle.WorldEntry{{
			Slug: "flat", Title: "Flat", UpdatedAt: "2026-09-01T00:00:00Z",
		}},
	}
	path := filepath.Join(dir, bundle.VersionedFileName(manifest))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDeflated("worlds/flat.json",
		[]byte(`{"lenses":[{"name":"Flat","tiles":"flat","minZoom":0,"maxZoom":0,"fullZoom":0,"sourceZoom":0,"formats":["jpg"],"interpolate":true}],"collections":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddStored("worlds/flat.bin", bytes.NewReader(bundle.PackLocations(nil))); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDeflated("worlds/flat.text", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddStored("tiles/flat/0/0/0.jpg", bytes.NewReader(statedJPEG(t))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func statedJPEG(t *testing.T) []byte {
	t.Helper()
	reader := openIncluded(t)
	data, err := reader.ReadEntry("tiles/earth/0/0/0.jpg")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestDesktopStartupExposesEarthOnTheFirstScan walks the shell's own startup
// order over a temporary empty data directory: library created, built-in
// installed, host constructed -- and the host's first scan already serves
// Earth, with no network anywhere in the path.
func TestDesktopStartupExposesEarthOnTheFirstScan(t *testing.T) {
	data := t.TempDir()
	library := filepath.Join(data, "bundles")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	host, err := oshost.New(oshost.Options{
		BundlesDir:  library,
		SessionsDir: filepath.Join(data, "sessions"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Volumes().(interface{ Close() error }).Close() })

	volumes := host.Volumes().Volumes()
	if len(volumes) != 1 {
		t.Fatalf("the first scan lists %d volumes", len(volumes))
	}
	manifest := volumes[0].Manifest()
	if manifest.Volume.Slug != "earth" || manifest.Volume.Title != "Earth" {
		t.Errorf("the first scan serves %s/%s", manifest.Volume.Slug, manifest.Volume.Title)
	}
}

// TestEarthServesThroughTheApplication: the installed file -- never the
// embedded copy -- answers the application's own data plane: the catalog lists
// Earth, the world page opens, and a raster tile reads back as the JPEG it is.
func TestEarthServesThroughTheApplication(t *testing.T) {
	data := t.TempDir()
	library := filepath.Join(data, "bundles")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	host, err := oshost.New(oshost.Options{
		BundlesDir:  library,
		SessionsDir: filepath.Join(data, "sessions"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Volumes().(interface{ Close() error }).Close() })
	handler := app.New(host, app.Options{})
	server := httptest.NewServer(handler)
	defer server.Close()

	catalog := get(t, server.Client(), server.URL+"/data/catalog.json")
	if !strings.Contains(string(catalog), `"earth"`) {
		t.Errorf("the catalog does not list earth: %s", catalog)
	}

	page := get(t, server.Client(), server.URL+"/v/earth/earth")
	if !strings.Contains(string(page), "Earth") {
		t.Error("the world page does not open on Earth")
	}

	stamp := bundle.ShortStamp(host.Volumes().Volumes()[0].Manifest().Version.Stamp)
	tile := get(t, server.Client(), server.URL+"/data/v/earth/"+stamp+"/tiles/earth/0/0/0.jpg")
	if _, err := jpeg.Decode(bytes.NewReader(tile)); err != nil {
		t.Fatalf("the served tile does not decode: %v", err)
	}
}

func get(t *testing.T, client *http.Client, url string) []byte {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
