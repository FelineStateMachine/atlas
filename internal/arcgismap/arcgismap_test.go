package arcgismap

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// The window must be square in Mercator's own terms, or one scale cannot
// serve both axes.
func TestCityWindowIsSquare(t *testing.T) {
	window := CityWindow(bend.BBox)
	u0, v0 := mercator(window.West, window.North)
	u1, v1 := mercator(window.East, window.South)
	du, dv := u1-u0, v1-v0
	if math.Abs(du-dv) > 1e-12 {
		t.Fatalf("window is not square: du=%v dv=%v", du, dv)
	}
	if du <= 0 {
		t.Fatalf("window has no ground: du=%v", du)
	}
	// The curated box sits inside its padded window.
	if window.West >= bend.BBox[0] || window.East <= bend.BBox[2] {
		t.Fatalf("window %+v does not contain the bbox longitudes", window)
	}
	if window.South >= bend.BBox[1] || window.North <= bend.BBox[3] {
		t.Fatalf("window %+v does not contain the bbox latitudes", window)
	}
}

// A feature's true coordinates, through the window and the synthetic
// spelling, must land back on the same world pixel the viewer will project
// them to -- the whole trick, held to a hundredth of a pixel.
func TestWorldPixelRoundTripsThroughSyntheticCoordinates(t *testing.T) {
	window := CityWindow(bend.BBox)
	points := [][2]float64{
		{-121.30810509, 44.05813637}, // a real historic site
		{-121.4180, 43.9500},         // the bbox corners
		{-121.2170, 44.1650},
	}
	for _, point := range points {
		x, y := window.WorldPixel(point[0], point[1])
		lng := mgdoc.SyntheticLongitude(x)
		lat := mgdoc.SyntheticLatitude(y)
		backX := mgdoc.ProjectX(lng, mgdoc.SourceZoom, 0)
		backY := mgdoc.ProjectY(lat, mgdoc.SourceZoom, 0)
		if math.Abs(backX-x) > 0.01 || math.Abs(backY-y) > 0.01 {
			t.Fatalf("pixel (%v,%v) round-trips to (%v,%v)", x, y, backX, backY)
		}
	}
}

// The declared mercator.deg mapping and WorldPixel must be the same
// transform: a pin's world pixel, inverted through the declared window
// edges, recovers the published coordinates to well under a meter.
func TestDeclaredWindowInvertsToTrueCoordinates(t *testing.T) {
	window := CityWindow(bend.BBox)
	lon, lat := -121.3153, 44.0582
	x, y := window.WorldPixel(lon, lat)
	u0, v0 := mercator(window.West, window.North)
	u1, _ := mercator(window.East, window.South)
	side := u1 - u0
	backLon, backLat := degrees(u0+x/mgdoc.WorldSize*side, v0+y/mgdoc.WorldSize*side)
	if math.Abs(backLon-lon) > 1e-9 || math.Abs(backLat-lat) > 1e-9 {
		t.Fatalf("(%v,%v) inverts to (%v,%v)", lon, lat, backLon, backLat)
	}
}

