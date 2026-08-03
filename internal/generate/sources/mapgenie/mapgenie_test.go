package mapgenie

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

func TestNormalizeHexColor(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"six digits", "6984F2", "#6984F2"},
		{"six digits with a hash", "#6984f2", "#6984F2"},
		{"padded", "  6984F2 ", "#6984F2"},
		{"three digits", "abc", "#ABC"},
		{"four digits", "abcd", "#ABCD"},
		{"eight digits", "aabbccdd", "#AABBCCDD"},
		{"five digits is no colour", "abcde", ""},
		{"a word is no colour", "orange", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeHexColor(tt.in); got != tt.want {
				t.Errorf("normalizeHexColor(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFirstColor(t *testing.T) {
	tests := []struct {
		name, category, group, want string
	}{
		{"the category decides", "ff0000", "00ff00", "#FF0000"},
		{"its group answers for it", "", "00ff00", "#00FF00"},
		{"a malformed category falls back", "nope", "00ff00", "#00FF00"},
		{"neither", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstColor(tt.category, tt.group); got != tt.want {
				t.Errorf("firstColor(%q, %q) = %q, want %q", tt.category, tt.group, got, tt.want)
			}
		})
	}
}

// TestNumber pins the one tolerance this reader owes its captures: MapGenie
// spells coordinates as numbers on some maps and as quoted strings on others.
func TestNumber(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
		bad  bool
	}{
		{"a number", `0.78796967592872`, 0.78796967592872, false},
		{"a quoted number", `"0.78796967592872"`, 0.78796967592872, false},
		{"a negative", `-0.77144622802734`, -0.77144622802734, false},
		{"padded", "  1.5  ", 1.5, false},
		{"prose", `"north"`, 0, true},
		{"nothing", ``, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := number(json.RawMessage(tt.raw))
			if tt.bad {
				if err == nil {
					t.Fatalf("number(%s) = %v, want a refusal", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("number(%s): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("number(%s) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestOptionalNumber(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    float64
		present bool
	}{
		{"a number", `12`, 12, true},
		{"null", `null`, 0, false},
		{"absent", ``, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, present, err := optionalNumber(json.RawMessage(tt.raw))
			if err != nil {
				t.Fatalf("optionalNumber(%s): %v", tt.raw, err)
			}
			if present != tt.present || got != tt.want {
				t.Errorf("optionalNumber(%s) = %v, %t, want %v, %t", tt.raw, got, present, tt.want, tt.present)
			}
		})
	}
}

// TestResolveLinks pins what happens to the publisher's prose. Nothing may ship
// a live URL, and a link that pointed at another pin of this same world becomes
// something the reader can follow instead.
func TestResolveLinks(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantText    string
		wantLinks   []doc.Link
	}{
		{
			name:        "prose with no link is untouched",
			description: "A shrine on the hill.",
			wantText:    "A shrine on the hill.",
		},
		{
			name:        "a link to another pin becomes a cross-reference",
			description: "See [the well](https://mapgenie.io/tunic/maps/world?locationIds=200).",
			wantText:    "See the well.",
			wantLinks:   []doc.Link{{Title: "the well", Feature: 200}},
		},
		{
			name:        "a link to a pin this world does not hold keeps only its label",
			description: "See [elsewhere](https://mapgenie.io/x?locationIds=999).",
			wantText:    "See elsewhere.",
		},
		{
			name:        "a link to itself is not a cross-reference",
			description: "See [here](https://mapgenie.io/x?locationIds=100).",
			wantText:    "See here.",
		},
		{
			// The guard against image syntax reads the matched label, and the
			// match begins at the bracket, so a bare "![alt](url)" is read as
			// an ordinary link whose label happens to follow a bang. This is
			// the reference build's behaviour, recorded rather than corrected:
			// the shape that actually occurs in captures is the nested one
			// below, and that is the one the guard was written for.
			name:        "a bang before a link is only a bang",
			description: "![shot](https://mapgenie.io/x?locationIds=200)",
			wantText:    "!shot",
			wantLinks:   []doc.Link{{Title: "shot", Feature: 200}},
		},
		{
			name:        "an image wrapped in a link is not a cross-reference",
			description: "[![alt](picture.png)](https://mapgenie.io/x?locationIds=200)",
			wantText:    "![alt](picture.png)",
		},
		{
			name:        "a bare URL is removed outright",
			description: "A shrine (https://example.com/guide) on the hill.",
			wantText:    "A shrine on the hill.",
		},
		{
			name:        "a label may hold one bracketed aside",
			description: "[Oh Baby! [Super Sledge]](https://mapgenie.io/x?locationIds=200)",
			wantText:    "Oh Baby! [Super Sledge]",
			wantLinks:   []doc.Link{{Title: "Oh Baby! [Super Sledge]", Feature: 200}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collections := []doc.Collection{{
				Kind: doc.KindPoint,
				Features: []doc.Feature{
					{ID: 100, Description: tt.description},
					{ID: 200, Title: "The well"},
				},
			}}
			resolveLinks(collections)
			got := collections[0].Features[0]
			if got.Description != tt.wantText {
				t.Errorf("description %q, want %q", got.Description, tt.wantText)
			}
			if len(got.Links) != len(tt.wantLinks) {
				t.Fatalf("links %v, want %v", got.Links, tt.wantLinks)
			}
			for i, want := range tt.wantLinks {
				if got.Links[i] != want {
					t.Errorf("link %d is %+v, want %+v", i, got.Links[i], want)
				}
			}
		})
	}
}

// TestCollectionsOf walks one hand-written capture into the interchange
// document, so the shape rules -- legend order, regions gathered under a
// collection of their own, an empty shape dropped -- are readable in one place.
func TestCollectionsOf(t *testing.T) {
	var raw rawMap
	if err := json.Unmarshal([]byte(`{
		"id": 1, "slug": "world", "title": "World",
		"config": {"tile_sets": [{"name": "Default", "path": "g/world/v1"}]},
		"game": {"slug": "game", "title": "Game"},
		"groups": [
			{"title": "Locations", "color": "6984F2", "categories": [
				{"id": 10, "title": "Shrines", "icon": "shrine", "visible": true,
				 "locations": [{"id": 100, "title": "One", "latitude": 1, "longitude": 2, "region_id": 900}]},
				{"id": 11, "title": "Labels", "display_type": "text", "visible": false,
				 "color": "FF0000", "locations": []}
			]}
		],
		"regions": [
			{"id": 900, "title": "North", "parent_region_id": 901,
			 "center_x": 5, "center_y": 6,
			 "features": [{"geometry": {"type": "Polygon", "coordinates": [[[0,0]]]}}]},
			{"id": 902, "title": "Nothing", "features": [{"geometry": {"type": "", "coordinates": null}}]}
		]
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	collections, err := collectionsOf(raw)
	if err != nil {
		t.Fatalf("collectionsOf: %v", err)
	}
	if len(collections) != 3 {
		t.Fatalf("%d collections, want the two categories and the regions", len(collections))
	}

	shrines := collections[0]
	if shrines.ID != 10 || shrines.Title != "Shrines" || shrines.Group != "Locations" ||
		shrines.Kind != doc.KindPoint || shrines.Icon != "shrine" || !shrines.Visible {
		t.Errorf("shrines is %+v", shrines)
	}
	if shrines.Color != "#6984F2" {
		t.Errorf("shrines took colour %q, want its group's", shrines.Color)
	}
	if shrines.Attrs[semconv.KeyRenderAs] != semconv.RenderAsPin {
		t.Errorf("shrines renders as %q, want a pin", shrines.Attrs[semconv.KeyRenderAs])
	}
	if len(shrines.Features) != 1 || shrines.Features[0].Member != 900 {
		t.Errorf("shrines holds %+v", shrines.Features)
	}
	if shrines.Features[0].At == nil || shrines.Features[0].At.Lat != 1 || shrines.Features[0].At.Lng != 2 {
		t.Errorf("the shrine stands at %+v", shrines.Features[0].At)
	}

	labels := collections[1]
	if labels.Attrs[semconv.KeyRenderAs] != semconv.RenderAsText {
		t.Errorf("a text category renders as %q", labels.Attrs[semconv.KeyRenderAs])
	}
	if labels.Color != "#FF0000" {
		t.Errorf("a category with its own colour took %q", labels.Color)
	}
	if labels.Visible {
		t.Errorf("a hidden category came through visible")
	}

	regions := collections[2]
	if regions.Key != regionsKey || regions.Kind != doc.KindArea || !regions.Visible {
		t.Errorf("the regions collection is %+v", regions)
	}
	if regions.ID != 0 {
		t.Errorf("the regions collection claimed id %d; numbering is composition's", regions.ID)
	}
	if len(regions.Features) != 1 {
		t.Fatalf("%d regions, want the one that draws something", len(regions.Features))
	}
	north := regions.Features[0]
	if north.ID != 900 || north.Parent != 901 {
		t.Errorf("north is %+v", north)
	}
	if north.Center == nil || north.Center.Lat != 6 || north.Center.Lng != 5 {
		t.Errorf("north's centre is %+v; the capture spells it x then y", north.Center)
	}
}

func TestCollectionsOfRefusesLines(t *testing.T) {
	var raw rawMap
	if err := json.Unmarshal([]byte(`{
		"id": 1, "regions": [{"id": 900, "title": "A trail",
		  "features": [{"geometry": {"type": "MultiLineString", "coordinates": [[[0,0]]]}}]}]
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := collectionsOf(raw); err == nil || !strings.Contains(err.Error(), "not ground") {
		t.Fatalf("a line among the regions was accepted: %v", err)
	}
}

// TestTranslateRefusesAForeignKind is the source gate: reading another source's
// bytes through this reader would produce a document that lies about where it
// came from.
func TestTranslateRefusesAForeignKind(t *testing.T) {
	root := writeArchive(t, "trek-map", `{"id":1,"slug":"world","config":{"tile_sets":[{"name":"D","path":"p"}]}}`)
	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "answers only for") {
		t.Fatalf("a foreign capture was read: %v", err)
	}
}

// TestTranslateSkipsAWorldWithNoRaster: a world with no tile set is not ready,
// not broken. An archive is filled by hand and a half-crawled map is ordinary.
func TestTranslateSkipsAWorldWithNoRaster(t *testing.T) {
	root := writeArchive(t, "map", `{"id":1,"slug":"world","config":{"tile_sets":[]}}`)
	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("want a not-ready refusal, got %v", err)
	}
}

// writeArchive lays out the smallest archive holding one volume of one world.
func writeArchive(t *testing.T, kind, capture string) string {
	t.Helper()
	root := t.TempDir()
	worldDir := filepath.Join(root, "games", "g-1", "maps", "world-1")
	mustWrite(t, filepath.Join(root, "archive.json"),
		`{"games":[{"directory":"games/g-1","id":1,"title":"Game"}]}`)
	mustWrite(t, filepath.Join(root, "games", "g-1", "game.json"),
		`{"id":1,"title":"Game","maps":[{"directory":"games/g-1/maps/world-1","id":1,"slug":"world","title":"World"}]}`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "index.json"),
		`[{"capturedAt":"2026-01-01T00:00:00Z","contentHash":"h","kind":"`+kind+`","sourceId":1,"sourceUrl":"/x"}]`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "map", "h.json"), capture)
	return root
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
