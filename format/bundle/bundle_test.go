package bundle_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

func soundManifest() bundle.Manifest {
	return bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Volume:        bundle.Volume{Slug: "fixture", Title: "Fixture"},
		Version:       bundle.Version{Stamp: "abc", CreatedAt: "2026-01-01T00:00:00Z"},
		Worlds:        []bundle.WorldEntry{{Slug: "overworld", Title: "Overworld"}},
	}
}

func TestManifestValidation(t *testing.T) {
	if err := soundManifest().Validate(); err != nil {
		t.Fatalf("a sound manifest is refused: %v", err)
	}
	cases := map[string]func(*bundle.Manifest){
		"a container it does not name":  func(m *bundle.Manifest) { m.Format = "zip-of-things" },
		"a format version from ahead":   func(m *bundle.Manifest) { m.FormatVersion = 99 },
		"a format version from behind":  func(m *bundle.Manifest) { m.FormatVersion = 2 },
		"no volume slug":                func(m *bundle.Manifest) { m.Volume.Slug = "" },
		"a volume slug that climbs":     func(m *bundle.Manifest) { m.Volume.Slug = "../escape" },
		"a volume slug in caps":         func(m *bundle.Manifest) { m.Volume.Slug = "Fallout76" },
		"a volume slug with a space":    func(m *bundle.Manifest) { m.Volume.Slug = "fall out" },
		"a volume slug leading a dash":  func(m *bundle.Manifest) { m.Volume.Slug = "-fixture" },
		"no volume title":               func(m *bundle.Manifest) { m.Volume.Title = "" },
		"no stamp":                      func(m *bundle.Manifest) { m.Version.Stamp = "" },
		"no creation time":              func(m *bundle.Manifest) { m.Version.CreatedAt = "" },
		"no worlds":                     func(m *bundle.Manifest) { m.Worlds = nil },
		"a world slug with a separator": func(m *bundle.Manifest) { m.Worlds[0].Slug = "a/b" },
		"an untitled world":             func(m *bundle.Manifest) { m.Worlds[0].Title = "" },
		"a world listed twice":          func(m *bundle.Manifest) { m.Worlds = append(m.Worlds, m.Worlds[0]) },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := soundManifest()
			corrupt(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Errorf("a manifest with %s is accepted", name)
			}
		})
	}
}

func TestWorldEntryCountsFeatures(t *testing.T) {
	entry := bundle.WorldEntry{Points: 45, Paths: 5, Areas: 16}
	if got := entry.Features(); got != 66 {
		t.Errorf("Features = %d, want 66", got)
	}
}

func TestWriterRefusesUnsafeAndRepeatedEntries(t *testing.T) {
	writer, err := bundle.NewWriter(&bytes.Buffer{}, soundManifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"", "/etc/hosts", "worlds/../escape", "tiles/", "worlds//a.json",
		"./worlds/a.json", bundle.ManifestName,
	} {
		if err := writer.AddDeflated(name, []byte("x")); err == nil {
			t.Errorf("entry %q is accepted", name)
		}
	}
	if err := writer.AddDeflated("worlds/overworld.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDeflated("worlds/overworld.json", []byte("{}")); err == nil {
		t.Error("the same entry is accepted twice")
	}
}

func TestWriterRefusesAManifestAReaderWouldRefuse(t *testing.T) {
	manifest := soundManifest()
	manifest.Version.Stamp = ""
	if _, err := bundle.NewWriter(&bytes.Buffer{}, manifest); err == nil {
		t.Error("a stampless manifest started a bundle")
	}
}

// Tiles and packed locations are stored uncompressed so a server can answer a
// byte range straight out of the archive; payload JSON is deflated because it
// is read whole and shrinks severalfold.
func TestWriterStoresWhatIsServedByRange(t *testing.T) {
	reader := fixture{}.open(t, t.TempDir())
	stored := map[string]bool{
		bundle.WorldEntryName("overworld", bundle.PackedSuffix): true,
		bundle.TilesPrefix + "overworld/0/0/0.jpg":              true,
		bundle.WorldEntryName("overworld", bundle.WorldSuffix):  false,
		bundle.WorldEntryName("overworld", bundle.TextSuffix):   false,
		bundle.ManifestName: false,
	}
	for name, want := range stored {
		if got := reader.Stored(name); got != want {
			t.Errorf("%s stored = %v, want %v", name, got, want)
		}
	}
	if names := reader.Names(); len(names) == 0 || names[0] != bundle.ManifestName {
		t.Errorf("the archive does not lead with the manifest: %v", names)
	}
}

func TestOpenRoundTripsWhatAWriterWrote(t *testing.T) {
	reader := fixture{
		slug:  "fixture",
		stamp: "deadbeefcafe0000",
		worlds: []fixtureWorld{{
			slug: "overworld",
			locations: []bundle.Location{
				{ID: 1, Title: "Origin", Lat: 12.5, Lng: -3.25},
				{ID: 2, Title: "Peak", Lat: 80, Lng: 170},
			},
		}},
	}.open(t, t.TempDir())

	if reader.Manifest.Volume.Slug != "fixture" {
		t.Errorf("volume slug = %q", reader.Manifest.Volume.Slug)
	}
	if got := reader.Manifest.Worlds[0].Points; got != 2 {
		t.Errorf("point count = %d, want 2", got)
	}
	if err := reader.Validate(); err != nil {
		t.Errorf("a fixture bundle fails validation: %v", err)
	}

	packed, err := reader.Locations("overworld")
	if err != nil {
		t.Fatal(err)
	}
	if packed.Len() != 2 || packed.Title(0) != "Origin" || packed.Title(1) != "Peak" {
		t.Fatalf("locations round-tripped as %+v", packed.All())
	}
	if packed.Lat(0) != 12.5 || packed.Lng(0) != -3.25 {
		t.Errorf("Origin landed at %v,%v", packed.Lat(0), packed.Lng(0))
	}

	source, size, err := reader.OpenEntry(bundle.TilesPrefix + "overworld/0/0/0.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if size <= 0 {
		t.Errorf("tile size = %d", size)
	}
	if _, _, err := reader.OpenEntry("tiles/nowhere.jpg"); err == nil {
		t.Error("an absent entry opened")
	}
}

