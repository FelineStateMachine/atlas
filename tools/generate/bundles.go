package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// policyRevision orders builds of the same capture. The data has not moved
// between them, but what this tool makes of a capture has -- a merge rule, a
// kept field, a dropped category -- and among equal captures the registry
// serves the highest revision. Bump it when a policy change should supersede
// the builds already in every library.
//
//	1  first revisioned builds
//	2  merge resolution: subset names, adoption, one-to-one matches
//	3  origin provenance on every map; overlap merges across map slugs
//	4  semantic conventions: attributes ride every payload
//	5  standard icons resolved for categories that declare one
//	6  geometry declared: spheres say so, pins carry true coordinates
//	7  attribute-level merge resolution; ledgers name canonical source slugs
//	8  zone prose defers to the text payload; zones mark hasText
const policyRevision = 8

// writeBundles packs each game into its own .atlas file, named by game,
// capture day, and stamp. The directory is a registry, not a mirror: a new
// capture lands beside the builds before it rather than over them, nothing
// is pruned, and the newest-wins fold at serving time is what decides which
// version a reader sees.
func writeBundles(out catalog, tiles tileManifest, tilesDir, bundleDir string) error {
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return fmt.Errorf("create bundle directory: %w", err)
	}
	stampByAsset := make(map[string]string, len(tiles.Lenses))
	for _, variant := range tiles.Lenses {
		stampByAsset[variant.AssetPath] = variant.Stamp
	}

	for _, game := range out.Volumes {
		if err := writeVolumeBundle(game, out.TileGrid, stampByAsset, tilesDir, bundleDir); err != nil {
			return fmt.Errorf("%s: %w", game.Title, err)
		}
	}
	return writeRegistryIndex(bundleDir)
}

// volumePayload is one map's three parts, built once and used for both the
// stamp and the archive.
type volumePayload struct {
	slug   string
	detail []byte
	packed []byte
	text   []byte
}

func writeVolumeBundle(
	game catalogVolume,
	grid tileGrid,
	stampByAsset map[string]string,
	tilesDir, bundleDir string,
) error {
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Conventions:   semconv.Version,
		Volume:        bundle.Volume{Slug: game.Slug, Title: game.Title},
		Version:       bundle.Version{Revision: policyRevision},
		TileGrid: bundle.TileGrid{
			SourceZoom: grid.SourceZoom,
			FirstTile:  grid.FirstTile,
			TileSize:   grid.TileSize,
			Size:       grid.Size,
		},
	}

	// Pyramids are shared: several pieces of a split sheet draw from the one
	// raster, so tile sets are collected across maps and written once.
	pyramids := make(map[string]string)
	var payloads []volumePayload
	var stamp bundle.Stamp
	for _, m := range game.Worlds {
		detail, packed, text := buildPayload(m)
		detail.Lenses = append([]lens(nil), detail.Lenses...)
		for index := range detail.Lenses {
			local := localPyramidName(game.Slug, detail.Lenses[index].Tiles)
			pyramids[local] = detail.Lenses[index].Tiles
			detail.Lenses[index].Tiles = local
		}

		detailJSON, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("world %s: marshal payload: %w", m.Slug, err)
		}
		textJSON, err := json.Marshal(text)
		if err != nil {
			return fmt.Errorf("world %s: marshal text: %w", m.Slug, err)
		}
		payloads = append(payloads, volumePayload{slug: m.Slug, detail: detailJSON, packed: packed, text: textJSON})
		stamp.Add("worlds/"+m.Slug+".json", bundle.HashBytes(detailJSON))
		stamp.Add("worlds/"+m.Slug+".bin", bundle.HashBytes(packed))
		stamp.Add("worlds/"+m.Slug+".text", bundle.HashBytes(textJSON))

		manifest.Worlds = append(manifest.Worlds, bundle.WorldEntry{
			Slug:       m.Slug,
			Title:      m.Title,
			Parent:     m.Parent,
			IconOutset: m.IconOutset,
			Center:     bundle.Coordinate{Lat: m.Center.Latitude, Lng: m.Center.Longitude},
			PinCount:   m.PinCount,
			UpdatedAt:  m.UpdatedAt,
		})
	}

	// A pyramid's own stamp already names the source tiles and the tool that
	// derived them, so the tiles weigh into the bundle's stamp without being
	// read until the bundle is actually written.
	for local, asset := range pyramids {
		stamp.Add("tiles/"+local, stampByAsset[asset])
	}
	iconNames := make([]string, 0, len(game.Icons))
	for name, data := range game.Icons {
		iconNames = append(iconNames, name)
		stamp.Add("icons/"+name, bundle.HashBytes(data))
	}
	sort.Strings(iconNames)
	identity, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	stamp.Add(bundle.ManifestName, bundle.HashBytes(identity))
	manifest.Version.Stamp = stamp.Sum()

	// The version is the capture, not the build: the newest capture time
	// across the game's maps. Building the same archive anywhere, any time,
	// yields the same version and the same file name, which is what lets a
	// directory of these files stand as a registry.
	for _, m := range game.Worlds {
		if m.UpdatedAt > manifest.Version.CreatedAt {
			manifest.Version.CreatedAt = m.UpdatedAt
		}
	}
	if manifest.Version.CreatedAt == "" {
		return fmt.Errorf("no world carries a capture time to version the bundle by")
	}

	// This exact build already in the registry keeps its file untouched.
	target := filepath.Join(bundleDir, bundle.VersionedFileName(manifest))
	if _, err := os.Stat(target); err == nil {
		return nil
	}

	file, err := os.CreateTemp(bundleDir, game.Slug+".atlas.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		return err
	}
	for _, payload := range payloads {
		if err := writer.AddDeflated("worlds/"+payload.slug+".json", payload.detail); err != nil {
			return err
		}
		if err := writer.AddStored("worlds/"+payload.slug+".bin", bytes.NewReader(payload.packed)); err != nil {
			return err
		}
		if err := writer.AddDeflated("worlds/"+payload.slug+".text", payload.text); err != nil {
			return err
		}
	}
	locals := make([]string, 0, len(pyramids))
	for local := range pyramids {
		locals = append(locals, local)
	}
	sort.Strings(locals)
	for _, local := range locals {
		if err := addPyramid(writer, filepath.Join(tilesDir, pyramids[local]), local); err != nil {
			return err
		}
	}
	for _, name := range iconNames {
		if err := writer.AddDeflated("icons/"+name, game.Icons[name]); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// CreateTemp opens private files; a bundle is an artifact anyone may copy.
	if err := os.Chmod(file.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), target); err != nil {
		return err
	}

	// What the manifest promises is checked the moment the bundle exists, so
	// a bad bundle fails this build rather than someone's import.
	written, err := bundle.Open(target)
	if err != nil {
		return err
	}
	defer written.Close()
	if err := written.Validate(); err != nil {
		return fmt.Errorf("validate %s: %w", filepath.Base(target), err)
	}
	fmt.Printf("bundled %s (%d worlds)\n", filepath.Base(target), len(game.Worlds))
	return nil
}

