package arcgismap

import (
	"fmt"
	"math"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// The membership join. A capture that carries the national subwatersheds
// can say, of each zone the city itself curated, which subwatershed its
// ground lies in -- computed at translate time from the captured polygons,
// so re-curating the sentence never needs a re-fetch and the capture stays
// the one unit of versioning. The claim is made only when all of the
// zone's sampled ground agrees on one answer: a zone that straddles
// subwatersheds says nothing rather than something misleading, which is
// the lesson the excised zoning-rules join left behind -- one earned
// sentence beats a wall of uncorrelated prose.

// hydroUnit is one subwatershed as captured: its code, its name, and its
// ground in true degrees.
type hydroUnit struct {
	code  string
	name  string
	rings [][][][]float64
}

// hydroIndex answers which subwatershed a point lies in.
type hydroIndex struct {
	units []hydroUnit
}

// buildHydroIndex reads the captured subwatersheds, if the capture carries
// them; nil means the capture predates the national enrichment and no zone
// makes any claim.
func buildHydroIndex(capture *Capture) *hydroIndex {
	for _, dataset := range capture.Datasets {
		if dataset.Slug != SlugSubwatersheds {
			continue
		}
		index := &hydroIndex{}
		for _, feature := range dataset.Features {
			code, name := feature.Fields["huc12"], feature.Fields["name"]
			if code == "" || name == "" || feature.Geometry.Type != GeometryRings {
				continue
			}
			index.units = append(index.units, hydroUnit{
				code: code, name: name, rings: feature.Geometry.Rings,
			})
		}
		return index
	}
	return nil
}

// locate answers the subwatershed holding a point, if any does. Units
// arrive normalized, so ties cannot happen except on shared boundaries,
// where the first unit in capture order answers deterministically.
func (h *hydroIndex) locate(lon, lat float64) (hydroUnit, bool) {
	if h == nil {
		return hydroUnit{}, false
	}
	for _, unit := range h.units {
		if pointInRings(lon, lat, unit.rings) {
			return unit, true
		}
	}
	return hydroUnit{}, false
}

// pointInRings is even-odd ray casting over MultiPolygon nesting: crossing
// an outer ring enters ground, crossing a hole leaves it again.
func pointInRings(lon, lat float64, polygons [][][][]float64) bool {
	inside := false
	for _, polygon := range polygons {
		for _, ring := range polygon {
			for at := 0; at+1 < len(ring); at++ {
				a, b := ring[at], ring[at+1]
				if len(a) < 2 || len(b) < 2 {
					continue
				}
				if (a[1] > lat) == (b[1] > lat) {
					continue
				}
				crossing := a[0] + (lat-a[1])/(b[1]-a[1])*(b[0]-a[0])
				if lon < crossing {
					inside = !inside
				}
			}
		}
	}
	return inside
}

// featureAnchor is one feature's representative point in true degrees: the
// centroid of its largest outer ring, or the middle vertex of its longest
// line. A point feature is its own anchor, though no point dataset zones.
func featureAnchor(g Geometry) (lon, lat float64, ok bool) {
	switch g.Type {
	case GeometryPoint:
		if len(g.Point) < 2 {
			return 0, 0, false
		}
		return g.Point[0], g.Point[1], true
	case GeometryRings:
		best := 0.0
		for _, polygon := range g.Rings {
			if len(polygon) == 0 {
				continue
			}
			x, y, area, held := ringCentroid(polygon[0])
			if held && area > best {
				best, lon, lat, ok = area, x, y, true
			}
		}
		return lon, lat, ok
	case GeometryLines:
		var longest []float64
		best := 0.0
		for _, line := range g.Lines {
			length := 0.0
			for at := 0; at+1 < len(line); at++ {
				if len(line[at]) < 2 || len(line[at+1]) < 2 {
					continue
				}
				length += math.Hypot(line[at+1][0]-line[at][0], line[at+1][1]-line[at][1])
			}
			if length > best && len(line) > 0 {
				best, longest = length, line[len(line)/2]
			}
		}
		if len(longest) < 2 {
			return 0, 0, false
		}
		return longest[0], longest[1], true
	}
	return 0, 0, false
}

// ringCentroid is the shoelace centroid of one ring, with the enclosed
// area to rank rings by.
func ringCentroid(ring [][]float64) (lon, lat, area float64, ok bool) {
	var doubled, sumX, sumY float64
	for at := 0; at+1 < len(ring); at++ {
		a, b := ring[at], ring[at+1]
		if len(a) < 2 || len(b) < 2 {
			return 0, 0, 0, false
		}
		cross := a[0]*b[1] - b[0]*a[1]
		doubled += cross
		sumX += (a[0] + b[0]) * cross
		sumY += (a[1] + b[1]) * cross
	}
	if doubled == 0 {
		return 0, 0, 0, false
	}
	return sumX / (3 * doubled), sumY / (3 * doubled), math.Abs(doubled) / 2, true
}

// hydroClaims tracks, per zone, the subwatersheds its features' ground
// lands in, and whether any of it landed nowhere.
type hydroClaims struct {
	codes  map[string]bool
	missed bool
}

func (c *hydroClaims) observe(index *hydroIndex, g Geometry) {
	for _, position := range claimPositions(g) {
		unit, found := index.locate(position[0], position[1])
		if !found {
			c.missed = true
			continue
		}
		if c.codes == nil {
			c.codes = map[string]bool{}
		}
		c.codes[unit.code] = true
	}
}

// claimSamples is how many boundary positions a feature volunteers beside
// its anchor. The anchor alone would let a zone made of one citywide
// polygon claim whichever subwatershed its centroid happens to sit in;
// sampling the boundary catches the spread, and the cost stays flat no
// matter how detailed the ring.
const claimSamples = 16

// claimPositions is the ground a feature answers for: its anchor, and an
// even sample of its outer-ring or line vertices.
func claimPositions(g Geometry) [][2]float64 {
	var out [][2]float64
	if lon, lat, ok := featureAnchor(g); ok {
		out = append(out, [2]float64{lon, lat})
	}
	var boundary [][]float64
	switch g.Type {
	case GeometryRings:
		for _, polygon := range g.Rings {
			if len(polygon) > 0 {
				boundary = append(boundary, polygon[0]...)
			}
		}
	case GeometryLines:
		for _, line := range g.Lines {
			boundary = append(boundary, line...)
		}
	}
	step := max((len(boundary)+claimSamples-1)/claimSamples, 1)
	for at := 0; at < len(boundary); at += step {
		if len(boundary[at]) >= 2 {
			out = append(out, [2]float64{boundary[at][0], boundary[at][1]})
		}
	}
	return out
}

// apply writes the membership onto a zone when every feature agreed on one
// subwatershed: a short sentence for the card, and the code as the
// machine-readable key beside it.
func (c *hydroClaims) apply(index *hydroIndex, zone *mgdoc.Region) {
	if c.missed || len(c.codes) != 1 {
		return
	}
	var code string
	for held := range c.codes {
		code = held
	}
	for _, unit := range index.units {
		if unit.code != code {
			continue
		}
		zone.Description = scrub(fmt.Sprintf("Lies in the %s subwatershed (HUC %s).", unit.name, unit.code))
		if zone.Attrs == nil {
			zone.Attrs = map[string]string{}
		}
		zone.Attrs[semconv.KeyHydroHUC12] = unit.code
		return
	}
}
