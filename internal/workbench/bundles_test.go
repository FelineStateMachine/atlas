package workbench

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
)

// The bundles these tests measure.
//
// Two kinds, and the difference is the point. A stated bundle is written here
// from a few lines of Go, so a page test can say exactly what a build holds and
// then read the page back. A fixture bundle is packed out of golden/fixtures --
// the reference implementation's own extraction of a real volume -- so one test
// can hold a page to a score that was captured rather than invented.
//
// Both go through format/bundle's real writer: nothing opaque is checked in,
// and a bundle a test measures is a bundle the application could open.

type bundleSpec struct {
	slug        string
	title       string
	createdAt   string
	stamp       string
	revision    int
	conventions int
	worlds      []worldSpec
}

type worldSpec struct {
	slug     string
	title    string
	icon     string
	attrs    map[string]string
	features []featureSpec
	// merged is the world's ledger, stated as the payload carries it.
	merged []map[string]any
	lenses []map[string]any
}

type featureSpec struct {
	id          int64
	title       string
	description string
}

// write builds one bundle in dir and answers with its path.
func (b bundleSpec) write(t *testing.T, dir string) string {
	t.Helper()
	if b.slug == "" {
		b.slug = "hollowmere"
	}
	if b.title == "" {
		b.title = "Hollowmere"
	}
	if b.createdAt == "" {
		b.createdAt = "2026-01-01T00:00:00Z"
	}
	if b.stamp == "" {
		b.stamp = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	if b.conventions == 0 {
		b.conventions = semconv.Version
	}

	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Conventions:   b.conventions,
		Volume:        bundle.Volume{Slug: b.slug, Title: b.title},
		Version: bundle.Version{
			Stamp:     b.stamp,
			CreatedAt: b.createdAt,
			Revision:  b.revision,
		},
		TileGrid: bundle.TileGrid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192},
	}
	for _, world := range b.worlds {
		manifest.Worlds = append(manifest.Worlds, bundle.WorldEntry{
			Slug:      world.slug,
			Title:     world.titleOr(),
			Points:    len(world.features),
			UpdatedAt: b.createdAt,
		})
	}

	path := filepath.Join(dir, bundle.VersionedFileName(manifest))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, world := range b.worlds {
		payload, text, packed := world.parts(t)
		if err := writer.AddDeflated(bundle.WorldEntryName(world.slug, bundle.WorldSuffix), payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.AddDeflated(bundle.WorldEntryName(world.slug, bundle.TextSuffix), text); err != nil {
			t.Fatal(err)
		}
		if err := writer.AddStored(bundle.WorldEntryName(world.slug, bundle.PackedSuffix), bytes.NewReader(packed)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func (w worldSpec) titleOr() string {
	if w.title != "" {
		return w.title
	}
	return "Overworld"
}

// parts is one world's three payloads: the collections and the ledger, the
// prose, and the packed point features.
func (w worldSpec) parts(t *testing.T) (payload, text, packed []byte) {
	t.Helper()
	attrs := w.attrs
	if attrs == nil {
		attrs = map[string]string{semconv.KeyGeometrySurface: semconv.SurfacePlane}
	}
	lenses := w.lenses
	if lenses == nil {
		lenses = []map[string]any{{"tiles": w.slug, "minZoom": 0, "maxZoom": 2, "formats": []string{"jpg"}}}
	}
	collection := map[string]any{
		"id":    1,
		"title": "Markers",
		"group": "Places",
		"kind":  semconv.GeometryPoint,
		"attrs": map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin},
	}
	if w.icon != "" {
		collection["iconAsset"] = w.icon
	}
	body := map[string]any{
		"attrs":       attrs,
		"lenses":      lenses,
		"collections": []any{collection},
	}
	if w.merged != nil {
		body["merged"] = w.merged
	}
	payload = marshal(t, body)

	prose := map[string]any{}
	locations := make([]bundle.Location, 0, len(w.features))
	for at, feature := range w.features {
		if feature.description != "" {
			prose[strconv.FormatInt(feature.id, 10)] = map[string]any{"d": feature.description}
		}
		locations = append(locations, bundle.Location{
			ID:    feature.id,
			Owner: 0,
			Lat:   float64(at),
			Lng:   float64(at),
			Title: feature.title,
		})
	}
	return payload, marshal(t, prose), bundle.PackLocations(locations)
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// packFixture writes a golden fixture directory out as a real .atlas file.
//
// The fixture is the reference implementation's own extraction of a build
// (golden/fixtures/README.md): its manifest verbatim, and one payload trio per
// world. Packing it back into the container is what lets a page test measure
// the same bytes the enrich lane's own suite scores.
func packFixture(t *testing.T, fixture, dir string) string {
	t.Helper()
	var manifest bundle.Manifest
	readJSON(t, filepath.Join(fixture, "manifest.json"), &manifest)

	path := filepath.Join(dir, bundle.VersionedFileName(manifest))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Worlds {
		payload := read(t, filepath.Join(fixture, "worlds", entry.Slug+".payload.json"))
		text := read(t, filepath.Join(fixture, "worlds", entry.Slug+".text.json"))
		if err := writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix), payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.TextSuffix), text); err != nil {
			t.Fatal(err)
		}
		packed := bundle.PackLocations(fixtureLocations(t, fixture, entry.Slug))
		if err := writer.AddStored(bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix), bytes.NewReader(packed)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureLocations(t *testing.T, fixture, world string) []bundle.Location {
	t.Helper()
	var held struct {
		Locations []struct {
			ID     int64   `json:"id"`
			Owner  uint16  `json:"owner"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Member int64   `json:"member"`
			Shard  int64   `json:"shard"`
			Title  string  `json:"title"`
		} `json:"locations"`
	}
	readJSON(t, filepath.Join(fixture, "worlds", world+".locations.json"), &held)
	out := make([]bundle.Location, 0, len(held.Locations))
	for _, location := range held.Locations {
		out = append(out, bundle.Location{
			ID: location.ID, Owner: location.Owner, Lat: location.Lat, Lng: location.Lng,
			Member: location.Member, Shard: location.Shard, Title: location.Title,
		})
	}
	return out
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	if err := json.Unmarshal(read(t, path), into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
