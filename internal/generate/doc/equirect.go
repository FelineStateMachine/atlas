package doc

import (
	"math"
	"strconv"
)

// The whole-sphere equirectangular window: how a planet's global mosaic sits in
// the world square.
//
// A global equirectangular image is twice as wide as it is tall, so it fills
// the top half of the square: x runs the full width from longitude -180 to 180,
// y runs the top half from latitude 90 down to -90. Like the synthetic window
// above, this is the corpus's own convention rather than any one source's --
// NASA Trek's mosaics and the Blue Marble base map both flatten this way, and
// two sources describing one planet this way land on the same pixels, which is
// what lets their readings meet. The world declares the mapping through the
// registered conventions (atlas.geometry.equirect.px/.deg), spelled from these
// same constants, so the raster and the declaration cannot disagree.
const (
	// EquirectPx is the raster window the projection fills, as
	// atlas.geometry.equirect.px spells it: x, y, w, h in world pixels.
	EquirectPx = "0,0,8192,4096"
	// EquirectDeg is the ground that window pictures, as
	// atlas.geometry.equirect.deg spells it: west, north, east, south.
	EquirectDeg = "-180,90,180,-90"
)

// EquirectCenter is where a reader opens on a whole-sphere world: the centre of
// the planet's picture, halfway across the world square and a quarter down it,
// which is where a 2:1 image's middle sits in a square.
func EquirectCenter() Position {
	return SyntheticPosition(SyntheticWorldSize/2, SyntheticWorldSize/4)
}

// EquirectLevelExtent reports the last tile column and row of one square level
// of a whole-sphere pyramid: the full width of the world square, and the top
// half of its height. A crawler captures exactly these tiles and the deriver
// expects exactly these bounds, because both ask here.
func EquirectLevelExtent(zoom int) (maxX, maxY int) {
	if zoom == 0 {
		return 0, 0
	}
	return 1<<zoom - 1, 1<<(zoom-1) - 1
}

// EquirectFrame declares a whole-sphere pyramid for the deriver. Every level
// down to zero is declared: a level left unsaid would be measured against the
// corpus's shared square, which this world does not sit in, and the half-height
// windows are what tell frame discovery where the planet actually is. Every
// mosaic of a body shares that window whatever its depth, which is what lets
// siblings ride one world as its lenses.
func EquirectFrame(maxZoom int, format string) *Frame {
	if format == "" {
		format = "jpg"
	}
	frame := &Frame{
		MaxZoom: maxZoom,
		Format:  format,
		Windows: make(map[string]TileWindow, maxZoom+1),
	}
	for zoom := 0; zoom <= maxZoom; zoom++ {
		maxX, maxY := EquirectLevelExtent(zoom)
		frame.Windows[strconv.Itoa(zoom)] = TileWindow{MaxX: maxX, MaxY: maxY}
	}
	return frame
}

// EquirectWorldPixel lands planetary coordinates on the picture. The window
// spans longitude -180..180 west to east and latitude 90..-90 top to bottom; a
// longitude in an east-positive 0..360 spelling wraps into that half-open
// window first. X crosses the full world square, y only its top half, because
// that is where a 2:1 image sits in a square.
func EquirectWorldPixel(longitude, latitude float64) (x, y float64) {
	wrapped := math.Mod(longitude+180, 360) - 180
	x = (wrapped + 180) / 360 * SyntheticWorldSize
	y = (90 - latitude) / 180 * (SyntheticWorldSize / 2)
	return x, y
}