func fixtureCapture() Capture {
	window := CityWindow(bend.BBox)
	point := func(lon, lat float64) Geometry {
		return Geometry{Type: GeometryPoint, Point: []float64{lon, lat}}
	}
	square := func(lon, lat, size float64) Geometry {
		return Geometry{Type: GeometryRings, Rings: [][][][]float64{{{
			{lon, lat}, {lon + size, lat}, {lon + size, lat + size}, {lon, lat + size}, {lon, lat},
		}}}}
	}
	line := func(positions ...[2]float64) Geometry {
		one := make([][]float64, 0, len(positions))
		for _, position := range positions {
			one = append(one, []float64{position[0], position[1]})
		}
		return Geometry{Type: GeometryLines, Lines: [][][]float64{one}}
	}
	return Capture{
		Source:  Source,
		City:    "bend-or",
		Title:   "Bend, Oregon",
		MapSlug: "2026-08-01",
		Window:  window,
		Basemap: MapConfig{MaxZoom: 3, Extension: "png"},
		Datasets: []CapturedDataset{
			{Slug: "historic-sites", Features: []Feature{
				{ID: 1, Fields: Fields{"NAME": "A.C. Lucas House", "TAB_NAME": "Residential", "SHORT_DESC": "42 NW Hawthorne Ave", "DESC1": "Constructed: 1910"}, Geometry: point(-121.3081, 44.0581)},
				{ID: 9, Fields: Fields{"NAME": "Drake Park Gazebo", "TAB_NAME": "Civic", "SHORT_DESC": "Drake Park"}, Geometry: point(-121.315, 44.06)},
			}},
			{Slug: "zoning", Features: []Feature{
				{ID: 1, Fields: Fields{"ZONE": "PF"}, Geometry: square(-121.31, 44.05, 0.004)},
				{ID: 2, Fields: Fields{"ZONE": "RS"}, Geometry: square(-121.30, 44.06, 0.002)},
				{ID: 3, Fields: Fields{"ZONE": "PF"}, Geometry: square(-121.28, 44.08, 0.003)},
				// A second RS parcel in the other subwatershed: the RS zone
				// straddles, and must say nothing about membership.
				{ID: 4, Fields: Fields{"ZONE": "RS"}, Geometry: square(-121.36, 44.135, 0.002)},
			}},
			{Slug: "annexations", Features: []Feature{
				{ID: 3, Fields: Fields{"DESCRIPT": "Rockridge Park", "EFF_DATE": "1556866800000", "ORDIN_NO": "NS-2327"}, Geometry: square(-121.29, 44.09, 0.002)},
			}},
			{Slug: "wetlands", Features: []Feature{
				{ID: 1, Fields: Fields{"TYPE": "Significant Identified Wetlands", "MAP_CODE": "R9"}, Geometry: square(-121.32, 44.04, 0.003)},
			}},
			{Slug: "trails", Features: []Feature{
				{ID: 6, Fields: Fields{"Trail_Name": "Riley Ranch", "Park": "Riley Ranch Nature Reserve", "Status": "Existing"}, Geometry: line([2]float64{-121.35, 44.14}, [2]float64{-121.345, 44.145})},
				{ID: 7, Fields: Fields{"Trail_Name": "Planned Spur", "Status": "Proposed"}, Geometry: line([2]float64{-121.33, 44.02}, [2]float64{-121.329, 44.021})},
			}},
			{Slug: "mpo-boundary", Features: []Feature{
				{ID: 21, Fields: Fields{"LABEL": "MPO BOUNDARY"}, Geometry: square(-121.40, 43.97, 0.17)},
			}},
			// The national enrichment, city-sized: two subwatersheds that
			// between them hold most of the city fixtures -- but not the MPO
			// boundary's corners, so that zone's ground lands nowhere and it
			// claims nothing.
			{Slug: "watersheds", Features: []Feature{
				{ID: 1707030101, Fields: Fields{"huc10": "1707030101", "name": "Tumalo Creek"}, Geometry: square(-121.40, 44.00, 0.15)},
			}},
			{Slug: "subwatersheds", Features: []Feature{
				{ID: 170703010101, Fields: Fields{"huc12": "170703010101", "name": "Tumalo Creek"}, Geometry: square(-121.37, 44.03, 0.10)},
				{ID: 170703010102, Fields: Fields{"huc12": "170703010102", "name": "Bull Creek"}, Geometry: square(-121.37, 44.131, 0.03)},
			}},
			{Slug: "streams", Features: []Feature{
				{ID: 51, Fields: Fields{"gnis_name": "Tumalo Creek"}, Geometry: line([2]float64{-121.34, 44.05}, [2]float64{-121.30, 44.07})},
			}},
			{Slug: "waterbodies", Features: []Feature{
				{ID: 61, Fields: Fields{"GNIS_NAME": "Mirror Pond"}, Geometry: square(-121.316, 44.057, 0.002)},
				// An unnamed pond draws into the basemap but earns no zone.
				{ID: 62, Geometry: square(-121.29, 44.05, 0.001)},
			}},
		},
	}
}

