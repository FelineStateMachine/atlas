package app

import (
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestVNextEarthBuildsTheRendererDirectly(t *testing.T) {
	t.Parallel()

	native, err := vnext.OpenFile("../../included/earth-20260803-28305c3d1811.atlas", vnext.StandardSchema())
	if err != nil {
		t.Fatalf("open native Earth: %v", err)
	}
	defer native.Close()
	semantic, err := native.Volume()
	if err != nil {
		t.Fatalf("restore native Earth: %v", err)
	}
	for _, set := range semantic.Worlds[0].FeatureSets {
		if len(set.Features) == 0 {
			continue
		}
		page, err := native.FeaturePage(vnext.FeaturePageRequest{FeatureSet: set.ID, Limit: 1})
		if err != nil || len(page.Features) != 1 || page.PartitionsRead != 1 || page.BytesRead == 0 {
			t.Fatalf("framing-1 feature demand = %#v, %v", page, err)
		}
		break
	}
	summaries, err := native.FeatureSetSummaries()
	if err != nil || len(summaries) == 0 || summaries[0].Rows == 0 {
		t.Fatalf("framing-1 feature summaries = %#v, %v", summaries, err)
	}

	model, err := buildVNextWorld(semantic.Worlds[0], semantic.Assets)
	if err != nil {
		t.Fatalf("build vNext world: %v", err)
	}
	if len(model.Points) != 202 || len(model.Shapes) != 177 || len(model.Members) != 7 || len(model.Lenses) != 1 {
		t.Fatalf("runtime model has %d points, %d shapes, %d collections and %d lenses", len(model.Points), len(model.Shapes), len(model.Members), len(model.Lenses))
	}
	countries := nativeCollection(model, "Countries")
	if countries == nil || len(countries.Shapes) != 177 {
		t.Fatalf("Countries collection = %#v", countries)
	}
	fiji := nativeShape(countries, "Fiji")
	if fiji == nil || len(fiji.Polygons) < 2 || fiji.Feature == nil {
		t.Fatalf("Fiji native shape = %#v", fiji)
	}
	if !strings.Contains(fiji.ID, "/feature/") || countries.ID == "" {
		t.Fatalf("native identities were not preserved: feature=%q layer=%q", fiji.ID, countries.ID)
	}
}

func TestNativeCoordinateLabelsRespectTheDeclaredSpace(t *testing.T) {
	t.Parallel()

	space := vnext.CoordinateSpace{Kind: "projected", Unit: "metre", Extent: [4]float64{0, 0, 100, 100}}
	if got := coordinateLabel(space, tileGrid{}, vnext.Position{40, 60}); got != "40.000000, 60.000000 metre" {
		t.Fatalf("projected coordinate label = %q", got)
	}

	legacy := tileGrid{SourceZoom: 5, FirstTile: 10, TileSize: 256, Size: 8192}
	got := coordinateLabel(vnext.CoordinateSpace{Kind: "projected", Unit: "world-pixel", Definition: "atlas:tile-plane"}, legacy, vnext.Position{4096, 4096})
	if strings.Contains(got, "world-pixel") || strings.Contains(got, "Inf") {
		t.Fatalf("legacy tile-plane label = %q", got)
	}
}

func TestTileGridProjectsIndependentColumnAndRowOrigins(t *testing.T) {
	t.Parallel()

	grid := tileGrid{SourceZoom: 4, OriginX: 3, OriginY: 6, FirstTile: 99, TileSize: 256, Size: 1024}
	latitude, longitude := grid.unproject(0, 0)
	x, y := grid.project(latitude, longitude)
	if x != 0 || y != 0 {
		t.Fatalf("origin round trip = %v,%v, want 0,0", x, y)
	}
}

func TestNativeGridUsesTheAuthoritativeCoordinateExtent(t *testing.T) {
	space := vnext.CoordinateSpace{
		ID: "sample-grid", Kind: "projected", Unit: "metre", Definition: "local sample grid",
		Extent: [4]float64{10, 20, 110, 70}, OriginX: 7, OriginY: 11,
	}
	grid := nativeGrid(space)
	if grid.TileSize != 256 || grid.Size != 100 || grid.OriginX != 7 || grid.OriginY != 11 {
		t.Fatalf("native grid = %+v, want conventional tile step over the coordinate extent", grid)
	}
	space.SourceZoom, space.TileSize, space.Size = 3, 32, 256
	if got := coordinateLabel(space, nativeGrid(space), vnext.Position{40, 60}); got != "40.000000, 60.000000 metre" {
		t.Fatalf("local projected coordinate label = %q", got)
	}
}

func nativeCollection(model *worldModel, title string) *collectionModel {
	for _, collection := range model.Members {
		if collection.Title == title {
			return collection
		}
	}
	return nil
}

func nativeShape(collection *collectionModel, title string) *shapeModel {
	for _, shape := range collection.Shapes {
		if shape.Title == title {
			return shape
		}
	}
	return nil
}
