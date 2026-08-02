// Translator fixtures, read the way issue #5 §6 asks them to be read.
//
// A translator fixture is the reference tree's output for one archived capture:
// the MapGenie-shaped document every other source forged itself into. The clean
// room's interchange document is Atlas's own schema, so the two cannot be
// diffed. What these tests compare is what the two documents *mean* -- the same
// worlds, the same collections in the same order, the same features in the same
// places, the same prose -- and every intentional difference of shape is named
// here rather than left for a reader to infer.
//
// The fixtures are therefore reference material and regression tripwires, not
// the definition of correct. Correctness is defined at the composed-bundle
// level, which is reproduce_test.go's business.
package pipeline

import (
	"math"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// referenceDocument is as much of the reference tree's document shape as any of
// these tests reads. The field names are MapGenie's, because that is what the
// reference document was; nothing outside this file names them.
type referenceDocument struct {
	ID               int64   `json:"id"`
	Slug             string  `json:"slug"`
	Title            string  `json:"title"`
	InitialLatitude  float64 `json:"initial_latitude"`
	InitialLongitude float64 `json:"initial_longitude"`
	Game             struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	} `json:"game"`
	Config struct {
		TileSets []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"tile_sets"`
	} `json:"config"`
	Attrs  map[string]string `json:"atlas_attrs"`
	Groups []struct {
		Title      string `json:"title"`
		Categories []struct {
			ID        int64             `json:"id"`
			Title     string            `json:"title"`
			Icon      string            `json:"icon"`
			Visible   bool              `json:"visible"`
			Attrs     map[string]string `json:"atlas_attrs"`
			Locations []struct {
				ID          int64             `json:"id"`
				Title       string            `json:"title"`
				Description string            `json:"description"`
				Latitude    float64           `json:"latitude"`
				Longitude   float64           `json:"longitude"`
				Attrs       map[string]string `json:"atlas_attrs"`
			} `json:"locations"`
		} `json:"categories"`
	} `json:"groups"`
	Collections []struct {
		Key   string            `json:"key"`
		Title string            `json:"title"`
		Attrs map[string]string `json:"atlas_attrs"`
	} `json:"atlas_collections"`
	Regions []struct {
		ID          int64   `json:"id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Collection  string  `json:"atlas_collection"`
		Parent      *int64  `json:"parent_region_id"`
		CenterX     float64 `json:"center_x"`
		CenterY     float64 `json:"center_y"`
		Features    []struct {
			Geometry struct {
				Type string `json:"type"`
			} `json:"geometry"`
		} `json:"features"`
	} `json:"regions"`
}

// TestNASATrekTranslatorAgreesWithFixture holds the NASA Trek reader against the
// document the reference tree made of the same archived capture.
//
// Two differences of shape are deliberate and are not compared:
//
//   - The reference document declared each mosaic's zoom range, extension and
//     per-level bounds. The clean room's lens says only its name and its tile
//     set, because what a bundle promises about a raster must be what the
//     deriver actually derived, not what a publisher offered.
//   - The reference document nested every collection under a group container.
//     The clean room carries one ordered collections array and keeps the group
//     as the heading string it always was.
func TestNASATrekTranslatorAgreesWithFixture(t *testing.T) {
	document := translateFixture(t, "mars")

	var reference referenceDocument
	readJSON(t, "../fixtures/translators/nasa-trek.doc.json", &reference)

	if len(document.Worlds) != 1 {
		t.Fatalf("document carries %d worlds, the fixture describes one", len(document.Worlds))
	}
	world := document.Worlds[0]

	if document.Source.IDSpace != doc.IDSpaceDerived {
		t.Errorf("the gazetteer numbers nothing a bundle can use, so the source must declare "+
			"%q, not %q", doc.IDSpaceDerived, document.Source.IDSpace)
	}
	if document.Volume.Slug != reference.Game.Slug || document.Volume.Title != reference.Game.Title {
		t.Errorf("volume %s/%s, fixture %s/%s",
			document.Volume.Slug, document.Volume.Title, reference.Game.Slug, reference.Game.Title)
	}
	if world.ID != reference.ID || world.Slug != reference.Slug || world.Title != reference.Title {
		t.Errorf("world identity %d/%s/%s, fixture %d/%s/%s",
			world.ID, world.Slug, world.Title, reference.ID, reference.Slug, reference.Title)
	}
	if !near(world.Center.Lat, reference.InitialLatitude) || !near(world.Center.Lng, reference.InitialLongitude) {
		t.Errorf("center %v,%v, fixture %v,%v",
			world.Center.Lat, world.Center.Lng, reference.InitialLatitude, reference.InitialLongitude)
	}

	// The world's declared flattening is the whole coordinate design, so it is
	// compared attribute for attribute rather than sampled.
	compareAttrs(t, "world", world.Attrs, reference.Attrs)

	// One lens per captured mosaic, in capture order: the body's own picture
	// first and its siblings after.
	if len(world.Lenses) != len(reference.Config.TileSets) {
		t.Fatalf("%d lenses, fixture %d", len(world.Lenses), len(reference.Config.TileSets))
	}
	for i, set := range reference.Config.TileSets {
		if world.Lenses[i].Name != set.Name || world.Lenses[i].TileSet != set.Path {
			t.Errorf("lens %d is %s/%s, fixture %s/%s",
				i, world.Lenses[i].Name, world.Lenses[i].TileSet, set.Name, set.Path)
		}
	}

	compareCategories(t, world.Collections, reference)

	// A gazetteer has no ground and no artwork of its own: every collection
	// names a library glyph instead, and none of them ships one.
	if len(document.Icons) != 0 {
		t.Errorf("the source shipped %d pieces of artwork; it has none to ship", len(document.Icons))
	}
	for _, collection := range world.Collections {
		if collection.Attrs["atlas.icon.std"] == "" {
			t.Errorf("collection %q names no standard glyph and has no artwork", collection.Title)
		}
	}
}

