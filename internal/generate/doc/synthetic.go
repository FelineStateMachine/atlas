package doc

import "math"

// Synthetic coordinates: how a source that publishes a picture rather than a
// planet says where something is.
//
// A document's positions are latitude and longitude in the volume's own
// projection (see the package comment). For a real planet those are the real
// thing. For a picture -- a game's sheet, a rendered city, a flattened mosaic --
// the ground is an image, and what a source knows about a feature is which pixel
// of that image it sits on. These functions turn a pixel into the position that
// projects back onto exactly that pixel, by inverting the same spherical
// Mercator every reader uses.
//
// The window is the corpus's own: a 32-tile square, 8192 pixels across, whose
// pyramid comes to rest in a single tile. It is not the format's -- format v3
// carries whatever window a volume declares -- and it is not any one source's
// either, which is why it lives here rather than in a source directory. A source
// publishing a picture measures in these pixels; nothing downstream has to know
// that the degrees were once pixels, because the projection is the same one it
// would have applied anyway.
//
// Two sources describing the same ground this way land on the same numbers,
// which is what lets a merge compare them. A source whose ground is a planet
// ignores all of this and publishes what it knows.
const (
	// SyntheticZoom is the tile zoom whose pixels are the world's units: the
	// square is 32 tiles across, and 2^5 is 32.
	SyntheticZoom = 5
	// SyntheticTileSize is the edge of one tile, in pixels.
	SyntheticTileSize = 256
	// SyntheticWorldSize is the edge of the world square, in pixels.
	SyntheticWorldSize = SyntheticTileSize << SyntheticZoom
)

// SyntheticPosition is where a pixel of a source's own picture stands, measured
// from the top left of the world square.
func SyntheticPosition(x, y float64) Position {
	return Position{Lat: SyntheticLat(y), Lng: SyntheticLng(x)}
}

// SyntheticLng inverts the reader's projection across the square.
func SyntheticLng(x float64) float64 {
	worldTiles := math.Pow(2, SyntheticZoom)
	return ((x/SyntheticTileSize)/worldTiles)*360 - 180
}

// SyntheticLat inverts the reader's projection down the square.
func SyntheticLat(y float64) float64 {
	worldTiles := math.Pow(2, SyntheticZoom)
	yTile := y / SyntheticTileSize
	return math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
}

// Title spells a slug out for a person, which is all the naming some sources
// give a thing: "night-city" becomes "Night City".
func Title(slug string) string {
	out := []rune(slug)
	capitalize := true
	for index, r := range out {
		switch {
		case r == '-':
			out[index] = ' '
			capitalize = true
		case capitalize && r >= 'a' && r <= 'z':
			out[index] = r - ('a' - 'A')
			capitalize = false
		default:
			capitalize = false
		}
	}
	return string(out)
}

// Slugify spells a title the way an artwork key or a world slug is spelled:
// lower case, everything else run together as a single hyphen.
func Slugify(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+'a'-'A')
		default:
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
