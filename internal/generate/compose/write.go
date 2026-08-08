package compose

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func revisionOf(options Options) int {
	if options.Revision != 0 {
		return options.Revision
	}
	return PolicyRevision
}

// write builds exactly one native semantic bundle. The producer never creates
// the retired document-shaped archive, even as an intermediate artifact.
func write(options Options, worlds []composedWorld, icons map[string][]byte, log *slog.Logger) (Result, error) {
	semantic, rasterBlobs, tileCount, capturedAt, err := nativeVolume(options, worlds, icons)
	if err != nil {
		return Result{}, err
	}
	packed, err := vnext.Compile(semantic)
	if err != nil {
		return Result{}, fmt.Errorf("compile native volume: %w", err)
	}
	packed.Release.CreatedAt = capturedAt
	packed.Release.Revision = revisionOf(options)
	packed.Blobs = append(packed.Blobs, rasterBlobs...)

	stageDir := options.BundleDir
	if stageDir == "" {
		stageDir = os.TempDir()
	} else if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return Result{}, err
	}
	file, err := os.CreateTemp(stageDir, semantic.ID+".atlas.tmp-*")
	if err != nil {
		return Result{}, err
	}
	stage := file.Name()
	defer os.Remove(stage)
	if err := vnext.Write(file, packed); err != nil {
		file.Close()
		return Result{}, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return Result{}, err
	}
	if err := file.Close(); err != nil {
		return Result{}, err
	}
	opened, err := vnext.OpenFile(stage, vnext.StandardSchema())
	if err != nil {
		return Result{}, fmt.Errorf("reopen native volume: %w", err)
	}
	if err := opened.Validate(); err != nil {
		opened.Close()
		return Result{}, fmt.Errorf("validate native volume: %w", err)
	}
	descriptor := opened.Descriptor()
	if err := opened.Close(); err != nil {
		return Result{}, err
	}

	result := Result{
		Volume: semantic.ID, Stamp: descriptor.Stamp,
		File:   vnext.VersionedFileName(semantic.ID, descriptorRelease(descriptor)),
		Worlds: len(worlds), Tiles: tileCount, Icons: len(icons),
	}
	if options.BundleDir == "" {
		log.Info("native volume composed", logging.Stamp(vnext.ShortStamp(result.Stamp)),
			"worlds", result.Worlds, "tiles", result.Tiles, "icons", result.Icons)
		return result, nil
	}

	target := filepath.Join(options.BundleDir, result.File)
	result.Path = target
	if _, err := os.Stat(target); err == nil {
		result.Present = true
		log.Info("native build already installed", logging.Stamp(vnext.ShortStamp(result.Stamp)), logging.Path(result.File))
		return result, nil
	}
	if err := os.Chmod(stage, 0o644); err != nil {
		return Result{}, err
	}
	if err := os.Rename(stage, target); err != nil {
		return Result{}, err
	}
	log.Info("native bundle installed", logging.Stamp(vnext.ShortStamp(result.Stamp)),
		logging.Path(result.File), "worlds", result.Worlds, "tiles", result.Tiles, "icons", result.Icons)
	return result, nil
}

func descriptorRelease(descriptor vnext.Descriptor) vnext.Release {
	return vnext.Release{
		Title: descriptor.Title, CreatedAt: descriptor.CreatedAt, Revision: descriptor.Revision,
		Stamp: descriptor.Stamp, Worlds: descriptor.Worlds,
	}
}

// WriteRegistryIndex writes a derived convenience listing. It has no authority:
// deleting it is harmless because native release metadata and the directory
// fold remain the registry.
func WriteRegistryIndex(dir string) error {
	descriptors, _, err := vnext.Scan(dir, vnext.StandardSchema())
	if err != nil {
		return err
	}
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Slug != descriptors[j].Slug {
			return descriptors[i].Slug < descriptors[j].Slug
		}
		return vnext.Newer(descriptors[i], descriptors[j])
	})
	type build struct {
		File      string `json:"file"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
		Stamp     string `json:"stamp"`
		CreatedAt string `json:"createdAt"`
		Revision  int    `json:"revision,omitempty"`
		Size      int64  `json:"size"`
		Worlds    int    `json:"worlds"`
	}
	listed := make([]build, 0, len(descriptors))
	for _, descriptor := range descriptors {
		listed = append(listed, build{
			File: filepath.Base(descriptor.Locator), Slug: descriptor.Slug, Title: descriptor.Title,
			Stamp: descriptor.Stamp, CreatedAt: descriptor.CreatedAt, Revision: descriptor.Revision,
			Size: descriptor.Size, Worlds: descriptor.Worlds,
		})
	}
	data, err := json.Marshal(struct {
		Builds []build `json:"builds"`
	}{Builds: listed})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	stage, err := os.CreateTemp(dir, ".index-*")
	if err != nil {
		return err
	}
	name := stage.Name()
	defer os.Remove(name)
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "index.json"))
}

func rasterBlobList(set *tiles.Set, pyramids map[string]tiles.Pyramid) ([]vnext.Blob, int, error) {
	locals := make([]string, 0, len(pyramids))
	for local := range pyramids {
		locals = append(locals, local)
	}
	sort.Strings(locals)
	var blobs []vnext.Blob
	for _, local := range locals {
		list, err := set.Tiles(pyramids[local])
		if err != nil {
			return nil, 0, err
		}
		for _, tile := range list {
			blobs = append(blobs, vnext.Blob{Name: "tiles/" + local + "/" + tile.Name, Path: tile.Path})
		}
	}
	return blobs, len(blobs), nil
}
