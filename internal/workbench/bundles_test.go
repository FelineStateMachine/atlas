package workbench

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
)

// The native volumes these tests measure are stated here, then compiled by
// the same vNext writer used by production.
type bundleSpec struct {
	slug, title, createdAt, stamp string
	revision, conventions         int
	worlds                        []worldSpec
}

type worldSpec struct {
	slug, title, icon string
	attrs             map[string]string
	features          []featureSpec
	merged            []map[string]any
	lenses            []map[string]any
}

type featureSpec struct {
	id          int64
	title       string
	description string
}

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
		b.stamp = strings.Repeat("0", 64)
	}
	volume := vnext.Volume{ID: b.slug, Title: b.title}
	for _, world := range b.worlds {
		volume.Worlds = append(volume.Worlds, world.native())
		if world.icon != "" {
			volume.Assets = append(volume.Assets, vnext.Asset{ID: world.icon, MediaType: "image/png", Data: []byte("icon")})
		}
	}
	compiled, err := vnext.Compile(volume)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Release.CreatedAt = b.createdAt
	compiled.Release.Revision = b.revision
	path := filepath.Join(dir, b.slug+"-"+vnext.ShortStamp(b.stamp)+vnext.Extension)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := vnext.Write(file, compiled); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func (w worldSpec) native() vnext.World {
	title := w.title
	if title == "" {
		title = "Overworld"
	}
	attrs := w.attrs
	if attrs == nil {
		attrs = map[string]string{semconv.KeyGeometrySurface: semconv.SurfacePlane}
	}
	setID := w.slug + "/set/markers"
	styleID := w.slug + "/style/markers"
	layerID := w.slug + "/layer/markers"
	set := vnext.FeatureSet{ID: setID, Title: "Markers", SemanticType: "geometry.point"}
	sources := []string{"Test Origin"}
	for _, account := range w.merged {
		if source, ok := account["source"].(string); ok && source != "" {
			sources = append(sources, source)
		}
	}
	for index, item := range w.features {
		feature := vnext.Feature{
			ID: w.slug + "/feature/" + strconv.FormatInt(item.id, 10), Title: item.title, Description: item.description,
			Geometry: vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{float64(index), float64(index)}}}}}},
		}
		for _, source := range sources {
			feature.Provenance = append(feature.Provenance, vnext.Provenance{Source: source, NativeID: strconv.FormatInt(item.id, 10)})
		}
		set.Features = append(set.Features, feature)
	}
	world := vnext.World{
		ID: w.slug, Title: title,
		CoordinateSpace: vnext.CoordinateSpace{ID: w.slug + "/space", Kind: "projected", Unit: "pixel", Definition: "atlas:tile-plane", Extent: [4]float64{0, 0, 8192, 8192}, SourceZoom: 5, TileSize: 256, Size: 8192},
		Claims:          testProperties("world", attrs), FeatureSets: []vnext.FeatureSet{set},
		Presentation: vnext.Presentation{ID: w.slug + "/presentation", Title: title},
	}
	world.Presentation.Styles = []vnext.Style{{ID: styleID, IconAsset: w.icon, RenderAs: semconv.RenderAsPin}}
	world.Presentation.Layers = []vnext.Layer{{ID: layerID, FeatureSet: setID, Style: styleID, Group: "Places", Visible: true, MaxZoom: 32}}
	world.Presentation.Legend = []vnext.LegendEntry{{Layer: layerID, Label: "Markers"}}
	world.RasterPyramids = []vnext.RasterPyramid{{ID: w.slug + "/raster/0", Name: "Surface", Codec: "image/jpeg", TileSize: 256, MinZoom: 0, MaxZoom: 2, Formats: []string{"jpg", "jpg", "jpg"}, Template: "tiles/" + w.slug + "/{z}/{x}/{y}.{format}"}}
	return world
}

func testProperties(owner string, attrs map[string]string) []vnext.Property {
	out := make([]vnext.Property, 0, len(attrs))
	for key, value := range attrs {
		out = append(out, vnext.Property{Field: vnext.Field{ID: vnext.CoreID(owner + ".test." + key), Name: key, Kind: vnext.KindString, Optional: true}, Value: vnext.StringValue(value)})
	}
	return out
}
