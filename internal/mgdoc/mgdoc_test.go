package mgdoc

import (
	"hash/fnv"
	"math"
	"testing"
)

func TestClaimRefusesAnOccupiedID(t *testing.T) {
	space := NewIDSpace()
	hash := fnv.New32a()
	_, _ = hash.Write([]byte("some-seed"))
	occupied := int64(hash.Sum32() & 0x7fffffff)
	space.seen[occupied] = "already-here"
	if _, err := space.Claim("some-seed"); err == nil {
		t.Fatal("claiming an occupied id did not fail")
	}
}

func TestClaimStaysWithinPositiveInt31(t *testing.T) {
	space := NewIDSpace()
	for _, seed := range []string{"a", "b", "some:longer:seed", "ign:marker:x"} {
		id, err := space.Claim(seed)
		if err != nil {
			t.Fatal(err)
		}
		if id <= 0 || id > math.MaxInt32 {
			t.Fatalf("Claim(%q) = %d, outside the positive int31 range", seed, id)
		}
	}
}

// TestSyntheticCoordinatesInvertTheViewer projects the synthetic coordinates
// with the viewer's own formula and expects the original pixel back.
func TestSyntheticCoordinatesInvertTheViewer(t *testing.T) {
	for _, pixel := range [][2]float64{{0, 0}, {4096, 4096}, {8192, 8192}, {123.5, 7000.25}} {
		latitude := SyntheticLatitude(pixel[1])
		longitude := SyntheticLongitude(pixel[0])
		worldTiles := math.Pow(2, SourceZoom)
		x := ((longitude + 180) / 360) * worldTiles * TileSize
		y := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * worldTiles * TileSize
		if math.Abs(x-pixel[0]) > 1e-6 || math.Abs(y-pixel[1]) > 1e-6 {
			t.Fatalf("pixel %v round-tripped to %.8f,%.8f", pixel, x, y)
		}
	}
}

func TestSpellOut(t *testing.T) {
	cases := map[string]string{
		"night-city":           "Night City",
		"ncpd-scanner-hustles": "Ncpd Scanner Hustles",
		"collectibles":         "Collectibles",
	}
	for slug, want := range cases {
		if got := SpellOut(slug); got != want {
			t.Errorf("SpellOut(%q) = %q, want %q", slug, got, want)
		}
	}
}
