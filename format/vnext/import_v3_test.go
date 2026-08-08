package vnext

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

func TestImportV3TurnsTheIncludedEarthIntoTheVNextGraph(t *testing.T) {
	t.Parallel()

	legacy, err := bundle.Open(includedEarth(t))
	if err != nil {
		t.Fatalf("open v3 Earth: %v", err)
	}
	defer legacy.Close()

	imported, err := ImportV3(legacy)
	if err != nil {
		t.Fatalf("import v3 Earth: %v", err)
	}
	var archive bytes.Buffer
	if err := Write(&archive, imported); err != nil {
		t.Fatalf("write imported Earth: %v", err)
	}
	reader, err := Open(bytes.NewReader(archive.Bytes()), int64(archive.Len()), StandardSchema())
	if err != nil {
		t.Fatalf("open imported Earth: %v", err)
	}
	volume, err := reader.Volume()
	if err != nil {
		t.Fatalf("restore imported Earth: %v", err)
	}

	if volume.ID != "earth" || len(volume.Worlds) != 1 {
		t.Fatalf("imported volume identity = %s, worlds = %d", volume.ID, len(volume.Worlds))
	}
	world := volume.Worlds[0]
	if world.CoordinateSpace.SourceZoom != 5 || world.CoordinateSpace.Size != 8192 {
		t.Fatalf("coordinate space = %#v", world.CoordinateSpace)
	}
	if len(world.RasterPyramids) != 1 || len(world.RasterPyramids[0].Formats) != 7 {
		t.Fatalf("raster pyramids = %#v", world.RasterPyramids)
	}
	if got, want := featureCount(world), 379; got != want {
		t.Fatalf("features = %d, want %d", got, want)
	}
	if len(world.Presentation.Layers) != len(world.FeatureSets) || len(world.Presentation.Legend) != len(world.FeatureSets) {
		t.Fatalf("presentation did not split every v3 collection: %#v", world.Presentation)
	}

	countries := featureSetByTitle(world, "Countries")
	if countries == nil || len(countries.Features) == 0 {
		t.Fatal("Countries feature set was not imported")
	}
	fiji := featureByTitle(*countries, "Fiji")
	if fiji == nil || fiji.Geometry.Kind != GeometryPolygon || len(fiji.Geometry.Parts) != 3 {
		t.Fatalf("Fiji geometry = %#v", fiji)
	}
	if len(imported.Blobs) < 1000 {
		t.Fatalf("only %d raster/asset blobs crossed the import", len(imported.Blobs))
	}
}

func TestImportV3VolumeStopsAtTheSemanticBoundary(t *testing.T) {
	t.Parallel()

	legacy, err := bundle.Open(includedEarth(t))
	if err != nil {
		t.Fatalf("open v3 Earth: %v", err)
	}
	defer legacy.Close()

	volume, err := ImportV3Volume(legacy.Manifest, legacy.ReadEntry)
	if err != nil {
		t.Fatalf("import semantic Earth: %v", err)
	}
	if volume.ID != "earth" || len(volume.Worlds) != 1 {
		t.Fatalf("semantic volume = %#v", volume)
	}
	if got, want := featureCount(volume.Worlds[0]), 379; got != want {
		t.Fatalf("semantic features = %d, want %d", got, want)
	}
	if len(volume.Assets) != 0 {
		t.Fatalf("semantic-only import loaded %d assets", len(volume.Assets))
	}
}

func includedEarth(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate importer test")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "included", "earth-20260803-76633cb323ec.atlas")
}

func featureCount(world World) int {
	total := 0
	for _, set := range world.FeatureSets {
		total += len(set.Features)
	}
	return total
}

func featureSetByTitle(world World, title string) *FeatureSet {
	for index := range world.FeatureSets {
		if world.FeatureSets[index].Title == title {
			return &world.FeatureSets[index]
		}
	}
	return nil
}

func featureByTitle(set FeatureSet, title string) *Feature {
	for index := range set.Features {
		if set.Features[index].Title == title {
			return &set.Features[index]
		}
	}
	return nil
}
