package arcgishub

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The coordinate design, which is the whole reason a city can be a volume.
//
// A city sits off the Web-Mercator diagonal and the reader's window is a square
// upon it, so the city's bounding box -- padded, then squared in Mercator's own
// terms -- becomes the world square. A feature's true coordinates land on that
// square through the Mercator forward, and the pixel becomes a synthetic
// position through the same inverse every picture-publishing source uses. The
// distortion cancels exactly, because the rendered basemap and the features ride
// one mapping.
//
// Nothing is flattened away by it. The world declares the mapping as registered
// conventions, so any reader can run a packed position backward through it and
// stand on the ground, and every pin keeps the coordinates the city published
// beside its synthetic ones.

// window is the ground the world square pictures: the degrees at the padded
// square's edges. The square is square in Mercator's own terms -- u linear in
// longitude, v in projected latitude -- which is what lets one number serve as
// both axes' scale.
type window struct {
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
	South float64 `json:"south"`
}

// mercator lands degrees on the global Web-Mercator unit square.
func mercator(lon, lat float64) (u, v float64) {
	u = (lon + 180) / 360
	v = (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2
	return u, v
}

// degrees is the way back.
func degrees(u, v float64) (lon, lat float64) {
	lon = u*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*v))) * 180 / math.Pi
	return lon, lat
}

// cityWindow pads a bounding box five percent per side and squares it in
// Mercator terms, growing the shorter axis around its own middle. The same box
// always makes the same window, which is what keeps every capture of a city in
// one world, and it is why the crawler and the reader can both compute it
// rather than the capture having to carry it.
func cityWindow(bbox [4]float64) window {
	u0, v0 := mercator(bbox[0], bbox[3]) // west, north: the top-left
	u1, v1 := mercator(bbox[2], bbox[1]) // east, south: the bottom-right
	padU, padV := (u1-u0)*0.05, (v1-v0)*0.05
	u0, u1 = u0-padU, u1+padU
	v0, v1 = v0-padV, v1+padV
	side := math.Max(u1-u0, v1-v0)
	uMid, vMid := (u0+u1)/2, (v0+v1)/2
	u0, u1 = uMid-side/2, uMid+side/2
	v0, v1 = vMid-side/2, vMid+side/2
	west, north := degrees(u0, v0)
	east, south := degrees(u1, v1)
	return window{West: west, North: north, East: east, South: south}
}

// worldPixel lands true coordinates on the world square.
func (w window) worldPixel(lon, lat float64) (x, y float64) {
	u0, v0 := mercator(w.West, w.North)
	u1, _ := mercator(w.East, w.South)
	side := u1 - u0
	u, v := mercator(lon, lat)
	return (u - u0) / side * doc.SyntheticWorldSize, (v - v0) / side * doc.SyntheticWorldSize
}

// pxDeg spells the window as the two world attributes that declare it: the
// raster window the cut fills, and the ground it pictures.
func (w window) pxDeg() (px, deg string) {
	px = fmt.Sprintf("0,0,%d,%d", doc.SyntheticWorldSize, doc.SyntheticWorldSize)
	parts := []string{
		strconv.FormatFloat(w.West, 'f', -1, 64),
		strconv.FormatFloat(w.North, 'f', -1, 64),
		strconv.FormatFloat(w.East, 'f', -1, 64),
		strconv.FormatFloat(w.South, 'f', -1, 64),
	}
	return px, strings.Join(parts, ",")
}

// syntheticRing is one position's whole journey: true degrees to world pixel to
// the [longitude, latitude] pair a shape's coordinates speak.
func (w window) syntheticRing(lon, lat float64) []float64 {
	x, y := w.worldPixel(lon, lat)
	return []float64{doc.SyntheticLng(x), doc.SyntheticLat(y)}
}

// TileSetPath is the path a city's rendered basemap is captured under. It is
// exported because the renderer writes tiles at it and the reader names it, and
// the two have to agree. The capture day is part of it: every dated world
// renders its own basemap, and a tile set is told from its neighbours by this
// path alone.
func TileSetPath(city, day string) string { return city + "/" + day + "/basemap" }
