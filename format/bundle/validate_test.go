package bundle_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
)

// payload assembles a world payload around a collections array, so each case
// varies one thing.
func payload(collections string) string {
	return "{" + fixtureLenses + `,"collections":[` + collections + `]}`
}

const (
	line    = `{"type":"MultiLineString","coordinates":[[[0,0],[1,1]]]}`
	polygon = `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]}`
)

// The structural promises of the v3 wire, one broken at a time. Everything
// here is a producer error: a reader that meets one has been handed a bundle
// nobody validated.
func TestValidateHoldsAWorldToItsPromises(t *testing.T) {
	cases := []struct {
		name    string
		world   fixtureWorld
		icons   []string
		mention string
	}{
		{
			name:  "a sound point world",
			world: fixtureWorld{slug: "overworld", locations: []bundle.Location{{ID: 1, Title: "Origin"}}},
		},
		{
			name: "a sound area world",
			world: fixtureWorld{slug: "overworld", areas: 1, detail: payload(
				`{"id":9,"title":"Districts","kind":"area","visible":true,` +
					`"features":[{"id":5,"title":"R-5","geometry":[` + polygon + `]}]}`)},
		},
		{
			name: "a sound path world",
			world: fixtureWorld{slug: "overworld", paths: 1, detail: payload(
				`{"id":9,"title":"Creeks","kind":"path","visible":true,` +
					`"attrs":{"atlas.stroke.width_px":"10"},` +
					`"features":[{"id":5,"title":"Big Dry Creek","geometry":[` + line + `]}]}`)},
		},
		{
			name: "a payload carrying a runtime URL",
			world: fixtureWorld{slug: "overworld", locations: []bundle.Location{{ID: 1}},
				text: `{"1":{"d":"see https://example.invalid/x"}}`},
			mention: "runtime URL",
		},
		{
			name: "a payload linking over plain http",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true,` +
					`"icon":"http://example.invalid/pin.svg"}`)},
			mention: "runtime URL",
		},
		{
			name: "a manifest promising more points than are packed",
			world: fixtureWorld{slug: "overworld", points: 7, countsStated: true,
				locations: []bundle.Location{{ID: 1}}},
			mention: "7",
		},
		{
			name: "a manifest counting areas the payload does not hold",
			world: fixtureWorld{slug: "overworld", areas: 2, detail: payload(
				`{"id":9,"title":"Districts","kind":"area","visible":true,` +
					`"features":[{"id":5,"title":"R-5","geometry":[` + polygon + `]}]}`)},
			mention: "manifest says",
		},
		{
			name: "geometry disagreeing with the declared kind",
			world: fixtureWorld{slug: "overworld", areas: 1, detail: payload(
				`{"id":9,"title":"Districts","kind":"area","visible":true,` +
					`"features":[{"id":5,"title":"R-5","geometry":[` + line + `]}]}`)},
			mention: "inlines a MultiLineString",
		},
		{
			name: "a path collection with no stroke width",
			world: fixtureWorld{slug: "overworld", paths: 1, detail: payload(
				`{"id":9,"title":"Creeks","kind":"path","visible":true,` +
					`"features":[{"id":5,"title":"Big Dry Creek","geometry":[` + line + `]}]}`)},
			mention: "declares no atlas.stroke.width_px",
		},
		{
			name: "a label policy on a path collection",
			world: fixtureWorld{slug: "overworld", paths: 1, detail: payload(
				`{"id":9,"title":"Creeks","kind":"path","visible":true,` +
					`"attrs":{"atlas.stroke.width_px":"10","atlas.label.policy":"quiet"},` +
					`"features":[{"id":5,"title":"Big Dry Creek","geometry":[` + line + `]}]}`)},
			mention: "label policy",
		},
		{
			name: "a point collection carrying inline features",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true,` +
					`"features":[{"id":5,"title":"Stray","geometry":[]}]}`)},
			mention: "inline features",
		},
		{
			name: "a collection of no known kind",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"blob","visible":true}`)},
			mention: "none of point, path, area",
		},
		{
			name: "two collections sharing an id",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true},` +
					`{"id":1,"title":"Other","kind":"point","visible":true}`)},
			mention: "share id 1",
		},
		{
			name: "a collection naming an icon that is not there",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true,"iconAsset":"absent.svg"}`)},
			mention: "missing icon",
		},
		{
			name: "a location owned by no collection at all",
			world: fixtureWorld{slug: "overworld",
				locations: []bundle.Location{{ID: 1, Owner: 4}},
				detail:    payload(`{"id":1,"title":"Marker","kind":"point","visible":true}`)},
			mention: "no point collection",
		},
		{
			name: "a location owned by an area collection",
			world: fixtureWorld{slug: "overworld", areas: 1,
				locations: []bundle.Location{{ID: 1, Owner: 0}},
				detail: payload(`{"id":9,"title":"Districts","kind":"area","visible":true,` +
					`"features":[{"id":5,"title":"R-5","geometry":[` + polygon + `]}]}`)},
			mention: "no point collection",
		},
		{
			name:    "a world with no lenses at all",
			world:   fixtureWorld{slug: "overworld", detail: `{"lenses":[],"collections":[]}`},
			mention: "no lenses",
		},
		{
			name: "a lens naming fewer formats than it has levels",
			world: fixtureWorld{slug: "overworld", detail: `{"lenses":[{"tiles":"overworld",` +
				`"minZoom":0,"maxZoom":2,"formats":["jpg"]}],"collections":[]}`},
			mention: "tile formats",
		},
		{
			name: "a lens claiming a level that holds no tiles",
			world: fixtureWorld{slug: "overworld", detail: `{"lenses":[{"tiles":"overworld",` +
				`"minZoom":0,"maxZoom":1,"formats":["jpg","jpg"]}],"collections":[]}`},
			mention: "empty tile level 1",
		},
		{
			name:    "a payload that is not JSON",
			world:   fixtureWorld{slug: "overworld", detail: `{not json`},
			mention: "decode payload",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reader := fixture{worlds: []fixtureWorld{test.world}, icons: test.icons}.open(t, t.TempDir())
			err := reader.Validate()
			if test.mention == "" {
				if err != nil {
					t.Fatalf("a sound bundle is refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Fatalf("validated with %v, want a complaint about %q", err, test.mention)
			}
		})
	}
}

// The conventions half of validation is asymmetric by design: a bundle that
// declares a vocabulary is held to it, and one that declares none is held to
// nothing.
func TestValidateIsStrictOnlyAboutWhatIsDeclared(t *testing.T) {
	unregistered := payload(`{"id":1,"title":"Marker","kind":"point","visible":true,` +
		`"attrs":{"atlas.render.like":"pin"}}`)

	silent := fixture{worlds: []fixtureWorld{{slug: "overworld", detail: unregistered}}}
	if err := silent.open(t, t.TempDir()).Validate(); err != nil {
		t.Errorf("a bundle declaring no conventions was held to them: %v", err)
	}
	declaring := silent.conventional()
	if err := declaring.open(t, t.TempDir()).Validate(); err == nil ||
		!strings.Contains(err.Error(), "not registered") {
		t.Errorf("a declaring bundle escaped the registry: %v", err)
	}
}

func TestValidateHoldsDeclaredConventions(t *testing.T) {
	sphere := func(attrs string) string {
		return `{` + fixtureLenses + `,"collections":[],"attrs":` + attrs + `}`
	}
	cases := []struct {
		name    string
		world   fixtureWorld
		mention string
	}{
		{
			name: "a fully declared sphere",
			world: fixtureWorld{slug: "overworld", detail: sphere(
				`{"atlas.geometry.surface":"sphere","atlas.geometry.projection":"equirect",` +
					`"atlas.geometry.equirect.px":"0,0,8192,4096",` +
					`"atlas.geometry.equirect.deg":"-180,90,180,-90"}`)},
		},
		{
			name:    "a sphere with no projection",
			world:   fixtureWorld{slug: "overworld", detail: sphere(`{"atlas.geometry.surface":"sphere"}`)},
			mention: "no projection",
		},
		{
			name: "a sphere whose mapping does not parse",
			world: fixtureWorld{slug: "overworld", detail: sphere(
				`{"atlas.geometry.surface":"sphere","atlas.geometry.projection":"equirect",` +
					`"atlas.geometry.equirect.px":"0,0,0,4096",` +
					`"atlas.geometry.equirect.deg":"-180,90,180,-90"}`)},
			mention: "no area",
		},
		{
			name: "a world attribute on a collection",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true,` +
					`"attrs":{"atlas.geometry.surface":"plane"}}`)},
			mention: "attaches to",
		},
		{
			name: "a declared kind disagreeing with the wire",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true,` +
					`"attrs":{"atlas.geometry.kind":"area"}}`)},
			mention: "while its attributes say",
		},
		{
			name: "a standard icon that was never resolved",
			world: fixtureWorld{slug: "overworld", detail: payload(
				`{"id":1,"title":"Marker","kind":"point","visible":true,` +
					`"attrs":{"atlas.icon.std":"maki/mountain"}}`)},
			mention: "never resolved",
		},
		{
			name: "an inline feature carrying a collection's attribute",
			world: fixtureWorld{slug: "overworld", areas: 1, detail: payload(
				`{"id":9,"title":"Districts","kind":"area","visible":true,` +
					`"features":[{"id":5,"title":"R-5","attrs":{"atlas.render.as":"pin"},` +
					`"geometry":[` + polygon + `]}]}`)},
			mention: "attaches to",
		},
		{
			name: "a text entry carrying an unregistered attribute",
			world: fixtureWorld{slug: "overworld", locations: []bundle.Location{{ID: 1}},
				text: `{"1":{"d":"words","a":{"atlas.geo.height":"5"}}}`},
			mention: "not registered",
		},
		{
			name: "a text entry with a well-formed attribute",
			world: fixtureWorld{slug: "overworld", locations: []bundle.Location{{ID: 1}},
				text: `{"1":{"d":"words","a":{"atlas.geo.lat":"-42.4301"}}}`},
		},
		{
			name: "a text payload that is not JSON",
			world: fixtureWorld{slug: "overworld", locations: []bundle.Location{{ID: 1}},
				text: `{not json`},
			mention: "decode text",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reader := (fixture{worlds: []fixtureWorld{test.world}}).conventional().open(t, t.TempDir())
			err := reader.Validate()
			if test.mention == "" {
				if err != nil {
					t.Fatalf("a sound bundle is refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Fatalf("validated with %v, want a complaint about %q", err, test.mention)
			}
		})
	}
}

func TestValidateCatchesAWorldThatIsNotThere(t *testing.T) {
	// Two worlds written and listed validates; the same bundle with a third
	// world listed and not written does not.
	reader := fixture{
		worlds: []fixtureWorld{
			{slug: "overworld", locations: []bundle.Location{{ID: 1}}},
			{slug: "underworld", locations: []bundle.Location{{ID: 2}}},
		},
	}.open(t, t.TempDir())
	if err := reader.Validate(); err != nil {
		t.Fatalf("two written worlds fail validation: %v", err)
	}

	manifest := soundManifest()
	manifest.Worlds = append(manifest.Worlds, bundle.WorldEntry{Slug: "underworld", Title: "Underworld"})
	path := filepath.Join(t.TempDir(), "promised.atlas")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	must(t, writer.AddDeflated(bundle.WorldEntryName("overworld", bundle.WorldSuffix),
		[]byte(payload(`{"id":1,"title":"Marker","kind":"point","visible":true}`))))
	must(t, writer.AddStored(bundle.WorldEntryName("overworld", bundle.PackedSuffix),
		bytes.NewReader(bundle.PackLocations(nil))))
	must(t, writer.AddDeflated(bundle.WorldEntryName("overworld", bundle.TextSuffix), []byte("{}")))
	must(t, writer.AddStored(bundle.TilesPrefix+"overworld/0/0/0.jpg", bytes.NewReader([]byte("raster"))))
	must(t, writer.Close())
	must(t, file.Close())

	promised, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer promised.Close()
	if err := promised.Validate(); err == nil || !strings.Contains(err.Error(), "underworld") {
		t.Errorf("a promised-but-absent world validated with %v", err)
	}
}

func TestGeometryFitsKind(t *testing.T) {
	cases := []struct {
		kind, geometry string
		want           bool
	}{
		{semconv.GeometryArea, "MultiPolygon", true},
		{semconv.GeometryArea, "Polygon", true},
		{semconv.GeometryArea, "LineString", false},
		{semconv.GeometryPath, "MultiLineString", true},
		{semconv.GeometryPath, "LineString", true},
		{semconv.GeometryPath, "Polygon", false},
		{semconv.GeometryPoint, "Point", false},
		{"blob", "Polygon", false},
	}
	for _, test := range cases {
		if got := bundle.GeometryFitsKind(test.kind, test.geometry); got != test.want {
			t.Errorf("GeometryFitsKind(%q, %q) = %v, want %v", test.kind, test.geometry, got, test.want)
		}
	}
}
