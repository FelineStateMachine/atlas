package compose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// revisionOf is the revision this build carries: the caller's, where it has one
// to state, and this lane's own policy revision otherwise.
func revisionOf(o Options) int {
	if o.Revision != 0 {
		return o.Revision
	}
	return PolicyRevision
}

// payloadParts is one world's three payloads, built and hashed before anything
// is written: the stamp has to be known before the file can be named, and the
// file cannot be named before it is opened.
type payloadParts struct {
	slug   string
	detail []byte
	packed []byte
	text   []byte
}

// write stamps the volume, decides whether it needs writing at all, and writes
// it if it does.
//
// The stamp is taken over named parts rather than over the finished archive, so
// tile pyramids weigh into it through their derivation stamps without a single
// raster being read. The manifest is hashed as it stands before its own stamp
// and creation time are filled in -- a stamp cannot cover itself -- and the
// revision is included, so a policy change supersedes the builds already in
// every library.
func write(o Options, worlds []composedWorld, icons map[string][]byte, log *slog.Logger) (Result, error) {
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Conventions:   semconv.Version,
		Volume: bundle.Volume{
			Slug:  o.Document.Volume.Slug,
			Title: o.Document.Volume.Title,
		},
		Version: bundle.Version{Revision: revisionOf(o)},
		TileGrid: bundle.TileGrid{
			SourceZoom: o.Curation.Window.SourceZoom,
			FirstTile:  o.Curation.Window.FirstTile,
			TileSize:   o.Tiles.TileSize,
			Size:       o.Tiles.Size,
		},
	}

	var stamp bundle.Stamp
	var parts []payloadParts
	// pyramids maps a bundle-local pyramid name to the tile set's own name for
	// it. Several worlds may draw from one pyramid -- pieces of a split sheet do
	// -- so it is a map, and the pyramid travels once.
	pyramids := make(map[string]tiles.Pyramid)

	for _, world := range worlds {
		payload, packed, text := buildPayload(world)
		for index, lens := range payload.Lenses {
			pyramids[lens.Tiles] = world.Pyramids[index]
		}
		detail, err := json.Marshal(payload)
		if err != nil {
			return Result{}, fmt.Errorf("world %s: marshal payload: %w", world.Slug, err)
		}
		textJSON, err := json.Marshal(text)
		if err != nil {
			return Result{}, fmt.Errorf("world %s: marshal text: %w", world.Slug, err)
		}
		parts = append(parts, payloadParts{slug: world.Slug, detail: detail, packed: packed, text: textJSON})
		stamp.Add(bundle.WorldEntryName(world.Slug, bundle.WorldSuffix), bundle.HashBytes(detail))
		stamp.Add(bundle.WorldEntryName(world.Slug, bundle.PackedSuffix), bundle.HashBytes(packed))
		stamp.Add(bundle.WorldEntryName(world.Slug, bundle.TextSuffix), bundle.HashBytes(textJSON))

		count := tally(world.Collections)
		manifest.Worlds = append(manifest.Worlds, bundle.WorldEntry{
			Slug:       world.Slug,
			Title:      world.Title,
			Parent:     world.Parent,
			IconOutset: world.IconOutset,
			Center:     bundle.Coordinate{Lat: world.Center.Lat, Lng: world.Center.Lng},
			Points:     count.Point,
			Paths:      count.Path,
			Areas:      count.Area,
			UpdatedAt:  world.CapturedAt,
		})
	}

	// A pyramid's own stamp already names the source tiles and the tool that
	// derived them, so the tiles weigh into the bundle's stamp without being
	// read until the bundle is actually written.
	for local, pyramid := range pyramids {
		stamp.Add(tiles.StampPart(local), pyramid.Stamp)
	}
	iconNames := make([]string, 0, len(icons))
	for name, data := range icons {
		iconNames = append(iconNames, name)
		stamp.Add("icons/"+name, bundle.HashBytes(data))
	}
	sort.Strings(iconNames)

	identity, err := bundle.MarshalManifest(manifest)
	if err != nil {
		return Result{}, err
	}
	stamp.Add(bundle.ManifestName, bundle.HashBytes(identity))
	manifest.Version.Stamp = stamp.Sum()

	// The version is the capture, not the build: the newest capture time across
	// the volume's worlds. Building the same archive anywhere, at any time,
	// yields the same version and the same file name, which is what lets a
	// directory of these files stand as a registry.
	for _, world := range worlds {
		if world.CapturedAt > manifest.Version.CreatedAt {
			manifest.Version.CreatedAt = world.CapturedAt
		}
	}
	if manifest.Version.CreatedAt == "" {
		return Result{}, fmt.Errorf("no world carries a capture time to version the bundle by")
	}

	tileCount := 0
	for _, pyramid := range pyramids {
		list, err := o.Tiles.Tiles(pyramid)
		if err != nil {
			return Result{}, err
		}
		tileCount += len(list)
	}
	result := Result{
		Volume: manifest.Volume.Slug,
		Stamp:  manifest.Version.Stamp,
		File:   bundle.VersionedFileName(manifest),
		Worlds: len(worlds),
		Tiles:  tileCount,
		Icons:  len(iconNames),
	}
	if o.BundleDir == "" {
		log.Info("volume composed", logging.Stamp(bundle.ShortStamp(result.Stamp)),
			"worlds", result.Worlds, "tiles", result.Tiles, "icons", result.Icons)
		return result, nil
	}

	if err := os.MkdirAll(o.BundleDir, 0o755); err != nil {
		return Result{}, err
	}
	target := filepath.Join(o.BundleDir, result.File)
	result.Path = target
	// The name carries the stamp, so a file of that name is that build. There
	// is nothing to write and nothing to check.
	if _, err := os.Stat(target); err == nil {
		result.Present = true
		log.Info("build already installed", logging.Stamp(bundle.ShortStamp(result.Stamp)),
			logging.Path(result.File))
		return result, nil
	}
	if err := install(o, manifest, parts, pyramids, icons, iconNames, target); err != nil {
		return Result{}, err
	}
	log.Info("bundle installed", logging.Stamp(bundle.ShortStamp(result.Stamp)),
		logging.Path(result.File), "worlds", result.Worlds,
		"tiles", result.Tiles, "icons", result.Icons)
	return result, nil
}