// A reader made over bytes owns nothing and can be handed a buffer, a mapped
// file, or an object store's ranged reader. It is what keeps the package free
// of any particular filesystem.
func TestNewReaderWorksOverBytes(t *testing.T) {
	path := fixture{slug: "fixture"}.build(t, t.TempDir())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := bundle.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if reader.Manifest.Volume.Slug != "fixture" {
		t.Errorf("volume slug = %q", reader.Manifest.Volume.Slug)
	}
	if reader.Path != "" {
		t.Errorf("a reader over bytes claims path %q", reader.Path)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("closing a reader that owns nothing: %v", err)
	}
	if reader.Unchanged(int64(len(data)), time0()) {
		t.Error("a reader over bytes claims a file did not change")
	}
}

func TestOpenRefusesWhatIsNotABundle(t *testing.T) {
	dir := t.TempDir()

	plain := filepath.Join(dir, "plain.atlas")
	if err := os.WriteFile(plain, []byte("not a zip at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(plain); err == nil {
		t.Error("a file that is not an archive is accepted")
	}
	if _, err := bundle.Open(filepath.Join(dir, "absent.atlas")); err == nil {
		t.Error("a file that is not there is accepted")
	}

	// A real zip with no manifest is somebody's archive wearing our suffix.
	bare := filepath.Join(dir, "bare.atlas")
	if err := os.WriteFile(bare, archiveWithout(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(bare); err == nil || !strings.Contains(err.Error(), bundle.ManifestName) {
		t.Errorf("a zip without a manifest is refused with %v, wanting a complaint about %s", err, bundle.ManifestName)
	}
}

// An unknown format version is refused outright, which is the one place the
// reader is strict. Everything else about a newer bundle it would happily
// ignore; a layout it does not know it cannot.
func TestOpenRefusesAFormatVersionItDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future.atlas")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create(bundle.ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(`{"format":"atlas-bundle","formatVersion":4,` +
		`"volume":{"slug":"future","title":"Future"},` +
		`"version":{"stamp":"aa","createdAt":"2026-01-01T00:00:00Z"},` +
		`"worlds":[{"slug":"overworld","title":"Overworld"}]}`)); err != nil {
		t.Fatal(err)
	}
	must(t, archive.Close())
	must(t, file.Close())

	if _, err := bundle.Open(path); err == nil || !strings.Contains(err.Error(), "format version") {
		t.Errorf("a version-4 bundle opened with %v", err)
	}
}

func TestOpenRefusesAnImplausibleManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.atlas")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create(bundle.ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(bytes.Repeat([]byte(" "), bundle.MaxManifestSize+1)); err != nil {
		t.Fatal(err)
	}
	must(t, archive.Close())
	must(t, file.Close())

	if _, err := bundle.Open(path); err == nil || !strings.Contains(err.Error(), "implausibly large") {
		t.Errorf("an oversized manifest opened with %v", err)
	}
}

func TestMarshalManifestIsTheEncodingThatCounts(t *testing.T) {
	manifest := soundManifest()
	manifest.Conventions = 2
	data, err := bundle.MarshalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":"atlas-bundle","formatVersion":3,"conventions":2,` +
		`"volume":{"slug":"fixture","title":"Fixture"},` +
		`"version":{"stamp":"abc","createdAt":"2026-01-01T00:00:00Z"},` +
		`"tileGrid":{"sourceZoom":0,"firstTile":0,"tileSize":0,"size":0},` +
		`"worlds":[{"slug":"overworld","title":"Overworld","center":{"lat":0,"lng":0},` +
		`"points":0,"paths":0,"areas":0,"updatedAt":""}]}`
	if string(data) != want {
		t.Errorf("manifest encoded as\n%s\nwant\n%s", data, want)
	}

	// Conventions and revision are the two fields a bundle may leave out; an
	// undeclaring bundle must not carry them at all.
	plain := soundManifest()
	data, err = bundle.MarshalManifest(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "conventions") || strings.Contains(string(data), "revision") {
		t.Errorf("an undeclaring manifest carries them anyway: %s", data)
	}
}

func TestWorldEntryName(t *testing.T) {
	cases := map[string]string{
		bundle.WorldSuffix:  "worlds/overworld.json",
		bundle.PackedSuffix: "worlds/overworld.bin",
		bundle.TextSuffix:   "worlds/overworld.text",
	}
	for suffix, want := range cases {
		if got := bundle.WorldEntryName("overworld", suffix); got != want {
			t.Errorf("WorldEntryName(overworld, %q) = %q, want %q", suffix, got, want)
		}
	}
}

// archiveWithout builds a genuine zip holding a single unrelated entry,
// exercising the reader against an archive that is not a bundle.
func archiveWithout(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("README.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("just an archive")); err != nil {
		t.Fatal(err)
	}
	must(t, archive.Close())
	return buffer.Bytes()
}

// time0 is a zero modification time, for the one assertion that a reader over
// bytes never claims to know a file.
func time0() time.Time { return time.Time{} }