func translated(t *testing.T, capture Capture) mgdoc.Map {
	t.Helper()
	raw, err := json.Marshal(&capture)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Translate(raw)
	if err != nil {
		t.Fatal(err)
	}
	var out mgdoc.Map
	if err := json.Unmarshal(doc, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTranslateShapesTheDocument(t *testing.T) {
	out := translated(t, fixtureCapture())

	if out.Slug != "2026-08-01" || out.Title != "2026-08-01" {
		t.Fatalf("map is %q titled %q, not the capture day", out.Slug, out.Title)
	}
	if out.Game.Slug != "bend-or" || out.Game.Title != "Bend, Oregon" {
		t.Fatalf("game is %q titled %q", out.Game.Slug, out.Game.Title)
	}

	if len(out.Config.TileSets) != 1 {
		t.Fatalf("map has %d tile sets", len(out.Config.TileSets))
	}
	set := out.Config.TileSets[0]
	if set.Path != "bend-or/2026-08-01/basemap" || set.Extension != "png" || set.MaxZoom != 3 {
		t.Fatalf("tile set is %+v", set)
	}
	for zoom := 0; zoom <= set.MaxZoom; zoom++ {
		bound, declared := set.Bounds[strconv.Itoa(zoom)]
		if !declared {
			t.Fatalf("level %d declares no bounds", zoom)
		}
		if last := 1<<zoom - 1; bound.X.Max != last || bound.Y.Max != last || bound.X.Min != 0 || bound.Y.Min != 0 {
			t.Fatalf("level %d bounds are %+v", zoom, bound)
		}
	}

	if err := semconv.Validate(semconv.EntityWorld, out.Attrs); err != nil {
		t.Fatalf("map attrs: %v", err)
	}
	if out.Attrs[semconv.KeyGeometrySurface] != semconv.SurfacePlane {
		t.Fatalf("surface is %q", out.Attrs[semconv.KeyGeometrySurface])
	}

	// One category per point dataset, gathered under curated group titles.
	if len(out.Groups) != 1 {
		t.Fatalf("document carries %d groups", len(out.Groups))
	}
	group := out.Groups[0]
	if group.Title != "Heritage" || len(group.Categories) != 1 {
		t.Fatalf("group is %q with %d categories", group.Title, len(group.Categories))
	}
	historic := group.Categories[0]
	if historic.Title != "Historic Resources" || len(historic.Locations) != 2 {
		t.Fatalf("category is %q with %d pins", historic.Title, len(historic.Locations))
	}
	if err := semconv.Validate(semconv.EntityCollection, historic.Attrs); err != nil {
		t.Fatalf("category attrs: %v", err)
	}
	pin := historic.Locations[0]
	if pin.Title != "A.C. Lucas House" {
		t.Fatalf("first pin is %q", pin.Title)
	}
	if pin.Description != "Residential · 42 NW Hawthorne Ave · Constructed: 1910" {
		t.Fatalf("pin card reads %q", pin.Description)
	}
	if err := semconv.Validate(semconv.EntityFeature, pin.Attrs); err != nil {
		t.Fatalf("pin attrs: %v", err)
	}
	if pin.Attrs[semconv.KeyGeoLat] != "44.0581" || pin.Attrs[semconv.KeyGeoLon] != "-121.3081" {
		t.Fatalf("pin keeps %q, %q", pin.Attrs[semconv.KeyGeoLat], pin.Attrs[semconv.KeyGeoLon])
	}

	// Zones: the boundary as one, zoning bucketed by code, annexations by
	// decade, wetlands by type, and only the existing named trail -- in
	// curated dataset order, with the national enrichment after them all.
	titles := make([]string, 0, len(out.Regions))
	for _, region := range out.Regions {
		titles = append(titles, region.Title)
	}
	want := []string{"MPO Boundary", "PF", "RS", "Annexed 2010–2019",
		"Significant Identified Wetlands", "Riley Ranch",
		"Tumalo Creek", "Tumalo Creek", "Bull Creek", "Tumalo Creek", "Mirror Pond"}
	if len(titles) != len(want) {
		t.Fatalf("zones are %v", titles)
	}
	for at, name := range want {
		if titles[at] != name {
			t.Fatalf("zones are %v, want %v", titles, want)
		}
	}

	// The national zones say which grain of the nation they are.
	if got := out.Regions[6].Subtitle; got != "Watershed · HUC 1707030101" {
		t.Fatalf("watershed subtitle is %q", got)
	}
	if got := out.Regions[7].Subtitle; got != "Subwatershed · HUC 170703010101" {
		t.Fatalf("subwatershed subtitle is %q", got)
	}
	if got := out.Regions[9].Subtitle; got != "Stream" {
		t.Fatalf("stream subtitle is %q", got)
	}
	if got := out.Regions[10].Subtitle; got != "Waterbody" {
		t.Fatalf("waterbody subtitle is %q", got)
	}

	// Membership: the PF zone's ground lies wholly in one subwatershed, so
	// it says so, once, in both spellings.
	pf := out.Regions[1]
	if pf.Description != "Lies in the Tumalo Creek subwatershed (HUC 170703010101)." {
		t.Fatalf("PF card reads %q", pf.Description)
	}
	if err := semconv.Validate(semconv.EntityFeature, pf.Attrs); err != nil {
		t.Fatalf("PF attrs: %v", err)
	}
	if pf.Attrs[semconv.KeyHydroHUC12] != "170703010101" {
		t.Fatalf("PF claims %q", pf.Attrs[semconv.KeyHydroHUC12])
	}
	// The RS zone straddles the two subwatersheds and says nothing; the MPO
	// boundary's corners land in no unit and it says nothing either; the
	// national zones never describe themselves.
	for _, at := range []int{2, 0, 7} {
		zone := out.Regions[at]
		if zone.Description != "" || zone.Attrs[semconv.KeyHydroHUC12] != "" {
			t.Fatalf("zone %q claims membership: %q %v", zone.Title, zone.Description, zone.Attrs)
		}
	}
	// A line zone in one subwatershed claims it alongside its stroke.
	riley := out.Regions[5]
	if riley.Attrs[semconv.KeyHydroHUC12] != "170703010102" ||
		riley.Attrs[semconv.KeyStrokeWidthPx] != "12" {
		t.Fatalf("trail attrs are %v", riley.Attrs)
	}
	if out.Regions[1].Subtitle != "Zoning" {
		t.Fatalf("PF subtitle is %q", out.Regions[1].Subtitle)
	}
	if len(out.Regions[1].Features) != 2 {
		t.Fatalf("PF gathers %d features", len(out.Regions[1].Features))
	}
	trail := out.Regions[5]
	if trail.Subtitle != "Riley Ranch Nature Reserve" || len(trail.Features) != 1 {
		t.Fatalf("trail zone is %+v", trail)
	}
	// A line zone stays the line it is, and declares the width it is drawn
	// at. The v2 wire spells the collection's stroke on the zone, so its two
	// keys validate under the entities the registry attaches them to.
	if err := semconv.Validate(semconv.EntityCollection,
		map[string]string{semconv.KeyStrokeWidthPx: trail.Attrs[semconv.KeyStrokeWidthPx]}); err != nil {
		t.Fatalf("trail stroke: %v", err)
	}
	if err := semconv.Validate(semconv.EntityFeature,
		map[string]string{semconv.KeyHydroHUC12: trail.Attrs[semconv.KeyHydroHUC12]}); err != nil {
		t.Fatalf("trail attrs: %v", err)
	}
	if trail.Attrs[semconv.KeyStrokeWidthPx] != "12" {
		t.Fatalf("trail declares width %q", trail.Attrs[semconv.KeyStrokeWidthPx])
	}
	if trail.Features[0].Geometry.Type != "MultiLineString" {
		t.Fatalf("trail geometry is %q", trail.Features[0].Geometry.Type)
	}
	var lineCoords [][][]float64
	if err := json.Unmarshal(trail.Features[0].Geometry.Coordinates, &lineCoords); err != nil {
		t.Fatal(err)
	}
	if len(lineCoords) != 1 || len(lineCoords[0]) != 2 {
		t.Fatalf("the two-point trail should stay one two-point line, got %v", lineCoords)
	}
	if _, stroked := out.Regions[1].Attrs[semconv.KeyStrokeWidthPx]; stroked {
		t.Fatalf("a polygon zone declares no stroke, got %v", out.Regions[1].Attrs)
	}

	// Every zone position must project back inside the world square.
	positions := func(geometry mgdoc.Geometry) [][]float64 {
		var out [][]float64
		if geometry.Type == "MultiLineString" {
			var lines [][][]float64
			if err := json.Unmarshal(geometry.Coordinates, &lines); err != nil {
				t.Fatal(err)
			}
			for _, line := range lines {
				out = append(out, line...)
			}
			return out
		}
		var polygons [][][][]float64
		if err := json.Unmarshal(geometry.Coordinates, &polygons); err != nil {
			t.Fatal(err)
		}
		for _, polygon := range polygons {
			for _, ring := range polygon {
				out = append(out, ring...)
			}
		}
		return out
	}
	for _, region := range out.Regions {
		for _, feature := range region.Features {
			for _, position := range positions(feature.Geometry) {
				x := mgdoc.ProjectX(position[0], mgdoc.SourceZoom, 0)
				y := mgdoc.ProjectY(position[1], mgdoc.SourceZoom, 0)
				if x < -1 || x > mgdoc.WorldSize+1 || y < -1 || y > mgdoc.WorldSize+1 {
					t.Fatalf("zone %q position projects to (%v,%v)", region.Title, x, y)
				}
			}
		}
	}
}

// The same data in any order makes byte-identical documents, which is what
// content-addressed snapshots require.
func TestTranslateIsDeterministic(t *testing.T) {
	capture := fixtureCapture()
	first, err := json.Marshal(&capture)
	if err != nil {
		t.Fatal(err)
	}
	shuffled := fixtureCapture()
	for at := range shuffled.Datasets {
		features := shuffled.Datasets[at].Features
		for i, j := 0, len(features)-1; i < j; i, j = i+1, j-1 {
			features[i], features[j] = features[j], features[i]
		}
	}
	shuffled.Datasets[0], shuffled.Datasets[len(shuffled.Datasets)-1] =
		shuffled.Datasets[len(shuffled.Datasets)-1], shuffled.Datasets[0]
	shuffled.Normalize()
	capture.Normalize()
	second, err := json.Marshal(&capture)
	if err != nil {
		t.Fatal(err)
	}
	normalizedShuffled, err := json.Marshal(&shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second, normalizedShuffled) {
		t.Fatal("normalized captures differ by input order")
	}
	docA, err := Translate(first)
	if err != nil {
		t.Fatal(err)
	}
	docB, err := Translate(normalizedShuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(docA, docB) {
		t.Fatal("translation differs by input order")
	}
}

func TestTranslateRefusals(t *testing.T) {
	refuse := func(name string, mutate func(*Capture)) {
		t.Helper()
		capture := fixtureCapture()
		mutate(&capture)
		raw, err := json.Marshal(&capture)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Translate(raw); err == nil {
			t.Fatalf("%s: translated anyway", name)
		}
	}
	refuse("wrong source", func(c *Capture) { c.Source = "mapgenie" })
	refuse("uncurated city", func(c *Capture) { c.City = "gotham" })
	refuse("slug not a day", func(c *Capture) { c.MapSlug = "latest" })
	refuse("no pyramid", func(c *Capture) { c.Basemap.MaxZoom = 0 })
	refuse("uncurated dataset", func(c *Capture) { c.Datasets[0].Slug = "parcels" })
	refuse("dataset doubled", func(c *Capture) { c.Datasets = append(c.Datasets, c.Datasets[0]) })
	refuse("every pin titleless", func(c *Capture) {
		for at := range c.Datasets {
			if c.Datasets[at].Slug == "historic-sites" {
				for f := range c.Datasets[at].Features {
					c.Datasets[at].Features[f].Fields["NAME"] = ""
				}
			}
		}
	})
	refuse("every pin astray", func(c *Capture) {
		for at := range c.Datasets {
			if c.Datasets[at].Slug == "historic-sites" {
				for f := range c.Datasets[at].Features {
					c.Datasets[at].Features[f].Geometry.Point = []float64{0, 0}
				}
			}
		}
	})
}

// One nameless row is the source's hygiene: the pin is left out and the
// rest of the dataset stands.
func TestTranslateSkipsLoneUntitledPin(t *testing.T) {
	capture := fixtureCapture()
	for at := range capture.Datasets {
		if capture.Datasets[at].Slug == "historic-sites" {
			capture.Datasets[at].Features[0].Fields["NAME"] = ""
		}
	}
	out := translated(t, capture)
	pins := out.Groups[0].Categories[0].Locations
	if len(pins) != 1 || pins[0].Title != "Drake Park Gazebo" {
		t.Fatalf("pins are %+v, want only the titled one", pins)
	}
}

func TestScrubDropsLiveURLTokens(t *testing.T) {
	cases := map[string]string{
		"plain prose stays":              "plain prose stays",
		"see https://example.com for it": "see for it",
		"HTTP://LOUD.example":            "",
		"":                               "",
	}
	for in, want := range cases {
		if got := scrub(in); got != want {
			t.Fatalf("scrub(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFieldString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"  A.C. Lucas House ", "A.C. Lucas House"},
		{float64(97702), "97702"},
		{float64(-2208988800000), "-2208988800000"},
		{0.0202956, "0.0202956"},
		{true, "true"},
	}
	for _, tc := range cases {
		if got := FieldString(tc.in); got != tc.want {
			t.Fatalf("FieldString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRound7(t *testing.T) {
	if got := Round7(-121.308105091063071234); got != -121.3081051 {
		t.Fatalf("Round7 = %v", got)
	}
}

// The curated table itself must be internally sound: slugs unique and
// well-formed, point datasets fully spelled, zone functions only on ground.
// Every city walks with the national enrichment appended, so a private
// city registered from cities_local.go is held to the same rules,
// collisions with the national slugs included.
func TestCuratedTableIsSound(t *testing.T) {
	for slug, city := range Cities {
		if slug != city.Slug {
			t.Fatalf("city %q keyed as %q", city.Slug, slug)
		}
		if city.MaxZoom < 1 || city.BBox[0] >= city.BBox[2] || city.BBox[1] >= city.BBox[3] {
			t.Fatalf("city %q declares no ground or no pyramid", slug)
		}
		seen := map[string]bool{}
		for _, dataset := range city.AllDatasets() {
			if dataset.Slug == "" || seen[dataset.Slug] {
				t.Fatalf("dataset slug %q is empty or doubled", dataset.Slug)
			}
			seen[dataset.Slug] = true
			if (dataset.ItemID == "") == (dataset.Server == "") || dataset.Title == "" {
				t.Fatalf("dataset %q wants exactly one identity, hub or national", dataset.Slug)
			}
			if dataset.Server != "" && len(dataset.Keep) == 0 {
				t.Fatalf("dataset %q pages a national layer without a row identity", dataset.Slug)
			}
			isPoint := dataset.Geometry == "point"
			if isPoint != (dataset.Group != "") {
				t.Fatalf("dataset %q: points and only points make pins", dataset.Slug)
			}
			if isPoint && (dataset.TitleOf == nil || dataset.Describe == nil) {
				t.Fatalf("dataset %q makes pins without spelling them", dataset.Slug)
			}
			if isPoint && dataset.ZoneOf != nil {
				t.Fatalf("dataset %q zones points", dataset.Slug)
			}
			if dataset.Geometry == "line" && dataset.ZoneOf != nil && dataset.StrokeWidth <= 0 {
				t.Fatalf("dataset %q zones lines without a ribbon", dataset.Slug)
			}
		}
	}
}