// install writes the bundle beside the registry it belongs to and then proves
// it.
//
// The sequence is deliberate. The archive is built under a temporary name that
// does not end in the bundle extension, so a reader scanning the registry
// mid-write passes over it; it is renamed into place, so the name appears
// whole or not at all; and then it is reopened from disk and validated, because
// the file that will serve is the copy, and it is the copy's promises that
// matter. A bundle that fails that check fails this build rather than somebody's
// import.
func install(
	o Options,
	manifest bundle.Manifest,
	parts []payloadParts,
	pyramids map[string]tiles.Pyramid,
	icons map[string][]byte,
	iconNames []string,
	target string,
) error {
	file, err := os.CreateTemp(o.BundleDir, manifest.Volume.Slug+".atlas.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if err := writer.AddDeflated(bundle.WorldEntryName(part.slug, bundle.WorldSuffix), part.detail); err != nil {
			return err
		}
		if err := writer.AddStored(bundle.WorldEntryName(part.slug, bundle.PackedSuffix), bytes.NewReader(part.packed)); err != nil {
			return err
		}
		if err := writer.AddDeflated(bundle.WorldEntryName(part.slug, bundle.TextSuffix), part.text); err != nil {
			return err
		}
	}
	locals := make([]string, 0, len(pyramids))
	for local := range pyramids {
		locals = append(locals, local)
	}
	sort.Strings(locals)
	for _, local := range locals {
		if err := addPyramid(writer, o.Tiles, pyramids[local], local); err != nil {
			return err
		}
	}
	for _, name := range iconNames {
		if err := writer.AddDeflated("icons/"+name, icons[name]); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// CreateTemp opens private files; a bundle is an artefact anyone may copy.
	if err := os.Chmod(file.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), target); err != nil {
		return err
	}

	written, err := bundle.Open(target)
	if err != nil {
		return fmt.Errorf("reopen %s: %w", filepath.Base(target), err)
	}
	defer written.Close()
	if err := written.Validate(); err != nil {
		return fmt.Errorf("validate %s: %w", filepath.Base(target), err)
	}
	return nil
}

// addPyramid copies one derived pyramid's rasters into the bundle, stored
// uncompressed so a reader serves them as straight byte ranges.
func addPyramid(writer *bundle.Writer, set *tiles.Set, pyramid tiles.Pyramid, local string) error {
	list, err := set.Tiles(pyramid)
	if err != nil {
		return err
	}
	for _, tile := range list {
		file, err := os.Open(tile.Path)
		if err != nil {
			return err
		}
		err = writer.AddStored("tiles/"+local+"/"+tile.Name, file)
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// WriteRegistryIndex derives the registry listing from the files beside it. It
// is always safe to delete: the next scan derives it again.
func WriteRegistryIndex(dir string) error {
	candidates, skipped, err := bundle.Scan(dir)
	if err != nil {
		return err
	}
	_ = skipped
	return bundle.WriteIndex(dir, candidates)
}
