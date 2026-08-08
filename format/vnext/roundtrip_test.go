package vnext

import (
	"bytes"
	"testing"
)

func TestVolumeTravelsFromDomainThroughBundleToPresentation(t *testing.T) {
	t.Parallel()

	emergency := Field{
		ID:       IDFromName("com.example.emergency", "feature.level"),
		Name:     "emergencyLevel",
		Kind:     KindInt64,
		Optional: true,
	}
	volume := Volume{
		ID:    "bend-or",
		Title: "Bend, Oregon",
		Worlds: []World{{
			ID:    "2026-08-01",
			Title: "2026-08-01",
			CoordinateSpace: CoordinateSpace{
				ID:         "bend-web-mercator",
				Kind:       "projected",
				Unit:       "metre",
				Definition: "EPSG:3857",
				Extent:     [4]float64{-13500000, 5400000, -13400000, 5500000},
				SourceZoom: 13,
				TileSize:   256,
				Size:       8192,
			},
			FeatureSets: []FeatureSet{{
				ID:           "hospitals",
				Title:        "Hospitals",
				SemanticType: "healthcare.hospital",
				Properties:   []Field{emergency},
				Features: []Feature{{
					ID:    "osm/node/1842",
					Title: "St. Charles Bend",
					Geometry: Geometry{
						Kind:  GeometryPoint,
						Parts: []GeometryPart{{Rings: [][]Position{{{-121.2691, 44.0671}}}}},
					},
					Properties:    []Property{{FieldID: emergency.ID, Value: Int64Value(4)}},
					Relationships: []Relationship{{Predicate: "within", Target: "bend-or"}},
					Provenance:    []Provenance{{Source: "openstreetmap", NativeID: "node/1842", CapturedAt: "2026-08-01T20:13:08Z"}},
				}},
			}},
			RasterPyramids: []RasterPyramid{{
				ID: "streets", Name: "Streets", Codec: "image/webp", TileSize: 256, MinZoom: 0, MaxZoom: 13, FullZoom: 13, SourceZoom: 13,
				Template: "rasters/streets/{z}/{x}/{y}.webp",
				Formats:  []string{"webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp", "webp"},
			}},
			Presentation: Presentation{
				ID: "default", Title: "Default",
				Styles: []Style{{ID: "hospital", Symbol: "asset:hospital", Fill: "#bc3b4a"}},
				Layers: []Layer{{ID: "services-hospitals", FeatureSet: "hospitals", Style: "hospital", Visible: true, Order: 30, MinZoom: 8, MaxZoom: 22}},
				Legend: []LegendEntry{{Layer: "services-hospitals", Label: "Hospitals", Order: 10}},
			},
		}},
		Assets: []Asset{{ID: "hospital", MediaType: "image/svg+xml", Data: []byte("<svg/>"), Provenance: "custom"}},
	}

	packed, err := Compile(volume)
	if err != nil {
		t.Fatalf("compile volume: %v", err)
	}
	var archive bytes.Buffer
	if err := Write(&archive, packed); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	opened, err := Open(bytes.NewReader(archive.Bytes()), int64(archive.Len()), StandardSchema())
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	restored, err := opened.Volume()
	if err != nil {
		t.Fatalf("restore domain volume: %v", err)
	}
	if restored.ID != volume.ID || restored.Worlds[0].CoordinateSpace.Definition != "EPSG:3857" {
		t.Fatalf("restored wrong volume: %#v", restored)
	}
	if got := restored.Worlds[0].FeatureSets[0].Features[0].Properties[0].Value.Int64; got != 4 {
		t.Fatalf("typed extension property = %d, want 4", got)
	}
	if got := string(restored.Assets[0].Data); got != "<svg/>" {
		t.Fatalf("asset = %q", got)
	}

	presented, err := Present(restored.Worlds[0])
	if err != nil {
		t.Fatalf("present world: %v", err)
	}
	if len(presented) != 1 {
		t.Fatalf("presented %d features, want 1", len(presented))
	}
	if got := presented[0]; got.FeatureID != "osm/node/1842" || got.LayerID != "services-hospitals" || got.Symbol != "asset:hospital" || got.Legend != "Hospitals" {
		t.Fatalf("presented feature = %#v", got)
	}
}
