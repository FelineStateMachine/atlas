package doc

import (
	"encoding/json"
	"strings"
	"testing"
)

// minimal is the smallest document that passes Validate. Every test below
// starts here and breaks one thing, so a failing case names exactly the rule it
// broke.
func minimal() Document {
	return Document{
		Doc:     Doc,
		Version: Version,
		Volume:  Volume{Slug: "tunic", Title: "TUNIC"},
		Source:  Provenance{Name: "somewhere", Label: "Somewhere", IDSpace: IDSpaceNative},
		Worlds: []World{{
			ID:      427,
			Slug:    "world",
			Title:   "World",
			Capture: Capture{Kind: "map", ContentHash: "abc", CapturedAt: "2026-07-30T03:57:41.529Z"},
			Lenses:  []Lens{{Name: "Default", TileSet: "tunic/world/default-v2"}},
			Collections: []Collection{{
				ID: 1, Title: "Shrines", Kind: KindPoint, Visible: true,
				Features: []Feature{{ID: 10, Title: "One", At: &Position{Lat: 1, Lng: 2}}},
			}},
		}},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		bend func(*Document)
		want string // substring of the expected message; "" means valid
	}{
		{"the minimal document", func(*Document) {}, ""},
		{"a foreign schema", func(d *Document) { d.Doc = "something-else" }, "not \"atlas-generate-doc\""},
		{"a version from the future", func(d *Document) { d.Version = 99 }, "version 99"},
		{"no volume", func(d *Document) { d.Volume.Slug = "" }, "names no volume"},
		{"no source", func(d *Document) { d.Source.Name = "" }, "names no source"},
		{"an unspoken id space", func(d *Document) { d.Source.IDSpace = "" }, "declares id space"},
		{"no world", func(d *Document) { d.Worlds = nil }, "carries no world"},
		{"a world with no slug", func(d *Document) { d.Worlds[0].Slug = "" }, "carries no slug"},
		{"two worlds of one slug", func(d *Document) {
			d.Worlds = append(d.Worlds, d.Worlds[0])
		}, "appears twice"},
		{"a world naming no capture", func(d *Document) {
			d.Worlds[0].Capture.ContentHash = ""
		}, "names no capture"},
		{"a capture with no time", func(d *Document) {
			d.Worlds[0].Capture.CapturedAt = ""
		}, "carries no time"},
		{"a world with no lens", func(d *Document) { d.Worlds[0].Lenses = nil }, "has no lens"},
		{"a lens naming no tile set", func(d *Document) {
			d.Worlds[0].Lenses[0].TileSet = ""
		}, "names no tile set"},
		{"a collection of no kind", func(d *Document) {
			d.Worlds[0].Collections[0].Kind = "blob"
		}, "declares kind \"blob\""},
		{"a feature with no id", func(d *Document) {
			d.Worlds[0].Collections[0].Features[0].ID = 0
		}, "feature with no id"},
		{"two features of one id", func(d *Document) {
			world := &d.Worlds[0]
			world.Collections = append(world.Collections, Collection{
				ID: 2, Title: "Chests", Kind: KindPoint,
				Features: []Feature{{ID: 10, Title: "Other", At: &Position{}}},
			})
		}, "share id 10"},
		{"a point standing nowhere", func(d *Document) {
			d.Worlds[0].Collections[0].Features[0].At = nil
		}, "stands nowhere"},
		{"an area drawing nothing", func(d *Document) {
			d.Worlds[0].Collections[0].Kind = KindArea
			d.Worlds[0].Collections[0].Features[0].At = nil
		}, "draws nothing"},
		{"an icon with no key", func(d *Document) {
			d.Icons = []Icon{{File: "x.svg"}}
		}, "carries no key"},
		{"one icon twice", func(d *Document) {
			d.Icons = []Icon{{Key: "a", File: "a.svg"}, {Key: "a", File: "a.png"}}
		}, "carried twice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := minimal()
			tt.bend(&d)
			err := d.Validate()
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("valid document refused: %v", err)
			case tt.want == "":
				return
			case err == nil:
				t.Fatalf("accepted, want a refusal naming %q", tt.want)
			case !strings.Contains(err.Error(), tt.want):
				t.Fatalf("refused with %q, want it to name %q", err, tt.want)
			}
		})
	}
}

func TestNewestCapture(t *testing.T) {
	d := minimal()
	d.Worlds = append(d.Worlds, World{
		Slug:    "second",
		Capture: Capture{ContentHash: "def", CapturedAt: "2026-08-01T00:00:00Z"},
		Lenses:  []Lens{{TileSet: "x"}},
	})
	if got := d.NewestCapture(); got != "2026-08-01T00:00:00Z" {
		t.Errorf("newest capture %q", got)
	}
}

// TestRoundTrip holds the document to the one property a debugging dump needs:
// what is printed reads back as the same thing.
func TestRoundTrip(t *testing.T) {
	d := minimal()
	d.Icons = []Icon{{Key: "shrine", File: "shrine.svg", Data: []byte("<svg/>")}}
	d.Worlds[0].Collections[0].Features[0].Links = []Link{{Title: "See also", Feature: 11}}
	data, err := d.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := back.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(again) {
		t.Errorf("a document does not survive a round trip\nfirst:\n%s\nagain:\n%s", data, again)
	}
}

// TestGeometryIsCarriedOpaquely is the property that lets a source's numbers
// reach a payload unrounded: coordinates are never decoded on the way through.
func TestGeometryIsCarriedOpaquely(t *testing.T) {
	raw := json.RawMessage(`[[[0.30000001192092896,1e3]]]`)
	d := minimal()
	d.Worlds[0].Collections[0].Kind = KindArea
	d.Worlds[0].Collections[0].Features[0].At = nil
	d.Worlds[0].Collections[0].Features[0].Geometry = []Geometry{{Type: "Polygon", Coordinates: raw}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := d.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "0.30000001192092896") || !strings.Contains(string(data), "1e3") {
		t.Errorf("coordinates were reformatted on the way through:\n%s", data)
	}
}

func TestAttrKeys(t *testing.T) {
	got := AttrKeys(map[string]string{"b": "", "a": "", "c": ""})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("AttrKeys = %v, want sorted", got)
	}
}
