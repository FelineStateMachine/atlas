package bundle_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/bundle/bundletest"
)

func validManifest() bundle.Manifest {
	return bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Volume:        bundle.Volume{Slug: "fixture", Title: "Fixture"},
		Version:       bundle.Version{Stamp: "abc", CreatedAt: "2026-01-01T00:00:00Z"},
		Worlds:        []bundle.WorldEntry{{Slug: "overworld", Title: "Overworld"}},
	}
}

func TestManifestValidation(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("a valid manifest is refused: %v", err)
	}
	cases := map[string]func(*bundle.Manifest){
		"wrong format":        func(m *bundle.Manifest) { m.Format = "zip-of-things" },
		"unknown version":     func(m *bundle.Manifest) { m.FormatVersion = 99 },
		"empty game slug":     func(m *bundle.Manifest) { m.Volume.Slug = "" },
		"unsafe game slug":    func(m *bundle.Manifest) { m.Volume.Slug = "../escape" },
		"uppercase game slug": func(m *bundle.Manifest) { m.Volume.Slug = "Fallout76" },
		"untitled game":       func(m *bundle.Manifest) { m.Volume.Title = "" },
		"missing stamp":       func(m *bundle.Manifest) { m.Version.Stamp = "" },
		"missing created at":  func(m *bundle.Manifest) { m.Version.CreatedAt = "" },
		"no maps":             func(m *bundle.Manifest) { m.Worlds = nil },
		"unsafe map slug":     func(m *bundle.Manifest) { m.Worlds[0].Slug = "a/b" },
		"duplicate map slugs": func(m *bundle.Manifest) { m.Worlds = append(m.Worlds, m.Worlds[0]) },
	}
	for name, corrupt := range cases {
		manifest := validManifest()
		corrupt(&manifest)
		if err := manifest.Validate(); err == nil {
			t.Errorf("a manifest with %s is accepted", name)
		}
	}
}

