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
	"time"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

// writeBundles packs each game into its own .atlas file. A bundle is the
// game complete -- payloads, tiles, icons -- named by nothing but its own
// slug, so the file can travel on its own and land in any Atlas.
func writeBundles(out catalog, tiles tileManifest, tilesDir, bundleDir string) error {
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return fmt.Errorf("create bundle directory: %w", err)
	}
	stampByAsset := make(map[string]string, len(tiles.Variants))
	for _, variant := range tiles.Variants {
		stampByAsset[variant.AssetPath] = variant.Stamp
	}

	kept := make(map[string]bool, len(out.Games))
	for _, game := range out.Games {
		kept[game.Slug+".atlas"] = true
		if err := writeGameBundle(game, out.TileGrid, stampByAsset, tilesDir, bundleDir); err != nil {
			return fmt.Errorf("%s: %w", game.Title, err)
		}
	}

	// A game that has left the catalog leaves the directory too, the same way
	// tools/tiles prunes pyramids: what the source no longer names is not
	// kept around to shadow anything.
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || kept[name] || !strings.HasSuffix(name, ".atlas") {
			continue
		}
		if err := os.Remove(filepath.Join(bundleDir, name)); err != nil {
			return fmt.Errorf("prune %s: %w", name, err)
		}
	}
	return nil
}

// gamePayload is one map's three parts, built once and used for both the
// stamp and the archive.
type gamePayload struct {
	slug   string
	detail []byte
	packed []byte
	text   []byte
}

func writeGameBundle(
	game catalogGame,
	grid tileGrid,
	stampByAsset map[string]string,
	tilesDir, bundleDir string,
) error {
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Game:          bundle.Game{Slug: game.Slug, Title: game.Title},
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
	var payloads []gamePayload
	var stamp bundle.Stamp
	for _, m := range game.Maps {
		detail, packed, text := buildPayload(m)
		detail.Variants = append([]variant(nil), detail.Variants...)
		for index := range detail.Variants {
			local := localPyramidName(game.Slug, detail.Variants[index].Tiles)
			pyramids[local] = detail.Variants[index].Tiles
			detail.Variants[index].Tiles = local
		}
		for groupIndex := range detail.Groups {
			categories := detail.Groups[groupIndex].Categories
			for index := range categories {
				categories[index].IconAsset = strings.TrimPrefix(categories[index].IconAsset, game.Slug+"/")
			}
		}

		detailJSON, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("map %s: marshal payload: %w", m.Slug, err)
		}
		textJSON, err := json.Marshal(text)
		if err != nil {
			return fmt.Errorf("map %s: marshal text: %w", m.Slug, err)
		}
		payloads = append(payloads, gamePayload{slug: m.Slug, detail: detailJSON, packed: packed, text: textJSON})
		stamp.Add("maps/"+m.Slug+".json", bundle.HashBytes(detailJSON))
		stamp.Add("maps/"+m.Slug+".bin", bundle.HashBytes(packed))
		stamp.Add("maps/"+m.Slug+".text", bundle.HashBytes(textJSON))

		manifest.Maps = append(manifest.Maps, bundle.MapEntry{
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

	// An unchanged game keeps its file, and with it its creation time, so a
	// rebuild of everything does not read as an update of everything.
	target := filepath.Join(bundleDir, game.Slug+".atlas")
	if existing, err := bundle.Open(target); err == nil {
		carried := existing.Manifest.Version.Stamp
		existing.Close()
		if carried == manifest.Version.Stamp {
			return nil
		}
	}
	manifest.Version.CreatedAt = time.Now().UTC().Format(time.RFC3339)

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
		if err := writer.AddDeflated("maps/"+payload.slug+".json", payload.detail); err != nil {
			return err
		}
		if err := writer.AddStored("maps/"+payload.slug+".bin", bytes.NewReader(payload.packed)); err != nil {
			return err
		}
		if err := writer.AddDeflated("maps/"+payload.slug+".text", payload.text); err != nil {
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
	fmt.Printf("bundled %s (%d maps)\n", filepath.Base(target), len(game.Maps))
	return nil
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