// compareCategories walks the reference document's group-of-categories nesting
// against the clean room's flat collections array, in order.
func compareCategories(t *testing.T, collections []doc.Collection, reference referenceDocument) {
	t.Helper()
	index := 0
	for _, group := range reference.Groups {
		for _, category := range group.Categories {
			if index >= len(collections) {
				t.Fatalf("document ran out of collections at %s/%s", group.Title, category.Title)
			}
			got := collections[index]
			index++
			if got.ID != category.ID || got.Title != category.Title ||
				got.Group != group.Title || got.Icon != category.Icon ||
				got.Visible != category.Visible {
				t.Errorf("collection %d is %d/%s/%s/%s/%t, fixture %d/%s/%s/%s/%t",
					index-1, got.ID, got.Title, got.Group, got.Icon, got.Visible,
					category.ID, category.Title, group.Title, category.Icon, category.Visible)
				continue
			}
			if got.Kind != doc.KindPoint {
				t.Errorf("collection %s is kind %q, want %q", got.Title, got.Kind, doc.KindPoint)
			}
			compareAttrs(t, "collection "+got.Title, got.Attrs, category.Attrs)
			if len(got.Features) != len(category.Locations) {
				t.Errorf("collection %s holds %d features, fixture %d",
					got.Title, len(got.Features), len(category.Locations))
				continue
			}
			for i, location := range category.Locations {
				feature := got.Features[i]
				if feature.ID != location.ID || feature.Title != location.Title ||
					feature.Description != location.Description {
					t.Errorf("collection %s feature %d is %d/%q/%q, fixture %d/%q/%q",
						got.Title, i, feature.ID, feature.Title, feature.Description,
						location.ID, location.Title, location.Description)
					continue
				}
				if feature.At == nil {
					t.Errorf("collection %s feature %d stands nowhere", got.Title, i)
					continue
				}
				if !near(feature.At.Lat, location.Latitude) || !near(feature.At.Lng, location.Longitude) {
					t.Errorf("collection %s feature %d stands at %v,%v, fixture %v,%v",
						got.Title, i, feature.At.Lat, feature.At.Lng,
						location.Latitude, location.Longitude)
				}
				compareAttrs(t, "feature "+feature.Title, feature.Attrs, location.Attrs)
			}
		}
	}
	if index != len(collections) {
		t.Errorf("%d collections, fixture implies %d", len(collections), index)
	}
}

func compareAttrs(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s carries %d attributes %v, fixture %d %v", what, len(got), got, len(want), want)
		return
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s says %s=%q, fixture %q", what, key, got[key], value)
		}
	}
}

// near compares two coordinates the way a fixture round-tripped through JSON
// has to be compared: the reference document was written out and read back, so
// its last bit is a decimal spelling rather than the float that produced it.
func near(got, want float64) bool {
	return math.Abs(got-want) <= 1e-12*math.Max(1, math.Abs(want))
}

