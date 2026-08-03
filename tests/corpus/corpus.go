// Package corpus packs the committed corpus extractions back into real
// .atlas files.
//
// testdata/corpus/bundles/<slug>/ holds a JSON extraction of one build: the
// manifest, and one payload trio per world. Packing an extraction through
// format/bundle's real writer is what lets a test — or the e2e registry —
// serve real volumes without a single opaque byte in the repository. No
// rasters are committed, so a packed bundle carries every payload and no
// tiles; a page over one draws its chrome, its features and its island, and
// the map answers 404 for imagery, which the renderer is built to survive.
package corpus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// Root answers the corpus bundles directory, relative to some caller-known
// repository root.
func Root(repo string) string {
	return filepath.Join(repo, "testdata", "corpus", "bundles")
}

// Slugs lists the extracted volumes, which is the corpus contract: public
// provenance only, so the list is short and worth reading.
func Slugs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var slugs []string
	for _, entry := range entries {
		if entry.IsDir() {
			slugs = append(slugs, entry.Name())
		}
	}
	return slugs, nil
}

// Pack writes one extraction out as a real .atlas file in dir and answers
// with its path.
func Pack(fixture, dir string) (string, error) {
	var manifest bundle.Manifest
	if err := readJSON(filepath.Join(fixture, "manifest.json"), &manifest); err != nil {
		return "", err
	}

	path := filepath.Join(dir, bundle.VersionedFileName(manifest))
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		return "", err
	}
	for _, entry := range manifest.Worlds {
		payload, err := os.ReadFile(filepath.Join(fixture, "worlds", entry.Slug+".payload.json"))
		if err != nil {
			return "", err
		}
		text, err := os.ReadFile(filepath.Join(fixture, "worlds", entry.Slug+".text.json"))
		if err != nil {
			return "", err
		}
		if err := writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix), payload); err != nil {
			return "", err
		}
		if err := writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.TextSuffix), text); err != nil {
			return "", err
		}
		locations, err := readLocations(filepath.Join(fixture, "worlds", entry.Slug+".locations.json"))
		if err != nil {
			return "", err
		}
		packed := bundle.PackLocations(locations)
		if err := writer.AddStored(bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix), bytes.NewReader(packed)); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func readLocations(path string) ([]bundle.Location, error) {
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
	if err := readJSON(path, &held); err != nil {
		return nil, err
	}
	out := make([]bundle.Location, 0, len(held.Locations))
	for _, location := range held.Locations {
		out = append(out, bundle.Location{
			ID: location.ID, Owner: location.Owner, Lat: location.Lat, Lng: location.Lng,
			Member: location.Member, Shard: location.Shard, Title: location.Title,
		})
	}
	return out, nil
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
