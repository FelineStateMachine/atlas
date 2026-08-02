package semconv_test

import (
	"math"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

func TestValidateAcceptsTheVocabulary(t *testing.T) {
	cases := []struct {
		name   string
		entity semconv.Entity
		attrs  map[string]string
	}{
		{"a point collection", semconv.EntityCollection, map[string]string{semconv.KeyRenderAs: "pin"}},
		{"a text collection with a standard icon", semconv.EntityCollection, map[string]string{
			semconv.KeyRenderAs: "text", semconv.KeyIconStd: "maki/mountain"}},
		{"a glyph collection with a merge identity", semconv.EntityCollection, map[string]string{
			semconv.KeyIconKind: "glyph", semconv.KeyCollectionKey: "ripperdoc"}},
		{"a declared kind", semconv.EntityCollection, map[string]string{semconv.KeyGeometryKind: "point"}},
		{"a path with a stroke", semconv.EntityCollection, map[string]string{
			semconv.KeyGeometryKind: "path", semconv.KeyStrokeWidthPx: "12"}},
		{"an area with a label policy", semconv.EntityCollection, map[string]string{
			semconv.KeyGeometryKind: "area", semconv.KeyLabelPolicy: "quiet"}},
		{"a sphere fully declared", semconv.EntityWorld, map[string]string{
			semconv.KeyGeometrySurface:     "sphere",
			semconv.KeyGeometryProjection:  "equirect",
			semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
			semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
			semconv.KeyGeometryBody:        "mars",
			semconv.KeyGeometryRadiusKM:    "3389.5",
		}},
		{"a mercator cut", semconv.EntityWorld, map[string]string{
			semconv.KeyGeometrySurface:     "plane",
			semconv.KeyGeometryMercatorPx:  "0,0,8192,8192",
			semconv.KeyGeometryMercatorDeg: "-105.19,39.98,-104.96,39.80",
		}},
		{"a marker outset", semconv.EntityWorld, map[string]string{semconv.KeyIconOutset: "dark"}},
		{"published coordinates", semconv.EntityFeature, map[string]string{
			semconv.KeyGeoLat: "-42.4301", semconv.KeyGeoLon: "70.5025"}},
		{"a watershed", semconv.EntityFeature, map[string]string{semconv.KeyHydroHUC12: "170703010101"}},
		{"a foreign namespace", semconv.EntityFeature, map[string]string{"vendor.thing": "whatever"}},
		// Empty is always fine: conventions are declared, never demanded.
		{"nothing at all", semconv.EntityCollection, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := semconv.Validate(test.entity, test.attrs); err != nil {
				t.Errorf("%s %v refused: %v", test.entity, test.attrs, err)
			}
		})
	}
}

func TestValidateRefusesWhatTheRegistryDoesNotKnow(t *testing.T) {
	cases := []struct {
		name    string
		entity  semconv.Entity
		attrs   map[string]string
		mention string
	}{
		{"an invented key", semconv.EntityCollection, map[string]string{"atlas.render.like": "pin"}, "not registered"},
		{"a value outside the vocabulary", semconv.EntityCollection, map[string]string{semconv.KeyRenderAs: "zone"}, "not one of"},
		{"a collection key on a world", semconv.EntityWorld, map[string]string{semconv.KeyRenderAs: "pin"}, "attaches to"},
		{"a bare icon name", semconv.EntityCollection, map[string]string{semconv.KeyIconStd: "mountain"}, "set/name"},
		{"an icon name in caps", semconv.EntityCollection, map[string]string{semconv.KeyIconStd: "Maki/Mountain"}, "slug"},
		{"a geometry kind that is none", semconv.EntityCollection, map[string]string{semconv.KeyGeometryKind: "polygon"}, "not one of"},
		{"a label policy that is none", semconv.EntityCollection, map[string]string{semconv.KeyLabelPolicy: "loud"}, "not one of"},
		{"a collection key on a feature", semconv.EntityFeature, map[string]string{semconv.KeyGeometryKind: "area"}, "attaches to"},
		{"a stroke on a feature", semconv.EntityFeature, map[string]string{semconv.KeyStrokeWidthPx: "12"}, "attaches to"},
		{"a stroke of zero", semconv.EntityCollection, map[string]string{semconv.KeyStrokeWidthPx: "0"}, "positive"},
		{"a three-number window", semconv.EntityWorld, map[string]string{semconv.KeyGeometryEquirectPx: "0,0,8192"}, "4"},
		{"a radius in words", semconv.EntityWorld, map[string]string{semconv.KeyGeometryRadiusKM: "big"}, "number"},
		{"a latitude in words", semconv.EntityFeature, map[string]string{semconv.KeyGeoLat: "north"}, "number"},
		{"a short watershed", semconv.EntityFeature, map[string]string{semconv.KeyHydroHUC12: "17070301"}, "twelve digits"},
		{"a watershed on a collection", semconv.EntityCollection, map[string]string{semconv.KeyHydroHUC12: "170703010101"}, "attaches to"},
		// The policy name never rides a payload.
		{"a policy name", semconv.EntityFeature, map[string]string{semconv.KeyNoteText: "words"}, "not registered"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := semconv.Validate(test.entity, test.attrs)
			if err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Errorf("%s %v: %v, wanted mention of %q", test.entity, test.attrs, err, test.mention)
			}
		})
	}
}

