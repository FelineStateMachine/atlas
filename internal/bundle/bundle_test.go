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
		Game:          bundle.Game{Slug: "fixture", Title: "Fixture"},
		Version:       bundle.Version{Stamp: "abc", CreatedAt: "2026-01-01T00:00:00Z"},
		Maps:          []bundle.MapEntry{{Slug: "overworld", Title: "Overworld"}},
	}
}

func TestManifestValidation(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("a valid manifest is refused: %v", err)
	}
	cases := map[string]func(*bundle.Manifest){
		"wrong format":        func(m *bundle.Manifest) { m.Format = "zip-of-things" },
		"unknown version":     func(m *bundle.Manifest) { m.FormatVersion = 99 },
		"empty game slug":     func(m *bundle.Manifest) { m.Game.Slug = "" },
		"unsafe game slug":    func(m *bundle.Manifest) { m.Game.Slug = "../escape" },
		"uppercase game slug": func(m *bundle.Manifest) { m.Game.Slug = "Fallout76" },
		"untitled game":       func(m *bundle.Manifest) { m.Game.Title = "" },
		"missing stamp":       func(m *bundle.Manifest) { m.Version.Stamp = "" },
		"missing created at":  func(m *bundle.Manifest) { m.Version.CreatedAt = "" },
		"no maps":             func(m *bundle.Manifest) { m.Maps = nil },
		"unsafe map slug":     func(m *bundle.Manifest) { m.Maps[0].Slug = "a/b" },
		"duplicate map slugs": func(m *bundle.Manifest) { m.Maps = append(m.Maps, m.Maps[0]) },
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
	for _, name := range []string{"", "/etc/hosts", "maps/../escape", "tiles/", bundle.ManifestName} {
		if err := writer.AddDeflated(name, []byte("x")); err == nil {
			t.Errorf("entry %q is accepted", name)
		}
	}
	if err := writer.AddDeflated("maps/overworld.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDeflated("maps/overworld.json", []byte("{}")); err == nil {
		t.Error("the same entry is accepted twice")
	}
}

func TestOpenRoundTripsWhatBuildWrites(t *testing.T) {
	path := bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "fixture",
		Maps: []bundletest.MapSpec{{
			Slug: "overworld",
			Pins: []bundletest.Pin{{Title: "Origin", Lat: 12.5, Lng: -3.25}, {Title: "Peak", Lat: 80, Lng: 170}},
		}},
	})
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	if opened.Manifest.Game.Slug != "fixture" {
		t.Errorf("game slug = %q", opened.Manifest.Game.Slug)
	}
	if got := opened.Manifest.Maps[0].PinCount; got != 2 {
		t.Errorf("pin count = %d, want 2", got)
	}
	if err := opened.Validate(); err != nil {
		t.Errorf("a fixture bundle fails validation: %v", err)
	}

	packed, err := opened.ReadEntry("maps/overworld.bin")
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
	manifest.Maps = append(manifest.Maps, bundle.MapEntry{Slug: "underworld", Title: "Underworld"})
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
	must(writer.AddDeflated("maps/overworld.json",
		[]byte(`{"variants":[{"tiles":"overworld","minZoom":0,"maxZoom":0,"formats":["jpg"]}],"groups":[]}`)))
	must(writer.AddStored("maps/overworld.bin", bytes.NewReader(bundle.PackLocations(nil))))
	must(writer.AddDeflated("maps/overworld.text", []byte(`{}`)))
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

func TestValidateRefusesRuntimeURLsAndWrongCounts(t *testing.T) {
	build := func(text string, pins int) *bundle.Bundle {
		t.Helper()
		manifest := validManifest()
		manifest.Maps[0].PinCount = pins
		path := filepath.Join(t.TempDir(), "fixture.atlas")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		writer, err := bundle.NewWriter(file, manifest)
		if err != nil {
			t.Fatal(err)
		}
		steps := []error{
			writer.AddDeflated("maps/overworld.json",
				[]byte(`{"variants":[{"tiles":"overworld","minZoom":0,"maxZoom":0,"formats":["jpg"]}],"groups":[]}`)),
			writer.AddStored("maps/overworld.bin",
				bytes.NewReader(bundle.PackLocations([]bundle.Location{{ID: 1, Title: "Origin"}}))),
			writer.AddDeflated("maps/overworld.text", []byte(text)),
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

	if err := build(`{"1":{"d":"see https://mapgenie.io/x"}}`, 1).Validate(); err == nil ||
		!strings.Contains(err.Error(), "runtime URL") {
		t.Errorf("a payload carrying a live URL validates with %v", err)
	}
	if err := build(`{}`, 7).Validate(); err == nil || !strings.Contains(err.Error(), "7") {
		t.Errorf("a wrong pin count validates with %v", err)
	}
	if err := build(`{}`, 1).Validate(); err != nil {
		t.Errorf("a sound bundle is refused: %v", err)
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
