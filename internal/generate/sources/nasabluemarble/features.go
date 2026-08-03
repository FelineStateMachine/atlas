package nasabluemarble

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The vector half of the volume: country borders as one quiet area collection,
// and the primary capitals as one point collection per continent. The
// arrangement is the capture's own -- continents are the publication's filing,
// capitals ride in the publication's order -- and the one judgement made here
// is a join: a capital whose country is not drawn at this scale still belongs
// to a continent, and the nearest drawn ground says which.

// capitalIcon is the library glyph every capital collection names, resolved to
// embedded bytes by composition.
const capitalIcon = "maki/marker"

// collectionsOf arranges the capture's features the way the legend reads:
// capitals first -- one collection per continent, sorted, under one heading --
// and the ground they stand on after, as a single quiet area collection.
func collectionsOf(raw *capture, ids *doc.IDSpace) ([]doc.Collection, error) {
	countryIDs := make(map[string]int64, len(raw.Features.Countries))
	for _, entry := range raw.Features.Countries {
		id, err := ids.Claim("ne:country:" + entry.A3)
		if err != nil {
			return nil, err
		}
		countryIDs[entry.A3] = id
	}

	byContinent := make(map[string][]doc.Feature)
	var order []string
	for _, entry := range raw.Features.Capitals {
		continent := continentOf(entry, raw.Features.Countries)
		id, err := ids.Claim("ne:capital:" + entry.A3 + ":" + doc.Slugify(entry.Name))
		if err != nil {
			return nil, err
		}
		at := worldPoint(entry.Lon, entry.Lat)
		if _, seen := byContinent[continent]; !seen {
			order = append(order, continent)
		}
		byContinent[continent] = append(byContinent[continent], doc.Feature{
			ID:       id,
			Title:    entry.Name,
			Subtitle: entry.Country,
			At:       &at,
			// The pin stands on its country's ground where that ground is
			// drawn; a capital whose country is below this scale stands on
			// open map, member zero.
			Member: countryIDs[entry.A3],
			// The coordinates as the publication gives them, verbatim.
			Attrs: map[string]string{
				semconv.KeyGeoLat: strconv.FormatFloat(entry.Lat, 'f', -1, 64),
				semconv.KeyGeoLon: strconv.FormatFloat(entry.Lon, 'f', -1, 64),
			},
		})
	}
	sort.Strings(order)

	out := make([]doc.Collection, 0, len(order)+1)
	for _, continent := range order {
		id, err := ids.Claim("ne:capitals:" + doc.Slugify(continent))
		if err != nil {
			return nil, err
		}
		out = append(out, doc.Collection{
			ID:      id,
			Title:   continent,
			Group:   "Capitals",
			Kind:    doc.KindPoint,
			Visible: true,
			Attrs: map[string]string{
				semconv.KeyRenderAs: semconv.RenderAsPin,
				semconv.KeyIconStd:  capitalIcon,
			},
			Features: byContinent[continent],
		})
	}

	countries, err := countriesCollection(raw, ids, countryIDs)
	if err != nil {
		return nil, err
	}
	return append(out, countries), nil
}

// countriesCollection is the ground: every country's rings as one area
// collection, labels quiet so the picture stays a picture until a border is
// asked about.
func countriesCollection(raw *capture, ids *doc.IDSpace, countryIDs map[string]int64) (doc.Collection, error) {
	id, err := ids.Claim("ne:countries")
	if err != nil {
		return doc.Collection{}, err
	}
	collection := doc.Collection{
		ID:      id,
		Title:   "Countries",
		Kind:    doc.KindArea,
		Visible: true,
		Attrs: map[string]string{
			semconv.KeyLabelPolicy: semconv.LabelQuiet,
		},
		Features: make([]doc.Feature, 0, len(raw.Features.Countries)),
	}
	for _, entry := range raw.Features.Countries {
		geometry, err := worldGeometry(entry.Polygons)
		if err != nil {
			return doc.Collection{}, fmt.Errorf("country %q: %w", entry.Name, err)
		}
		center := worldPoint(entry.LabelLon, entry.LabelLat)
		collection.Features = append(collection.Features, doc.Feature{
			ID:       countryIDs[entry.A3],
			Title:    entry.Name,
			Center:   &center,
			Geometry: []doc.Geometry{geometry},
			Attrs: map[string]string{
				semconv.KeyGeoLat: strconv.FormatFloat(entry.LabelLat, 'f', -1, 64),
				semconv.KeyGeoLon: strconv.FormatFloat(entry.LabelLon, 'f', -1, 64),
			},
		})
	}
	return collection, nil
}

// worldPoint lands published coordinates on the picture. Unlike the shared
// EquirectWorldPixel it does not wrap: the publication speaks -180..180 and
// splits its rings at the antimeridian, so the mapping is linear and a vertex
// pinned to either edge of the world stays on its own edge instead of folding
// onto the other.
func worldPoint(lon, lat float64) doc.Position {
	x := (lon + 180) / 360 * doc.SyntheticWorldSize
	y := (90 - lat) / 180 * (doc.SyntheticWorldSize / 2)
	return doc.SyntheticPosition(x, y)
}

// worldGeometry converts a country's rings into the volume's own space, as one
// MultiPolygon. Positions are rounded to a millionth of a degree -- far inside
// a world pixel -- so the payload does not spend fifteen digits saying what
// six already say.
func worldGeometry(polygons [][][][2]float64) (doc.Geometry, error) {
	if len(polygons) == 0 {
		return doc.Geometry{}, fmt.Errorf("draws no ground")
	}
	converted := make([][][][2]float64, len(polygons))
	for p, polygon := range polygons {
		if len(polygon) == 0 {
			return doc.Geometry{}, fmt.Errorf("draws an empty polygon")
		}
		converted[p] = make([][][2]float64, len(polygon))
		for r, ring := range polygon {
			converted[p][r] = make([][2]float64, len(ring))
			for v, vertex := range ring {
				at := worldPoint(vertex[0], vertex[1])
				converted[p][r][v] = [2]float64{rounded(at.Lng), rounded(at.Lat)}
			}
		}
	}
	coordinates, err := json.Marshal(converted)
	if err != nil {
		return doc.Geometry{}, err
	}
	return doc.Geometry{Type: "MultiPolygon", Coordinates: coordinates}, nil
}

func rounded(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}

// continentOf answers which continent a capital belongs to: its country's
// filing when the country is drawn at this scale, and the nearest drawn
// ground's otherwise. Twenty-nine capitals are microstates the 1:110m borders
// leave out -- Singapore, Monaco, the island states -- and the nearest vertex
// of any drawn country decides for them, computed from the captured polygons
// at translate time the way the city source computes its watershed join.
func continentOf(entry capital, countries []country) string {
	for _, held := range countries {
		if held.A3 == entry.A3 {
			return held.Continent
		}
	}
	nearest, best := "", math.Inf(1)
	scale := math.Cos(entry.Lat * math.Pi / 180)
	for _, held := range countries {
		for _, polygon := range held.Polygons {
			for _, ring := range polygon {
				for _, vertex := range ring {
					dLon := math.Abs(vertex[0] - entry.Lon)
					if dLon > 180 {
						dLon = 360 - dLon
					}
					dLat := vertex[1] - entry.Lat
					distance := dLat*dLat + dLon*scale*dLon*scale
					if distance < best {
						best, nearest = distance, held.Continent
					}
				}
			}
		}
	}
	return nearest
}
