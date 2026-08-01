package semconv

import (
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestValidateHoldsTheVocabulary(t *testing.T) {
	good := []struct {
		entity Entity
		attrs  map[string]string
	}{
		{EntityCategory, map[string]string{KeyRenderAs: "pin"}},
		{EntityCategory, map[string]string{KeyRenderAs: "text", KeyIconStd: "maki/mountain"}},
		{EntityCategory, map[string]string{KeyIconKind: "glyph", KeyCategoryKey: "ripperdoc"}},
		{EntityMap, map[string]string{
			KeyGeometrySurface:     "sphere",
			KeyGeometryProjection:  "equirect",
			KeyGeometryEquirectPx:  "0,0,8192,4096",
			KeyGeometryEquirectDeg: "-180,90,180,-90",
			KeyGeometryBody:        "mars",
			KeyGeometryRadiusKM:    "3389.5",
		}},
		{EntityMap, map[string]string{KeyIconOutset: "dark"}},
		{EntityLocation, map[string]string{KeyGeoLat: "-42.4301", KeyGeoLon: "70.5025"}},
		// Empty is always fine: conventions are declared, never demanded.
		{EntityCategory, nil},
	}
	for _, test := range good {
		if err := Validate(test.entity, test.attrs); err != nil {
			t.Errorf("%s %v refused: %v", test.entity, test.attrs, err)
		}
	}

	bad := []struct {
		entity  Entity
		attrs   map[string]string
		mention string
	}{
		{EntityCategory, map[string]string{"atlas.render.like": "pin"}, "not registered"},
		{EntityCategory, map[string]string{KeyRenderAs: "zone"}, "not one of"},
		{EntityMap, map[string]string{KeyRenderAs: "pin"}, "attaches to"},
		{EntityCategory, map[string]string{KeyIconStd: "mountain"}, "set/name"},
		{EntityCategory, map[string]string{KeyIconStd: "Maki/Mountain"}, "slug"},
		{EntityMap, map[string]string{KeyGeometryEquirectPx: "0,0,8192"}, "4"},
		{EntityMap, map[string]string{KeyGeometryRadiusKM: "big"}, "number"},
		{EntityLocation, map[string]string{KeyGeoLat: "north"}, "number"},
		// The policy name never rides a payload.
		{EntityLocation, map[string]string{KeyNoteText: "words"}, "not registered"},
	}
	for _, test := range bad {
		err := Validate(test.entity, test.attrs)
		if err == nil || !strings.Contains(err.Error(), test.mention) {
			t.Errorf("%s %v: %v, wanted mention of %q", test.entity, test.attrs, err, test.mention)
		}
	}
}

func TestRenderAsPrefersAttributesOverLegacy(t *testing.T) {
	cases := []struct {
		attrs  map[string]string
		legacy string
		want   string
	}{
		{map[string]string{KeyRenderAs: "text"}, "markers", "text"},
		{map[string]string{KeyRenderAs: "pin"}, "text", "pin"},
		{nil, "text", "text"},
		{nil, "markers", "pin"},
		{nil, "", "pin"},
	}
	for _, test := range cases {
		if got := RenderAs(test.attrs, test.legacy); got != test.want {
			t.Errorf("RenderAs(%v, %q) = %q, want %q", test.attrs, test.legacy, got, test.want)
		}
	}
}

// TestEquirectRoundTrip drives the Mars mapping both ways: the declared
// window is the top half of the 8192-pixel world, and every corner and
// famous place must survive there and back within floating-point noise.
func TestEquirectRoundTrip(t *testing.T) {
	mapping, err := ParseEquirect("0,0,8192,4096", "-180,90,180,-90")
	if err != nil {
		t.Fatal(err)
	}
	places := [][2]float64{
		{90, -180}, {90, 180}, {-90, -180}, {-90, 180}, {0, 0},
		{18.6528, -133.8025}, // Olympus Mons
		{-42.4301, 70.5025},  // Hellas Planitia
		{-14.0059, -58.5877}, // Valles Marineris
	}
	for _, place := range places {
		x, y := mapping.Apply(place[0], place[1])
		lat, lon := mapping.Invert(x, y)
		if math.Abs(lat-place[0]) > 1e-9 || math.Abs(lon-place[1]) > 1e-9 {
			t.Errorf("%v round-trips to %v,%v via %v,%v", place, lat, lon, x, y)
		}
	}
	// And the anchor the whole design stands on: Olympus Mons lands on the
	// same pixel the trekmap translator computes.
	x, y := mapping.Apply(18.6528, -133.8025)
	wantX := (-133.8025 + 180) / 360 * 8192
	wantY := (90 - 18.6528) / 180 * 4096
	if math.Abs(x-wantX) > 1e-9 || math.Abs(y-wantY) > 1e-9 {
		t.Errorf("Olympus Mons at %v,%v, want %v,%v", x, y, wantX, wantY)
	}
}

func TestParseEquirectRefusesEmptyWindows(t *testing.T) {
	cases := [][2]string{
		{"0,0,0,4096", "-180,90,180,-90"},
		{"0,0,8192,4096", "-180,90,-180,-90"},
		{"0,0,8192", "-180,90,180,-90"},
	}
	for _, test := range cases {
		if _, err := ParseEquirect(test[0], test[1]); err == nil {
			t.Errorf("ParseEquirect(%q, %q) accepted", test[0], test[1])
		}
	}
}

// TestRegistryAgreesWithItsDocument holds semconv.go and REGISTRY.md to the
// same vocabulary: every registered key appears in the document's attribute
// table with the same entity and stability, and the document names no
// attribute the registry does not know.
func TestRegistryAgreesWithItsDocument(t *testing.T) {
	doc, err := os.ReadFile("REGISTRY.md")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `(atlas\\.[a-z_.]+)` \\| (bundle|map|category|location) \\|.*\\| (stable|experimental) \\|")
	documented := make(map[string][2]string)
	for _, match := range row.FindAllStringSubmatch(string(doc), -1) {
		documented[match[1]] = [2]string{match[2], match[3]}
	}
	for _, key := range Keys() {
		entry, found := documented[key]
		if !found {
			t.Errorf("%s is registered but not documented", key)
			continue
		}
		entity, _ := EntityOf(key)
		stability, _ := StabilityOf(key)
		if entry[0] != string(entity) || entry[1] != string(stability) {
			t.Errorf("%s documented as %s/%s, registered as %s/%s",
				key, entry[0], entry[1], entity, stability)
		}
		delete(documented, key)
	}
	for key := range documented {
		t.Errorf("%s is documented but not registered", key)
	}
}
