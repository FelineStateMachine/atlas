package authoring

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

type Event struct {
	Stage    string `json:"stage"`
	Message  string `json:"message"`
	Current  int    `json:"current,omitempty"`
	Total    int    `json:"total,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	Cached   bool   `json:"cached,omitempty"`
	Artifact string `json:"artifact,omitempty"`
}

type BuildOptions struct {
	ProjectPath string
	CacheDir    string
	LibraryDir  string
	Offline     bool
	ReplayPath  string
	PlanOnly    bool
	Event       func(Event)
}

type BuildResult struct {
	Plan       Plan             `json:"plan"`
	Descriptor vnext.Descriptor `json:"descriptor,omitempty"`
	Path       string           `json:"path,omitempty"`
	Present    bool             `json:"present,omitempty"`
}

func Build(ctx context.Context, options BuildOptions) (BuildResult, error) {
	project, err := LoadProject(options.ProjectPath)
	if err != nil {
		return BuildResult{}, err
	}
	cache := OpenCache(options.CacheDir)
	plan, err := PlanProject(project, cache, options.LibraryDir)
	if err != nil {
		return BuildResult{}, err
	}
	emit(options, Event{Stage: "plan", Message: "build planned", Total: len(plan.Requests), Bytes: plan.Estimated})
	result := BuildResult{Plan: plan}
	if options.PlanOnly {
		return result, nil
	}
	var captures map[string]Capture
	if options.ReplayPath != "" {
		selection, err := loadEvidenceSelection(options.ReplayPath)
		if err != nil {
			return result, err
		}
		captures, err = replayCaptures(plan, selection, cache)
		if err != nil {
			return result, err
		}
		for index, planned := range plan.Requests {
			capture := captures[planned.ID]
			emit(options, Event{Stage: "capture", Message: planned.Source, Current: index + 1, Total: len(plan.Requests), Bytes: capture.Body.Length, Cached: true})
		}
	} else {
		captures = make(map[string]Capture, len(plan.Requests))
		acquisitions := make(map[string]Capture, len(plan.Requests))
		for index, planned := range plan.Requests {
			key := requestCacheKey(planned.Request)
			capture, cached := acquisitions[key], true
			if capture.RequestHash == "" {
				var err error
				capture, cached, err = cache.Acquire(ctx, planned.Request, options.Offline)
				if err != nil {
					return result, err
				}
				acquisitions[key] = capture
			}
			captures[planned.ID] = capture
			emit(options, Event{
				Stage: "capture", Message: planned.Source, Current: index + 1,
				Total: len(plan.Requests), Bytes: capture.Body.Length, Cached: cached,
			})
		}
	}
	var observations []observation
	for sourceIndex, source := range project.Sources {
		planned, ok := featurePlan(plan, source.ID)
		if !ok {
			return result, fmt.Errorf("source %s has no planned request", source.ID)
		}
		capture := captures[planned.ID]
		body, err := os.ReadFile(cache.BlobPath(capture.Body))
		if err != nil {
			return result, fmt.Errorf("read captured source %s: %w", source.ID, err)
		}
		items, err := observe(project, sourceIndex, source, capture, body)
		if err != nil {
			return result, err
		}
		observations = append(observations, items...)
		emit(options, Event{Stage: "observe", Message: source.ID, Current: sourceIndex + 1, Total: len(project.Sources)})
	}
	volume, err := assemble(project, observations)
	if err != nil {
		return result, err
	}
	if err := attachAssets(project, plan, captures, cache, &volume); err != nil {
		return result, err
	}
	rasterBlobs, err := attachRasters(project, plan, captures, cache, &volume.Worlds[0])
	if err != nil {
		return result, err
	}
	receipt, createdAt, err := buildReceipt(plan, captures)
	if err != nil {
		return result, err
	}
	volume.Assets = append(volume.Assets, vnext.Asset{
		ID: "build-receipt", MediaType: "application/json", Data: receipt,
		Provenance: project.ID,
	})
	emit(options, Event{Stage: "assemble", Message: "native semantic volume assembled"})
	bundle, err := vnext.Compile(volume)
	if err != nil {
		return result, fmt.Errorf("compile native volume: %w", err)
	}
	bundle.Blobs = append(bundle.Blobs, rasterBlobs...)
	bundle.Release.CreatedAt = createdAt
	bundle.Release.Revision = project.Release.Revision
	if bundle.Release.Revision == 0 {
		bundle.Release.Revision = 1
	}
	descriptor, path, present, err := writeBuild(options.LibraryDir, bundle)
	if err != nil {
		return result, err
	}
	result.Descriptor, result.Path, result.Present = descriptor, path, present
	emit(options, Event{Stage: "publish", Message: "native build installed", Artifact: path})
	return result, nil
}

func emit(options BuildOptions, event Event) {
	if options.Event != nil {
		options.Event(event)
	}
}

func featurePlan(plan Plan, source string) (PlannedRequest, bool) {
	for _, request := range plan.Requests {
		if request.Kind == RequestFeatures && request.Source == source {
			return request, true
		}
	}
	return PlannedRequest{}, false
}

func attachRasters(project Project, plan Plan, captures map[string]Capture, cache *Cache, world *vnext.World) ([]vnext.Blob, error) {
	var blobs []vnext.Blob
	for _, source := range project.Rasters {
		var requests []PlannedRequest
		for _, request := range plan.Requests {
			if request.Kind == RequestRaster && request.Raster == source.ID {
				requests = append(requests, request)
			}
		}
		if len(requests) == 0 {
			return nil, fmt.Errorf("raster %s has no planned requests", source.ID)
		}
		if source.Adapter == "raster-file" {
			capture := captures[requests[0].ID]
			pyramid, derived, err := deriveRasterFile(source, capture, cache)
			if err != nil {
				return nil, err
			}
			pyramid.ID = world.ID + "/raster/" + source.ID
			world.RasterPyramids = append(world.RasterPyramids, pyramid)
			blobs = append(blobs, derived...)
			continue
		}
		sort.Slice(requests, func(i, j int) bool {
			if requests[i].Zoom != requests[j].Zoom {
				return requests[i].Zoom < requests[j].Zoom
			}
			if requests[i].Y != requests[j].Y {
				return requests[i].Y < requests[j].Y
			}
			return requests[i].X < requests[j].X
		})
		minZoom, maxZoom := requests[0].Zoom, requests[len(requests)-1].Zoom
		formats := make([]string, maxZoom-minZoom+1)
		codec := "application/octet-stream"
		for _, request := range requests {
			capture := captures[request.ID]
			format := formatOf(capture.Body.MediaType, request.IdentityLocator)
			if held := formats[request.Zoom-minZoom]; held != "" && held != format {
				return nil, fmt.Errorf("raster %s mixes formats at zoom %d", source.ID, request.Zoom)
			}
			formats[request.Zoom-minZoom] = format
			codec = capture.Body.MediaType
			blobs = append(blobs, vnext.Blob{
				Name: fmt.Sprintf("tiles/%s/%d/%d/%d.%s", source.ID, request.Zoom, request.X, request.Y, format),
				Path: cache.BlobPath(capture.Body),
			})
		}
		for index := range formats {
			if formats[index] == "" {
				return nil, fmt.Errorf("raster %s omits zoom %d", source.ID, int64(index)+minZoom)
			}
		}
		fullZoom := source.FullZoom
		if fullZoom == 0 {
			fullZoom = maxZoom
		}
		world.RasterPyramids = append(world.RasterPyramids, vnext.RasterPyramid{
			ID: world.ID + "/raster/" + source.ID, Name: source.Name, Codec: codec,
			TileSize: source.TileSize, MinZoom: minZoom, MaxZoom: maxZoom, FullZoom: fullZoom,
			SourceZoom: source.SourceZoom, Template: "tiles/" + source.ID + "/{z}/{x}/{y}.{format}",
			Formats: formats, Bounds: nativeRect(source.Bounds), Surface: nativeRect(source.Surface),
			Interpolate: source.Interpolate, Background: source.Background, Shard: source.Shard,
		})
	}
	sort.Slice(world.RasterPyramids, func(i, j int) bool { return world.RasterPyramids[i].ID < world.RasterPyramids[j].ID })
	sort.Slice(blobs, func(i, j int) bool { return blobs[i].Name < blobs[j].Name })
	return blobs, nil
}

func formatOf(mediaType, locator string) string {
	extension := strings.TrimPrefix(filepath.Ext(locator), ".")
	if extension == "" {
		extensions, _ := mime.ExtensionsByType(mediaType)
		if len(extensions) > 0 {
			extension = strings.TrimPrefix(extensions[0], ".")
		}
	}
	if extension == "jpeg" {
		extension = "jpg"
	}
	if extension == "" {
		extension = "bin"
	}
	return strings.ToLower(extension)
}

func writeBuild(library string, bundle vnext.Bundle) (vnext.Descriptor, string, bool, error) {
	if library == "" {
		return vnext.Descriptor{}, "", false, fmt.Errorf("build has no Atlas library")
	}
	if err := os.MkdirAll(library, 0o755); err != nil {
		return vnext.Descriptor{}, "", false, err
	}
	file, err := os.CreateTemp(library, ".building-*.atlas")
	if err != nil {
		return vnext.Descriptor{}, "", false, err
	}
	stage := file.Name()
	defer os.Remove(stage)
	if err := vnext.Write(file, bundle); err != nil {
		file.Close()
		return vnext.Descriptor{}, "", false, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return vnext.Descriptor{}, "", false, err
	}
	if err := file.Close(); err != nil {
		return vnext.Descriptor{}, "", false, err
	}
	opened, err := vnext.OpenFile(stage, vnext.StandardSchema())
	if err != nil {
		return vnext.Descriptor{}, "", false, fmt.Errorf("reopen native build: %w", err)
	}
	if err := opened.Validate(); err != nil {
		opened.Close()
		return vnext.Descriptor{}, "", false, fmt.Errorf("validate native build: %w", err)
	}
	descriptor := opened.Descriptor()
	if err := opened.Close(); err != nil {
		return vnext.Descriptor{}, "", false, err
	}
	target := filepath.Join(library, vnext.VersionedFileName(descriptor.Slug, vnext.Release{
		Title: descriptor.Title, CreatedAt: descriptor.CreatedAt, Revision: descriptor.Revision,
		Stamp: descriptor.Stamp, Worlds: descriptor.Worlds,
	}))
	if _, err := os.Stat(target); err == nil {
		held, err := vnext.Describe(target, vnext.StandardSchema())
		return held, target, true, err
	}
	if err := os.Chmod(stage, 0o644); err != nil {
		return vnext.Descriptor{}, "", false, err
	}
	if err := os.Rename(stage, target); err != nil {
		return vnext.Descriptor{}, "", false, err
	}
	installed, err := vnext.Describe(target, vnext.StandardSchema())
	return installed, target, false, err
}