// registryVersion is one build of one volume as the registry index lists it.
type registryVersion struct {
	File      string `json:"file"`
	Stamp     string `json:"stamp"`
	CreatedAt string `json:"createdAt"`
	Revision  int    `json:"revision,omitempty"`
	Size      int64  `json:"size"`
	Maps      int    `json:"maps"`
}

type registryEntry struct {
	Slug     string            `json:"slug"`
	Title    string            `json:"title"`
	Versions []registryVersion `json:"versions"`
}

// writeRegistryIndex reads every bundle in the directory back and writes
// index.json beside them: each game, each of its builds, newest first. The
// index is derived from the files it sits with, never the other way around,
// so a hand-copied or hand-deleted bundle is reflected by the next run
// rather than contradicted by a stale listing.
func writeRegistryIndex(bundleDir string) error {
	paths, err := filepath.Glob(filepath.Join(bundleDir, "*.atlas"))
	if err != nil {
		return err
	}
	entries := make(map[string]*registryEntry)
	newest := make(map[string]string)
	for _, path := range paths {
		opened, err := bundle.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate: index: skipping %s: %v\n", filepath.Base(path), err)
			continue
		}
		manifest := opened.Manifest
		opened.Close()
		var size int64
		if info, err := os.Stat(path); err == nil {
			size = info.Size()
		}
		entry := entries[manifest.Volume.Slug]
		if entry == nil {
			entry = &registryEntry{Slug: manifest.Volume.Slug}
			entries[manifest.Volume.Slug] = entry
		}
		// The newest version's title speaks for the game.
		if manifest.Version.CreatedAt >= newest[manifest.Volume.Slug] {
			newest[manifest.Volume.Slug] = manifest.Version.CreatedAt
			entry.Title = manifest.Volume.Title
		}
		entry.Versions = append(entry.Versions, registryVersion{
			File:      filepath.Base(path),
			Stamp:     manifest.Version.Stamp,
			CreatedAt: manifest.Version.CreatedAt,
			Revision:  manifest.Version.Revision,
			Size:      size,
			Maps:      len(manifest.Worlds),
		})
	}

	listed := make([]registryEntry, 0, len(entries))
	for _, entry := range entries {
		sort.Slice(entry.Versions, func(i, j int) bool {
			if entry.Versions[i].CreatedAt != entry.Versions[j].CreatedAt {
				return entry.Versions[i].CreatedAt > entry.Versions[j].CreatedAt
			}
			if entry.Versions[i].Revision != entry.Versions[j].Revision {
				return entry.Versions[i].Revision > entry.Versions[j].Revision
			}
			return entry.Versions[i].Stamp > entry.Versions[j].Stamp
		})
		listed = append(listed, *entry)
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Title < listed[j].Title })

	data, err := json.Marshal(struct {
		Games []registryEntry `json:"games"`
	}{Games: listed})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleDir, "index.json"), append(data, '\n'), 0o644)
}

// localPyramidName strips the game the pyramid belonged to in the shared tile
// tree. Inside a bundle the game is the file itself, so its pyramids are
// named by map and layer alone.
func localPyramidName(gameSlug, assetPath string) string {
	return strings.TrimPrefix(assetPath, gameSlug+"__")
}

// addPyramid streams one tile pyramid off disk into the bundle, stored: the
// rasters are already compressed, and a stored entry is served as a straight
// byte range.
func addPyramid(writer *bundle.Writer, pyramidDir, local string) error {
	return filepath.WalkDir(pyramidDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(pyramidDir, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		return writer.AddStored("tiles/"+local+"/"+filepath.ToSlash(relative), file)
	})
}