func TestWriterRefusesUnsafeAndRepeatedEntries(t *testing.T) {
	writer, err := bundle.NewWriter(&bytes.Buffer{}, validManifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "/etc/hosts", "worlds/../escape", "tiles/", bundle.ManifestName} {
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

func TestOpenRoundTripsWhatBuildWrites(t *testing.T) {
	path := bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "fixture",
		Worlds: []bundletest.WorldSpec{{
			Slug: "overworld",
			Pins: []bundletest.Pin{{Title: "Origin", Lat: 12.5, Lng: -3.25}, {Title: "Peak", Lat: 80, Lng: 170}},
		}},
	})
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	if opened.Manifest.Volume.Slug != "fixture" {
		t.Errorf("game slug = %q", opened.Manifest.Volume.Slug)
	}
	if got := opened.Manifest.Worlds[0].Points; got != 2 {
		t.Errorf("point count = %d, want 2", got)
	}
	if err := opened.Validate(); err != nil {
		t.Errorf("a fixture bundle fails validation: %v", err)
	}

	packed, err := opened.ReadEntry("worlds/overworld.bin")
	if err != nil {
		t.Fatal(err)
	}
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 2 || locations[0].Title != "Origin" || locations[1].Title != "Peak" {
		t.Fatalf("locations round-tripped as %+v", locations)
	}
	if locations[0].Lat != 12.5 || locations[0].Lng != -3.25 {
		t.Errorf("Origin landed at %v,%v", locations[0].Lat, locations[0].Lng)
	}

	reader, size, err := opened.OpenEntry("tiles/overworld/0/0/0.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if size <= 0 {
		t.Errorf("tile size = %d", size)
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

	// A real zip with no manifest is somebody's archive wearing our suffix.
	bare := filepath.Join(dir, "bare.atlas")
	if err := os.WriteFile(bare, zipWithout(t, bare), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(bare); err == nil || !strings.Contains(err.Error(), bundle.ManifestName) {
		t.Errorf("a zip without a manifest is refused with %v, wanting a complaint about %s", err, bundle.ManifestName)
	}
}

func TestValidateCatchesBrokenPromises(t *testing.T) {
	dir := t.TempDir()

	// The manifest promises two maps; only one is written.
	manifest := validManifest()
	manifest.Worlds = append(manifest.Worlds, bundle.WorldEntry{Slug: "underworld", Title: "Underworld"})
	path := filepath.Join(dir, "fixture.atlas")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(writer.AddDeflated("worlds/overworld.json",
		[]byte(`{"lenses":[{"tiles":"overworld","minZoom":0,"maxZoom":0,"formats":["jpg"]}],"collections":[]}`)))
	must(writer.AddStored("worlds/overworld.bin", bytes.NewReader(bundle.PackLocations(nil))))
	must(writer.AddDeflated("worlds/overworld.text", []byte(`{}`)))
	must(writer.AddStored("tiles/overworld/0/0/0.jpg", bytes.NewReader([]byte("raster"))))
	must(writer.Close())
	file.Close()

	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := opened.Validate(); err == nil || !strings.Contains(err.Error(), "underworld") {
		t.Errorf("a missing map validates with %v, wanting a complaint about underworld", err)
	}
}

// buildForValidation writes a one-world bundle whose payload JSON, text, and
// per-kind manifest counts a case chooses, so each promise Validate makes can
// be broken one at a time.
func buildForValidation(t *testing.T, detail, text string, points, paths, areas int) *bundle.Bundle {
	t.Helper()
	manifest := validManifest()
	manifest.Worlds[0].Points = points
	manifest.Worlds[0].Paths = paths
	manifest.Worlds[0].Areas = areas
	path := filepath.Join(t.TempDir(), "fixture.atlas")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	// At most one location is packed however many the manifest promises, so
	// a case can promise more than the payload holds.
	locations := make([]bundle.Location, min(points, 1))
	for index := range locations {
		locations[index] = bundle.Location{ID: int64(index + 1), Title: "Origin"}
	}
	steps := []error{
		writer.AddDeflated("worlds/overworld.json", []byte(detail)),
		writer.AddStored("worlds/overworld.bin", bytes.NewReader(bundle.PackLocations(locations))),
		writer.AddDeflated("worlds/overworld.text", []byte(text)),
		writer.AddStored("tiles/overworld/0/0/0.jpg", bytes.NewReader([]byte("raster"))),
		writer.Close(),
		file.Close(),
	}
	for _, err := range steps {
		if err != nil {
			t.Fatal(err)
		}
	}
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}

const validationLenses = `"lenses":[{"tiles":"overworld","minZoom":0,"maxZoom":0,"formats":["jpg"]}]`

func TestValidateRefusesRuntimeURLsAndWrongCounts(t *testing.T) {
	pointOnly := `{` + validationLenses + `,"collections":[{"id":1,"title":"Marker","kind":"point","visible":true}]}`

	if err := buildForValidation(t, pointOnly, `{"1":{"d":"see https://mapgenie.io/x"}}`, 1, 0, 0).
		Validate(); err == nil || !strings.Contains(err.Error(), "runtime URL") {
		t.Errorf("a payload carrying a live URL validates with %v", err)
	}
	if err := buildForValidation(t, pointOnly, `{}`, 7, 0, 0).
		Validate(); err == nil || !strings.Contains(err.Error(), "7") {
		t.Errorf("a wrong point count validates with %v", err)
	}
	if err := buildForValidation(t, pointOnly, `{}`, 1, 0, 0).Validate(); err != nil {
		t.Errorf("a sound bundle is refused: %v", err)
	}
}

// The v3 wire's structural promises: every collection is the kind it says,
// paths carry their stroke, labels are curated only where labels draw, points
// never inline, and the manifest's per-kind counts hold.
func TestValidateHoldsCollectionsToTheirKinds(t *testing.T) {
	area := func(attrs string) string {
		return `{` + validationLenses + `,"collections":[{"id":9,"title":"Districts","kind":"area",` +
			`"visible":true` + attrs + `,"features":[{"id":5,"title":"R-5",` +
			`"geometry":[{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]}]}]}]}`
	}
	line := `{"type":"MultiLineString","coordinates":[[[0,0],[1,1]]]}`
	cases := []struct {
		name          string
		detail        string
		paths, areas  int
		wantSomewhere string
	}{
		{
			name:          "geometry disagreeing with the declared kind",
			detail:        `{` + validationLenses + `,"collections":[{"id":9,"title":"Districts","kind":"area","visible":true,"features":[{"id":5,"title":"R-5","geometry":[` + line + `]}]}]}`,
			areas:         1,
			wantSomewhere: "inlines a MultiLineString",
		},
		{
			name:          "a path collection with no stroke width",
			detail:        `{` + validationLenses + `,"collections":[{"id":9,"title":"Creeks","kind":"path","visible":true,"features":[{"id":5,"title":"Big Dry Creek","geometry":[` + line + `]}]}]}`,
			paths:         1,
			wantSomewhere: "declares no atlas.stroke.width_px",
		},
		{
			name:          "a manifest counting more areas than the payload holds",
			detail:        area(""),
			areas:         2,
			wantSomewhere: "manifest says",
		},
		{
			name: "a label policy on a path collection",
			detail: `{` + validationLenses + `,"collections":[{"id":9,"title":"Creeks","kind":"path","visible":true,` +
				`"attrs":{"atlas.stroke.width_px":"10","atlas.label.policy":"quiet"},` +
				`"features":[{"id":5,"title":"Big Dry Creek","geometry":[` + line + `]}]}]}`,
			paths:         1,
			wantSomewhere: "label policy",
		},
		{
			name: "a point collection carrying inline features",
			detail: `{` + validationLenses + `,"collections":[{"id":1,"title":"Marker","kind":"point","visible":true,` +
				`"features":[{"id":5,"title":"Stray","geometry":[]}]}]}`,
			wantSomewhere: "inline features",
		},
		{
			name:          "a sound area collection",
			detail:        area(`,"attrs":{"atlas.label.policy":"quiet"}`),
			areas:         1,
			wantSomewhere: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := buildForValidation(t, c.detail, `{}`, 0, c.paths, c.areas).Validate()
			if c.wantSomewhere == "" {
				if err != nil {
					t.Fatalf("a sound bundle is refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantSomewhere) {
				t.Fatalf("validated with %v, want a complaint about %q", err, c.wantSomewhere)
			}
		})
	}
}

// The file name is derived from nothing but the manifest, so the same build
// carries the same name wherever and whenever it is written.
func TestVersionedFileNameIsDeterministic(t *testing.T) {
	manifest := validManifest()
	manifest.Version.CreatedAt = "2026-08-01T09:30:00Z"
	manifest.Version.Stamp = strings.Repeat("ab", 32)
	if got, want := bundle.VersionedFileName(manifest), "fixture-20260801-abababababab.atlas"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestMoreRecentIsTotalAndPrefersNewer(t *testing.T) {
	older := open(t, bundletest.Build(t, t.TempDir(), bundletest.Spec{Slug: "game", CreatedAt: "2026-01-01T00:00:00Z"}))
	newer := open(t, bundletest.Build(t, t.TempDir(), bundletest.Spec{Slug: "game", CreatedAt: "2026-06-01T00:00:00Z"}))

	if !bundle.MoreRecent(newer, older) || bundle.MoreRecent(older, newer) {
		t.Error("creation time does not decide which bundle wins")
	}
	if bundle.MoreRecent(older, older) {
		t.Error("a bundle shadows itself")
	}
}

// Builds of one capture share a creation time; the policy revision decides
// between them, so a rebuild under a newer rule supersedes deterministically
// rather than by whichever stamp happens to sort higher.
func TestMoreRecentPrefersTheNewerRevisionOfOneCapture(t *testing.T) {
	captured := "2026-06-01T00:00:00Z"
	plain := open(t, bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "game", CreatedAt: captured,
		Stamp: strings.Repeat("ff", 32),
	}))
	revised := open(t, bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "game", CreatedAt: captured, Revision: 1,
		Stamp: strings.Repeat("00", 32),
	}))

	if !bundle.MoreRecent(revised, plain) || bundle.MoreRecent(plain, revised) {
		t.Error("the revision does not decide between builds of one capture")
	}

	newerCapture := open(t, bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "game", CreatedAt: "2026-07-01T00:00:00Z",
	}))
	if !bundle.MoreRecent(newerCapture, revised) {
		t.Error("a revision outweighed a newer capture")
	}
}

func open(t *testing.T, path string) *bundle.Bundle {
	t.Helper()
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}

// zipWithout builds a genuine zip holding a single unrelated entry,
// exercising the reader against an archive that is not a bundle.
func zipWithout(t *testing.T, _ string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	entry, err := zw.Create("README.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("just an archive")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
