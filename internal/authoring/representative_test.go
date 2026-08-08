package authoring

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestBuildRepresentativeWorldExercisesTheNativeContract(t *testing.T) {
	projectPath := filepath.Join("..", "..", "examples", "sample-region"+ProjectExtension)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	libraryDir := filepath.Join(t.TempDir(), "library")
	result, err := Build(context.Background(), BuildOptions{
		ProjectPath: projectPath,
		CacheDir:    cacheDir,
		LibraryDir:  libraryDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := vnext.OpenFile(result.Path, vnext.StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	volume, err := reader.Volume()
	if err != nil {
		t.Fatal(err)
	}
	if len(volume.Worlds) != 1 {
		t.Fatalf("world count = %d, want 1", len(volume.Worlds))
	}
	world := volume.Worlds[0]
	if len(world.FeatureSets) != 3 {
		t.Fatalf("feature set count = %d, want point/path/area", len(world.FeatureSets))
	}
	wantKinds := map[vnext.GeometryKind]bool{
		vnext.GeometryPoint:      false,
		vnext.GeometryLineString: false,
		vnext.GeometryPolygon:    false,
	}
	wantPropertyKinds := map[vnext.Kind]bool{
		vnext.KindBool: false, vnext.KindInt64: false, vnext.KindFloat64: false,
		vnext.KindString: false, vnext.KindBytes: false, vnext.KindID: false,
	}
	relationships, provenance := 0, 0
	featureIDs := make(map[string]bool)
	transformed := make(map[vnext.Position]bool)
	for _, set := range world.FeatureSets {
		if len(set.Features) == 0 {
			t.Fatalf("feature set %s is empty", set.ID)
		}
		kind := set.Features[0].Geometry.Kind
		wantKinds[kind] = true
		if !hasGeometryClaim(set.Claims, familyOf(kind)) {
			t.Errorf("feature set %s omits geometry claim %s", set.ID, familyOf(kind))
		}
		for _, feature := range set.Features {
			featureIDs[feature.ID] = true
			if feature.Geometry.Kind != kind {
				t.Fatalf("feature set %s mixes geometry %d and %d", set.ID, kind, feature.Geometry.Kind)
			}
			if len(feature.Provenance) == 0 {
				t.Fatalf("feature %s has no provenance", feature.ID)
			}
			provenance += len(feature.Provenance)
			relationships += len(feature.Relationships)
			if feature.Geometry.Kind == vnext.GeometryPoint {
				transformed[feature.Geometry.Parts[0].Rings[0][0]] = true
			}
			for _, property := range feature.Properties {
				wantPropertyKinds[property.Value.Kind] = true
			}
		}
	}
	for _, position := range []vnext.Position{{40, 60}, {75, 35}} {
		if !transformed[position] {
			t.Errorf("affine transform omits point %v", position)
		}
	}
	for kind, seen := range wantKinds {
		if !seen {
			t.Errorf("geometry kind %d is not represented", kind)
		}
	}
	for kind, seen := range wantPropertyKinds {
		if !seen {
			t.Errorf("property kind %d is not represented", kind)
		}
	}
	if relationships == 0 || provenance < 4 {
		t.Fatalf("relationships=%d provenance=%d, want a related evidence graph", relationships, provenance)
	}
	for _, set := range world.FeatureSets {
		for _, feature := range set.Features {
			for _, relationship := range feature.Relationships {
				if !featureIDs[relationship.Target] {
					t.Errorf("feature %s targets missing %s", feature.ID, relationship.Target)
				}
			}
		}
	}
	if len(world.RasterPyramids) != 1 {
		t.Fatalf("raster pyramid count = %d, want 1", len(world.RasterPyramids))
	}
	raster := world.RasterPyramids[0]
	if raster.MinZoom != 0 || raster.MaxZoom != 1 || raster.Formats[0] != "png" || raster.Formats[1] != "png" {
		t.Fatalf("raster pyramid = %+v, want canonical PNG zooms 0..1", raster)
	}
	blobNames := reader.BlobNames()
	for _, name := range []string{"tiles/background/0/0/0.png", "tiles/background/1/0/0.png", "tiles/background/1/1/1.png"} {
		if !containsString(blobNames, name) {
			t.Errorf("native Atlas omits raster blob %s", name)
		}
	}
	tile, err := reader.Blob("tiles/background/1/1/1.png")
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(tile))
	if err != nil || image.Bounds().Dx() != 32 || image.Bounds().Dy() != 32 {
		t.Fatalf("canonical tile = %v, %v", image, err)
	}
	if len(world.Presentation.Styles) != 3 || len(world.Presentation.Layers) != 3 || len(world.Presentation.Legend) != 3 {
		t.Fatalf("presentation = %d styles, %d layers, %d legend entries", len(world.Presentation.Styles), len(world.Presentation.Layers), len(world.Presentation.Legend))
	}
	assets := make(map[string]vnext.Asset, len(volume.Assets))
	for _, asset := range volume.Assets {
		assets[asset.ID] = asset
	}
	if len(assets["sample-symbol"].Data) == 0 || len(assets["build-receipt"].Data) == 0 {
		t.Fatalf("assets = %v, want authored symbol and build receipt", sortedAssetIDs(assets))
	}
	var receipt struct {
		Captures []struct {
			SHA256 string `json:"sha256"`
		} `json:"captures"`
	}
	if err := json.Unmarshal(assets["build-receipt"].Data, &receipt); err != nil || len(receipt.Captures) != 5 {
		t.Fatalf("build receipt captures = %d, %v", len(receipt.Captures), err)
	}
	firstBytes, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Build(context.Background(), BuildOptions{
		ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: libraryDir, Offline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rebuiltBytes, err := os.ReadFile(rebuilt.Path)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Path != result.Path || !rebuilt.Present || !bytes.Equal(firstBytes, rebuiltBytes) {
		t.Fatalf("offline rebuild differs: first=%s rebuilt=%s present=%v", result.Path, rebuilt.Path, rebuilt.Present)
	}
}

func hasGeometryClaim(properties []vnext.Property, family string) bool {
	for _, property := range properties {
		if property.Field.Name == "atlas.geometry.kind" && property.Value.String == family {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sortedAssetIDs(values map[string]vnext.Asset) string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	return strings.Join(ids, ", ")
}