// TestIGNTranslatorAgreesWithFixture holds the IGN reader against the document
// the reference tree made of the same archived capture.
//
// The reference document nested collections under group containers; the clean
// room carries one ordered array with the group kept as the heading string it
// always was. Everything else -- identities, order, titles, artwork keys,
// placement, the render attribute -- is compared.
func TestIGNTranslatorAgreesWithFixture(t *testing.T) {
	document := translateFrom(t, "ign", "cyberpunk-2077")

	var reference referenceDocument
	readJSON(t, "../fixtures/translators/ign.doc.json", &reference)

	if len(document.Worlds) != 1 {
		t.Fatalf("document carries %d worlds, the fixture describes one", len(document.Worlds))
	}
	world := document.Worlds[0]

	if document.Source.IDSpace != doc.IDSpaceDerived {
		t.Errorf("IGN numbers markers with opaque strings, so the source must declare %q, not %q",
			doc.IDSpaceDerived, document.Source.IDSpace)
	}
	if document.Volume.Slug != reference.Game.Slug || document.Volume.Title != reference.Game.Title {
		t.Errorf("volume %s/%s, fixture %s/%s",
			document.Volume.Slug, document.Volume.Title, reference.Game.Slug, reference.Game.Title)
	}
	if world.ID != reference.ID || world.Slug != reference.Slug || world.Title != reference.Title {
		t.Errorf("world identity %d/%s/%s, fixture %d/%s/%s",
			world.ID, world.Slug, world.Title, reference.ID, reference.Slug, reference.Title)
	}
	if !near(world.Center.Lat, reference.InitialLatitude) || !near(world.Center.Lng, reference.InitialLongitude) {
		t.Errorf("center %v,%v, fixture %v,%v",
			world.Center.Lat, world.Center.Lng, reference.InitialLatitude, reference.InitialLongitude)
	}
	if len(world.Lenses) != len(reference.Config.TileSets) {
		t.Fatalf("%d lenses, fixture %d", len(world.Lenses), len(reference.Config.TileSets))
	}
	for i, set := range reference.Config.TileSets {
		if world.Lenses[i].Name != set.Name || world.Lenses[i].TileSet != set.Path {
			t.Errorf("lens %d is %s/%s, fixture %s/%s",
				i, world.Lenses[i].Name, world.Lenses[i].TileSet, set.Name, set.Path)
		}
	}
	compareCategories(t, world.Collections, reference)

	// A wikimap has no ground: nothing here becomes an area or a path.
	for _, collection := range world.Collections {
		if collection.Kind != doc.KindPoint {
			t.Errorf("collection %q is kind %q; a wikimap publishes only markers",
				collection.Title, collection.Kind)
		}
	}
	// The archive holds a sprite per marker type, and the document carries them
	// so composition never has to reach back into it.
	if len(document.Icons) == 0 {
		t.Error("the document carries no artwork, but the archive holds a sprite per type")
	}
}

// TestPiggybackTranslatorAgreesWithFixture holds the Piggyback reader against
// the document the reference tree made of the same archived capture, and pins
// the two things that are this source's own: prose survives, and district name
// markers render as text rather than pins.
func TestPiggybackTranslatorAgreesWithFixture(t *testing.T) {
	document := translateFrom(t, "piggyback", "cyberpunk-2077")

	var reference referenceDocument
	readJSON(t, "../fixtures/translators/piggyback.doc.json", &reference)

	if len(document.Worlds) != 1 {
		t.Fatalf("document carries %d worlds, the fixture describes one", len(document.Worlds))
	}
	world := document.Worlds[0]

	if document.Volume.Slug != reference.Game.Slug || document.Volume.Title != reference.Game.Title {
		t.Errorf("volume %s/%s, fixture %s/%s",
			document.Volume.Slug, document.Volume.Title, reference.Game.Slug, reference.Game.Title)
	}
	if world.ID != reference.ID || world.Slug != reference.Slug || world.Title != reference.Title {
		t.Errorf("world identity %d/%s/%s, fixture %d/%s/%s",
			world.ID, world.Slug, world.Title, reference.ID, reference.Slug, reference.Title)
	}
	if !near(world.Center.Lat, reference.InitialLatitude) || !near(world.Center.Lng, reference.InitialLongitude) {
		t.Errorf("center %v,%v, fixture %v,%v",
			world.Center.Lat, world.Center.Lng, reference.InitialLatitude, reference.InitialLongitude)
	}
	if len(world.Lenses) != len(reference.Config.TileSets) {
		t.Fatalf("%d lenses, fixture %d", len(world.Lenses), len(reference.Config.TileSets))
	}
	for i, set := range reference.Config.TileSets {
		if world.Lenses[i].Name != set.Name || world.Lenses[i].TileSet != set.Path {
			t.Errorf("lens %d is %s/%s, fixture %s/%s",
				i, world.Lenses[i].Name, world.Lenses[i].TileSet, set.Name, set.Path)
		}
	}
	compareCategories(t, world.Collections, reference)

	// The guide house's prose is the reason this source exists beside the other
	// reading of the same city.
	described := 0
	for _, collection := range world.Collections {
		for _, feature := range collection.Features {
			if feature.Description != "" {
				described++
			}
		}
	}
	if described == 0 {
		t.Error("no feature carries prose, which is the one thing this source has that IGN does not")
	}
	// District names are floating labels, not markers.
	labels := 0
	for _, collection := range world.Collections {
		if collection.Attrs["atlas.render.as"] == "text" {
			labels++
		}
	}
	if labels == 0 {
		t.Error("no collection renders as text, so the district names became pins")
	}
}