// A reader that meets a key from a newer vocabulary skips it; it never has a
// reason to refuse the bundle carrying it. This is the lenient half of the
// asymmetry, stated as a test so it cannot quietly become strict.
func TestReadersAreLenientAboutUnknownKeys(t *testing.T) {
	if _, known := semconv.EntityOf("atlas.something.new"); known {
		t.Error("an unregistered key reports as known")
	}
	if _, known := semconv.StabilityOf("atlas.something.new"); known {
		t.Error("an unregistered key reports a stability")
	}
	for _, key := range semconv.Keys() {
		if _, known := semconv.EntityOf(key); !known {
			t.Errorf("%s is listed but unknown", key)
		}
		if stability, known := semconv.StabilityOf(key); !known ||
			(stability != semconv.Stable && stability != semconv.Experimental) {
			t.Errorf("%s carries stability %q", key, stability)
		}
	}
}

func TestKeysAreSortedAndComplete(t *testing.T) {
	keys := semconv.Keys()
	if len(keys) != 19 {
		t.Errorf("the registry holds %d keys, want 19", len(keys))
	}
	for index := 1; index < len(keys); index++ {
		if keys[index-1] >= keys[index] {
			t.Fatalf("Keys is unsorted at %d: %q then %q", index, keys[index-1], keys[index])
		}
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, semconv.Namespace) {
			t.Errorf("%s is registered outside the namespace", key)
		}
	}
}

func TestRenderAsPrefersAttributesOverLegacy(t *testing.T) {
	cases := []struct {
		attrs  map[string]string
		legacy string
		want   string
	}{
		{map[string]string{semconv.KeyRenderAs: "text"}, "markers", "text"},
		{map[string]string{semconv.KeyRenderAs: "pin"}, "text", "pin"},
		{nil, "text", "text"},
		{nil, "markers", "pin"},
		{nil, "", "pin"},
	}
	for _, test := range cases {
		if got := semconv.RenderAs(test.attrs, test.legacy); got != test.want {
			t.Errorf("RenderAs(%v, %q) = %q, want %q", test.attrs, test.legacy, got, test.want)
		}
	}
}

func TestLabelPolicyCuratesAreasOnly(t *testing.T) {
	cases := []struct {
		kind  string
		attrs map[string]string
		want  string
	}{
		{semconv.GeometryArea, nil, semconv.LabelAlways},
		{semconv.GeometryArea, map[string]string{semconv.KeyLabelPolicy: "quiet"}, semconv.LabelQuiet},
		{semconv.GeometryPath, nil, semconv.LabelQuiet},
		{semconv.GeometryPoint, nil, semconv.LabelQuiet},
	}
	for _, test := range cases {
		if got := semconv.LabelPolicy(test.kind, test.attrs); got != test.want {
			t.Errorf("LabelPolicy(%q, %v) = %q, want %q", test.kind, test.attrs, got, test.want)
		}
	}
}

