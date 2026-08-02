package bundle_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
)

// A fixture is a real bundle written through the real Writer, so the writer
// and the reader verify each other and nothing opaque is checked into the
// tree. Every field a test does not state is filled with something plausible.

const (
	// fixtureLenses is the one lens every fixture world is pictured by: a
	// single level, one format, drawing from a pyramid named for the world.
	fixtureLenses = `"lenses":[{"tiles":"overworld","minZoom":0,"maxZoom":0,"formats":["jpg"]}]`
	// fixturePointCollection is the simplest sound collections array: one
	// point collection, which the packed payload's owner column indexes.
	fixturePointCollection = `"collections":[{"id":1,"title":"Marker","kind":"point","visible":true}]`
)

type fixture struct {
	slug        string
	title       string
	createdAt   string
	stamp       string
	revision    int
	conventions int
	worlds      []fixtureWorld
	icons       []string
	// omitTiles leaves the tile pyramid out, so a test can break the promise
	// that a lens's levels hold tiles.
	omitTiles bool
}

type fixtureWorld struct {
	slug      string
	detail    string
	text      string
	locations []bundle.Location
	// points, paths, and areas are what the manifest promises. Left zero,
	// points follows the locations actually packed.
	points, paths, areas int
	countsStated         bool
}

func (f fixture) build(t *testing.T, dir string) string {
	t.Helper()
	if f.slug == "" {
		f.slug = "fixture"
	}
	if f.title == "" {
		f.title = "Fixture"
	}
	if f.createdAt == "" {
		f.createdAt = "2026-01-01T00:00:00Z"
	}
	if f.stamp == "" {
		f.stamp = bundle.HashBytes([]byte(f.slug + f.createdAt))
	}
	if len(f.worlds) == 0 {
		f.worlds = []fixtureWorld{{
			slug:      "overworld",
			locations: []bundle.Location{{ID: 1, Title: "Origin"}},
		}}
	}

	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Conventions:   f.conventions,
		Volume:        bundle.Volume{Slug: f.slug, Title: f.title},
		Version:       bundle.Version{Stamp: f.stamp, CreatedAt: f.createdAt, Revision: f.revision},
		TileGrid:      bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
	}
	for _, world := range f.worlds {
		points := world.points
		if !world.countsStated {
			points = len(world.locations)
		}
		manifest.Worlds = append(manifest.Worlds, bundle.WorldEntry{
			Slug:      world.slug,
			Title:     world.slug,
			Points:    points,
			Paths:     world.paths,
			Areas:     world.areas,
			UpdatedAt: f.createdAt,
		})
	}

	path := filepath.Join(dir, f.slug+bundle.Extension)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}

	for _, world := range f.worlds {
		detail := world.detail
		if detail == "" {
			detail = "{" + fixtureLenses + "," + fixturePointCollection + "}"
		}
		text := world.text
		if text == "" {
			text = "{}"
		}
		must(t, writer.AddDeflated(bundle.WorldEntryName(world.slug, bundle.WorldSuffix), []byte(detail)))
		must(t, writer.AddStored(bundle.WorldEntryName(world.slug, bundle.PackedSuffix),
			bytes.NewReader(bundle.PackLocations(world.locations))))
		must(t, writer.AddDeflated(bundle.WorldEntryName(world.slug, bundle.TextSuffix), []byte(text)))
	}
	// One shared pyramid: every fixture lens draws from "overworld", the way
	// several pieces of a split sheet draw from one raster.
	if !f.omitTiles {
		must(t, writer.AddStored(bundle.TilesPrefix+"overworld/0/0/0.jpg",
			bytes.NewReader([]byte("raster"))))
	}
	for _, icon := range f.icons {
		must(t, writer.AddDeflated(bundle.IconsPrefix+icon, []byte("<svg/>")))
	}
	must(t, writer.Close())
	must(t, file.Close())
	return path
}

func (f fixture) open(t *testing.T, dir string) *bundle.Reader {
	t.Helper()
	reader, err := bundle.Open(f.build(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	return reader
}

// conventional turns a fixture into one that declares the current
// conventions, which is what makes validation strict about its attributes.
func (f fixture) conventional() fixture {
	f.conventions = semconv.Version
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
