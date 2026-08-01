// Package bundletest builds small real bundles for tests. A fixture is
// written through the same Writer the bundler uses, so the writer and reader
// verify each other and nothing opaque is checked into the tree.
package bundletest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

// Spec describes one fixture game. Zero values are filled with something
// plausible, so a test that only cares about, say, the slug states the slug
// and nothing else.
type Spec struct {
	Slug      string
	Title     string
	CreatedAt string
	Stamp     string
	Revision  int
	Maps      []MapSpec
}

// MapSpec describes one map of a fixture game: a single layer two zoom
// levels deep, one category, and the given pins. Merged, when set, rides
// into the payload's merged block verbatim, so a test can hand a map the
// provenance ledger a real composition would have written.
type MapSpec struct {
	Slug   string
	Title  string
	Pins   []Pin
	Merged any
}

// Pin is one fixture location. Description, when set, replaces the
// placeholder note; Silent leaves the pin with no note at all, which is how
// a test states that a pin is undescribed.
type Pin struct {
	Title       string
	Lat         float64
	Lng         float64
	Description string
	Silent      bool
}

// Build writes <spec.Slug>.atlas under dir and returns its path.
func Build(t testing.TB, dir string, spec Spec) string {
	t.Helper()
	if spec.Title == "" {
		spec.Title = spec.Slug
	}
	if spec.CreatedAt == "" {
		spec.CreatedAt = "2026-01-01T00:00:00Z"
	}
	if spec.Stamp == "" {
		spec.Stamp = bundle.HashBytes([]byte(spec.Slug + spec.CreatedAt))
	}
	if len(spec.Maps) == 0 {
		spec.Maps = []MapSpec{{Slug: "overworld", Pins: []Pin{{Title: "Origin"}}}}
	}

	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Game:          bundle.Game{Slug: spec.Slug, Title: spec.Title},
		Version:       bundle.Version{Stamp: spec.Stamp, CreatedAt: spec.CreatedAt, Revision: spec.Revision},
		TileGrid:      bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
	}
	for _, m := range spec.Maps {
		if m.Title == "" {
			m.Title = m.Slug
		}
		manifest.Maps = append(manifest.Maps, bundle.MapEntry{
			Slug:      m.Slug,
			Title:     m.Title,
			PinCount:  len(m.Pins),
			UpdatedAt: spec.CreatedAt,
		})
	}

	path := filepath.Join(dir, spec.Slug+".atlas")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range spec.Maps {
		detail := map[string]any{
			"variants": []map[string]any{{
				"name":       "Default",
				"tiles":      m.Slug,
				"minZoom":    0,
				"maxZoom":    1,
				"fullZoom":   1,
				"sourceZoom": 1,
				"formats":    []string{"jpg", "jpg"},
			}},
			"groups": []map[string]any{{
				"id":    1,
				"title": "Markers",
				"categories": []map[string]any{{
					"id": 1, "title": "Marker", "iconAsset": "marker.svg",
					"displayType": "", "visible": true,
				}},
			}},
		}
		if m.Merged != nil {
			detail["merged"] = m.Merged
		}
		payload, err := json.Marshal(detail)
		if err != nil {
			t.Fatal(err)
		}
		add(t, writer.AddDeflated("maps/"+m.Slug+".json", payload))

		locations := make([]bundle.Location, len(m.Pins))
		text := make(map[string]any)
		for index, pin := range m.Pins {
			locations[index] = bundle.Location{
				ID:    int64(index + 1),
				Title: pin.Title,
				Lat:   pin.Lat,
				Lng:   pin.Lng,
			}
			if pin.Silent {
				continue
			}
			description := pin.Description
			if description == "" {
				description = "About " + pin.Title
			}
			text[strconv.Itoa(index+1)] = map[string]string{"d": description}
		}
		packed := bundle.PackLocations(locations)
		add(t, writer.AddStored("maps/"+m.Slug+".bin", bytes.NewReader(packed)))
		notes, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		add(t, writer.AddDeflated("maps/"+m.Slug+".text", notes))

		for _, tile := range []string{"0/0/0.jpg", "1/0/0.jpg"} {
			raster := []byte("jpeg bytes of " + m.Slug + "/" + tile)
			add(t, writer.AddStored("tiles/"+m.Slug+"/"+tile, bytes.NewReader(raster)))
		}
	}
	add(t, writer.AddDeflated("icons/marker.svg", []byte("<svg/>")))

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func add(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