// The Mars mapping drives both ways: the declared window is the top half of
// the 8192-pixel world, and every corner and famous place must survive there
// and back within floating-point noise.
func TestEquirectRoundTrip(t *testing.T) {
	mapping, err := semconv.ParseEquirect("0,0,8192,4096", "-180,90,180,-90")
	if err != nil {
		t.Fatal(err)
	}
	places := []struct {
		name     string
		lat, lon float64
	}{
		{"northwest corner", 90, -180},
		{"northeast corner", 90, 180},
		{"southwest corner", -90, -180},
		{"southeast corner", -90, 180},
		{"the origin", 0, 0},
		{"Olympus Mons", 18.6528, -133.8025},
		{"Hellas Planitia", -42.4301, 70.5025},
		{"Valles Marineris", -14.0059, -58.5877},
	}
	for _, place := range places {
		x, y := mapping.Apply(place.lat, place.lon)
		lat, lon := mapping.Invert(x, y)
		if math.Abs(lat-place.lat) > 1e-9 || math.Abs(lon-place.lon) > 1e-9 {
			t.Errorf("%s round-trips to %v,%v via %v,%v", place.name, lat, lon, x, y)
		}
	}
	// The anchor the whole design stands on: Olympus Mons lands where the
	// linear mapping says it should, with no hidden fudge in between.
	x, y := mapping.Apply(18.6528, -133.8025)
	wantX := (-133.8025 + 180) / 360 * 8192
	wantY := (90 - 18.6528) / 180 * 4096
	if math.Abs(x-wantX) > 1e-9 || math.Abs(y-wantY) > 1e-9 {
		t.Errorf("Olympus Mons at %v,%v, want %v,%v", x, y, wantX, wantY)
	}
}

func TestParseEquirectRefusesEmptyWindows(t *testing.T) {
	cases := []struct {
		name    string
		px, deg string
	}{
		{"a window with no width", "0,0,0,4096", "-180,90,180,-90"},
		{"a window with no height", "0,0,8192,0", "-180,90,180,-90"},
		{"a ground with no span", "0,0,8192,4096", "-180,90,-180,-90"},
		{"a ground with no height", "0,0,8192,4096", "-180,90,180,90"},
		{"three numbers", "0,0,8192", "-180,90,180,-90"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := semconv.ParseEquirect(test.px, test.deg); err == nil {
				t.Errorf("ParseEquirect(%q, %q) accepted", test.px, test.deg)
			}
		})
	}
}

func TestEquirectOfReadsAWorldsAttributes(t *testing.T) {
	if _, declared, err := semconv.EquirectOf(map[string]string{semconv.KeyGeometrySurface: "plane"}); declared || err != nil {
		t.Errorf("a plane declares a mapping: declared=%v err=%v", declared, err)
	}
	mapping, declared, err := semconv.EquirectOf(map[string]string{
		semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
		semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
	})
	if !declared || err != nil {
		t.Fatalf("a declared mapping did not read: declared=%v err=%v", declared, err)
	}
	if mapping.W != 8192 || mapping.North != 90 {
		t.Errorf("mapping read as %+v", mapping)
	}
	if _, declared, err := semconv.EquirectOf(map[string]string{
		semconv.KeyGeometryEquirectPx: "0,0,8192,4096",
	}); !declared || err == nil {
		t.Errorf("half a mapping passed: declared=%v err=%v", declared, err)
	}
}

// TestRegistryAgreesWithItsDocument holds the code registry and its prose
// twin to the same vocabulary: every registered key appears in the document's
// attribute table with the same entity and stability, and the document names
// no attribute the registry does not know. The generated single source
// arrives later; until then, this test is the agreement.
func TestRegistryAgreesWithItsDocument(t *testing.T) {
	doc, err := os.ReadFile("../../docs/semconv/REGISTRY.md")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `(atlas\\.[a-z0-9_.]+)` \\| (bundle|world|collection|feature) \\|.*\\| (stable|experimental) \\|")
	documented := make(map[string][2]string)
	for _, match := range row.FindAllStringSubmatch(string(doc), -1) {
		documented[match[1]] = [2]string{match[2], match[3]}
	}
	for _, key := range semconv.Keys() {
		entry, found := documented[key]
		if !found {
			t.Errorf("%s is registered but not documented", key)
			continue
		}
		entity, _ := semconv.EntityOf(key)
		stability, _ := semconv.StabilityOf(key)
		if entry[0] != string(entity) || entry[1] != string(stability) {
			t.Errorf("%s documented as %s/%s, registered as %s/%s",
				key, entry[0], entry[1], entity, stability)
		}
		delete(documented, key)
	}
	for key := range documented {
		t.Errorf("%s is documented but not registered", key)
	}
	// The document must also declare the version the code carries, so a
	// vocabulary break cannot land in one twin alone.
	if !strings.Contains(string(doc), "registry v2") || semconv.Version != 2 {
		t.Errorf("the document heads registry v2 while the code carries v%d", semconv.Version)
	}
}
