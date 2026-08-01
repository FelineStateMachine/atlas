// The ArcGIS mode fills the archive from a city's open-data hub: the curated
// municipal datasets a city publishes about itself, fetched as GeoJSON
// through the hub's download API, and a basemap rendered from that same
// vector data -- no outside tile service, because the bundle ships offline
// and the city's own streets are the honest picture of the city.
//
// This is the curated-cities importer, and says so. Each city in the
// arcgismap table names verified dataset identities, field names, and a
// bounding box; an unknown city is refused at the door rather than captured
// through unverified curation.
//
// Versioning is the mode's whole reason. Each crawl day registers its own
// map directory, so the archive keeps every day the data moved and the
// bundle's map picker reads as the city's version history. A re-crawl on a
// day already captured is a no-op by content addressing; a new day whose
// data has not moved registers nothing.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FelineStateMachine/atlas/internal/arcgismap"
	"github.com/FelineStateMachine/atlas/internal/basemap"
)

func runArcgis(ctx context.Context, fetcher *fetcher, o options) error {
	city, known := arcgismap.Cities[o.arcgis]
	if !known {
		cities := make([]string, 0, len(arcgismap.Cities))
		for name := range arcgismap.Cities {
			cities = append(cities, name)
		}
		sort.Strings(cities)
		return fmt.Errorf("the ArcGIS importer knows only %s: each city names verified "+
			"datasets and a verified window, and an unverified one could hang every pin "+
			"on the wrong pixel", strings.Join(cities, ", "))
	}

	date := o.captureDate
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("-capture-date %q is not a YYYY-MM-DD day", o.captureDate)
	}

	maxZoom := city.MaxZoom
	if o.maxZoom > 0 {
		maxZoom = o.maxZoom
	}

	window := arcgismap.CityWindow(city.BBox)
	capture := &arcgismap.Capture{
		Source:  arcgismap.Source,
		City:    city.Slug,
		Title:   city.Title,
		MapSlug: date,
		Window:  window,
		Basemap: arcgismap.MapConfig{MaxZoom: maxZoom, Extension: "png"},
	}

	fmt.Printf("\n== ArcGIS Hub %s / %s (%d datasets)\n", city.Title, date, len(city.Datasets))
	pinCount := 0
	for _, dataset := range city.Datasets {
		features, err := fetchArcgisDataset(ctx, fetcher, city, dataset)
		if err != nil {
			return fmt.Errorf("%s: %w", dataset.Slug, err)
		}
		if dataset.Geometry == "point" {
			pinCount += len(features)
		}
		fmt.Printf("   %-22s %5d features\n", dataset.Slug, len(features))
		capture.Datasets = append(capture.Datasets, arcgismap.CapturedDataset{
			Slug:     dataset.Slug,
			Features: features,
		})
	}
	capture.Normalize()
	raw, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return err
	}
	// A capture that does not translate would poison the archive for both
	// tools that read it, so it is refused here, while the problem is one
	// crawl old rather than buried in history.
	if _, err := arcgismap.Translate(raw); err != nil {
		return fmt.Errorf("capture does not translate: %w", err)
	}

	gameID := arcgisArchiveID("arcgis:" + city.Slug)
	mapID := arcgisArchiveID("arcgis:" + city.Slug + "/" + date)
	game := &apiGameFull{ID: gameID, Title: city.Title,
		Config: apiGameConfig{URL: city.HubBase}}
	full := &apiMapFull{ID: mapID, Title: date, Slug: date}
	// The directory says where the capture came from even though the game
	// keeps its plain title, the same custom every non-MapGenie source keeps.
	gameDirectory := resolveGameDirectory(o.archive,
		&apiGameFull{ID: gameID, Title: "ArcGIS Hub " + city.Title})

	// A new day whose data has not moved registers nothing: the version
	// history means "the city changed", and an unchanged capture would spend
	// a map saying it had not.
	if previous, found, err := latestArcgisCapture(o.archive, gameDirectory, date); err != nil {
		return err
	} else if found && sameArcgisCapture(previous.capture, capture) {
		fmt.Printf("   unchanged since %s; no new version registered\n", previous.day)
		return nil
	}

	mapDirectory := resolveMapDirectory(o.archive, gameDirectory, full)
	mapDir := filepath.Join(o.archive, mapDirectory)

	if o.dryRun {
		fmt.Printf("   would write capture to %s and render z%d (%d tiles)\n",
			mapDir, maxZoom, 1<<(2*maxZoom))
		return nil
	}
	if err := writeSnapshotRecord(mapDir, raw, arcgismap.Kind, mapID,
		city.HubBase+"/api/feed/dcat-us/1.1.json"); err != nil {
		return err
	}

	index, err := readTileIndex(mapDir)
	if err != nil {
		return err
	}
	index.mapID = mapID
	stats, err := renderArcgisBasemap(ctx, o, city, capture, mapDir, index)
	if err != nil {
		return err
	}
	if err := writeTileIndex(mapDir, index); err != nil {
		return err
	}
	if err := writeMapMetadata(mapDir, game, full, index, pinCount); err != nil {
		return err
	}
	if err := registerMap(o.archive, game, full, gameDirectory, mapDirectory, arcgismap.Source); err != nil {
		return err
	}
	fmt.Printf("   %d rendered · %d held · %.1f MB new\n",
		stats.fetched, stats.skipped, float64(stats.bytes)/1e6)
	return nil
}

// fetchArcgisDataset takes one curated layer through the hub's download API,
// which stages a complete file server-side -- no record-count paging, no
// partial captures -- and answers 202 until the file is ready.
func fetchArcgisDataset(
	ctx context.Context,
	fetcher *fetcher,
	city arcgismap.City,
	dataset arcgismap.Dataset,
) ([]arcgismap.Feature, error) {
	// spatialRefId asks for true degrees by name: without it a hub answers
	// in the layer's own projection -- state-plane feet, for some cities --
	// and every coordinate would be an address in the wrong language.
	url := fmt.Sprintf("%s/api/download/v1/items/%s/geojson?layers=%d&spatialRefId=4326",
		city.HubBase, dataset.ItemID, dataset.Layer)
	var body []byte
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var err error
		body, _, err = fetcher.get(ctx, url)
		if err == nil {
			break
		}
		if !errors.Is(err, errAccepted) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("still staging after two minutes: %s", url)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	var collection struct {
		Type string `json:"type"`
		CRS  struct {
			Properties struct {
				Name string `json:"name"`
			} `json:"properties"`
		} `json:"crs"`
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &collection); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	if collection.Type != "FeatureCollection" {
		return nil, fmt.Errorf("%s is a %q, not a FeatureCollection", url, collection.Type)
	}
	// GeoJSON without a crs member is WGS84 by definition; one with a crs
	// must actually say so, or every coordinate is in another language.
	switch collection.CRS.Properties.Name {
	case "", "EPSG:4326", "urn:ogc:def:crs:OGC:1.3:CRS84":
	default:
		return nil, fmt.Errorf("%s answered in %s, not WGS84", url, collection.CRS.Properties.Name)
	}

	features := make([]arcgismap.Feature, 0, len(collection.Features))
	for at, raw := range collection.Features {
		id, err := arcgisObjectID(raw.Properties)
		if err != nil {
			return nil, fmt.Errorf("feature %d: %w", at, err)
		}
		fields := arcgismap.Fields{}
		for _, keep := range dataset.Keep {
			if value := arcgismap.FieldString(raw.Properties[keep]); value != "" {
				fields[keep] = value
			}
		}
		geometry, err := arcgisGeometry(dataset.Geometry, raw.Geometry.Type, raw.Geometry.Coordinates)
		if err != nil {
			return nil, fmt.Errorf("feature %d: %w", id, err)
		}
		if geometry == nil {
			continue
		}
		features = append(features, arcgismap.Feature{ID: id, Fields: fields, Geometry: *geometry})
	}
	return features, nil
}

// arcgisObjectID reads a feature's object identifier under either of the
// spellings the hub's layers use: OBJECTID nearly everywhere, OID on the
// occasional legacy layer.
func arcgisObjectID(properties map[string]any) (int64, error) {
	value, held := properties["OBJECTID"]
	if !held {
		value, held = properties["OID"]
	}
	if !held {
		return 0, errors.New("no OBJECTID")
	}
	number, ok := value.(float64)
	if !ok || number != float64(int64(number)) {
		return 0, fmt.Errorf("OBJECTID %v is not an integer", value)
	}
	return int64(number), nil
}

// arcgisGeometry normalizes a GeoJSON geometry into the capture's three
// shapes, held to the kind the curated table promised, coordinates rounded
// so float spelling cannot masquerade as change. A feature without geometry
// is dropped rather than refused: hubs hold the occasional geometryless row.
func arcgisGeometry(expected, kind string, coordinates json.RawMessage) (*arcgismap.Geometry, error) {
	if kind == "" || len(coordinates) == 0 {
		return nil, nil
	}
	switch expected {
	case "point":
		if kind != "Point" {
			return nil, fmt.Errorf("geometry is %s, not a Point", kind)
		}
		var position []float64
		if err := json.Unmarshal(coordinates, &position); err != nil {
			return nil, err
		}
		if len(position) < 2 {
			return nil, errors.New("point has no position")
		}
		return &arcgismap.Geometry{Type: arcgismap.GeometryPoint,
			Point: []float64{arcgismap.Round7(position[0]), arcgismap.Round7(position[1])}}, nil
	case "line":
		var lines [][][]float64
		switch kind {
		case "LineString":
			var one [][]float64
			if err := json.Unmarshal(coordinates, &one); err != nil {
				return nil, err
			}
			lines = [][][]float64{one}
		case "MultiLineString":
			if err := json.Unmarshal(coordinates, &lines); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("geometry is %s, not a line", kind)
		}
		return &arcgismap.Geometry{Type: arcgismap.GeometryLines, Lines: roundLines(lines)}, nil
	case "polygon":
		var polygons [][][][]float64
		switch kind {
		case "Polygon":
			var one [][][]float64
			if err := json.Unmarshal(coordinates, &one); err != nil {
				return nil, err
			}
			polygons = [][][][]float64{one}
		case "MultiPolygon":
			if err := json.Unmarshal(coordinates, &polygons); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("geometry is %s, not a polygon", kind)
		}
		for _, polygon := range polygons {
			for at, ring := range polygon {
				polygon[at] = roundPositions(ring)
			}
		}
		return &arcgismap.Geometry{Type: arcgismap.GeometryRings, Rings: polygons}, nil
	}
	return nil, fmt.Errorf("curated geometry %q is unknown", expected)
}

func roundLines(lines [][][]float64) [][][]float64 {
	for at, line := range lines {
		lines[at] = roundPositions(line)
	}
	return lines
}

func roundPositions(positions [][]float64) [][]float64 {
	for _, position := range positions {
		for at, value := range position {
			position[at] = arcgismap.Round7(value)
		}
	}
	return positions
}

// arcgisSnapshot is the slice of game.json this mode reads back: which dated
// maps the game already carries, and where they live.
type previousCapture struct {
	day     string
	capture *arcgismap.Capture
}

// latestArcgisCapture loads the newest capture from a day before this one,
// so an unchanged city can decline to spend a version.
func latestArcgisCapture(archiveRoot, gameDirectory, before string) (previousCapture, bool, error) {
	var gameFile struct {
		Maps []struct {
			Directory string `json:"directory"`
			Slug      string `json:"slug"`
		} `json:"maps"`
	}
	err := readJSON(filepath.Join(archiveRoot, gameDirectory, "game.json"), &gameFile)
	if errors.Is(err, os.ErrNotExist) {
		return previousCapture{}, false, nil
	}
	if err != nil {
		return previousCapture{}, false, err
	}
	newest := ""
	directory := ""
	for _, entry := range gameFile.Maps {
		if len(entry.Slug) == len("2006-01-02") && entry.Slug < before && entry.Slug > newest {
			newest, directory = entry.Slug, entry.Directory
		}
	}
	if newest == "" {
		return previousCapture{}, false, nil
	}

	mapDir := filepath.Join(archiveRoot, directory)
	var snapshots []snapshotRecord
	if err := readJSON(filepath.Join(mapDir, "snapshots", "index.json"), &snapshots); err != nil {
		return previousCapture{}, false, err
	}
	if len(snapshots) == 0 {
		return previousCapture{}, false, nil
	}
	sort.Slice(snapshots, func(a, b int) bool {
		return snapshots[a].CapturedAt < snapshots[b].CapturedAt
	})
	latest := snapshots[len(snapshots)-1]
	raw, err := os.ReadFile(filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json"))
	if err != nil {
		return previousCapture{}, false, err
	}
	var capture arcgismap.Capture
	if err := json.Unmarshal(raw, &capture); err != nil {
		return previousCapture{}, false, err
	}
	return previousCapture{day: newest, capture: &capture}, true, nil
}

// sameArcgisCapture asks whether two captures say the same thing about the
// city, the day itself aside.
func sameArcgisCapture(previous, current *arcgismap.Capture) bool {
	a, b := *previous, *current
	a.MapSlug, b.MapSlug = "", ""
	rawA, errA := json.Marshal(&a)
	rawB, errB := json.Marshal(&b)
	return errA == nil && errB == nil && bytes.Equal(rawA, rawB)
}

// renderArcgisBasemap draws the deepest level of the city's own ground into
// the archive, every tile of it, exactly as a fetching source would have
// recorded downloaded ones: tools/tiles folds the rest of the pyramid down
// from this level and never knows the wire was absent. Tiles whose bytes
// already sit in the index are held, not rewritten, so a re-render without
// change leaves the archive's working tree untouched.
func renderArcgisBasemap(
	ctx context.Context,
	o options,
	city arcgismap.City,
	capture *arcgismap.Capture,
	mapDir string,
	index *tileIndex,
) (captureStats, error) {
	var stats captureStats
	features := basemapFeatures(city, capture)
	renderer := basemap.NewRenderer(features, capture.Basemap.MaxZoom)
	scope := arcgismap.TileSetPath(city.Slug, capture.MapSlug)
	setID := index.tileSetID(scope)
	template := arcgismap.TileTemplate(city.Slug, capture.MapSlug)
	zoom := capture.Basemap.MaxZoom
	edge := 1 << zoom
	fmt.Printf("   basemap %q z%d, %d tiles from %d features\n",
		scope, zoom, edge*edge, len(features))

	var mu sync.Mutex
	var group sync.WaitGroup
	var failure error
	gate := make(chan struct{}, o.concurrency)
	for y := range edge {
		for x := range edge {
			select {
			case <-ctx.Done():
				group.Wait()
				return stats, ctx.Err()
			default:
			}
			group.Add(1)
			gate <- struct{}{}
			go func(x, y int) {
				defer group.Done()
				defer func() { <-gate }()
				url := strings.NewReplacer(
					"{z}", strconv.Itoa(zoom), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y),
				).Replace(template)
				path := filepath.Join(mapDir, "tiles",
					"set-"+strconv.FormatInt(setID, 10),
					strconv.Itoa(zoom), strconv.Itoa(x), strconv.Itoa(y)+".png")

				body, err := basemap.EncodePNG(renderer.Tile(zoom, x, y))
				if err != nil {
					mu.Lock()
					if failure == nil {
						failure = fmt.Errorf("render %d/%d/%d: %w", zoom, x, y, err)
					}
					mu.Unlock()
					return
				}
				sum := sha256.Sum256(body)
				hash := hex.EncodeToString(sum[:])

				mu.Lock()
				record, held := index.byURL[url]
				mu.Unlock()
				if held && record.Status == "cached" && record.ContentHash == hash {
					if _, err := os.Stat(path); err == nil {
						mu.Lock()
						stats.skipped++
						mu.Unlock()
						return
					}
				}
				if err := writeFile(path, body); err != nil {
					mu.Lock()
					if failure == nil {
						failure = err
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				index.put(tileRecord{
					URL: url, Status: "cached", TileSetID: setID,
					Zoom: zoom, X: x, Y: y,
					ContentHash: hash, ContentType: "image/png",
					ByteLength: len(body),
					CoverageKey: fmt.Sprintf("%d:%d:%d:%d:%d",
						index.mapID, setID, zoom, x, y),
				})
				stats.fetched++
				stats.bytes += int64(len(body))
				mu.Unlock()
			}(x, y)
		}
	}
	group.Wait()
	return stats, failure
}

// basemapFeatures projects every drawing dataset into world pixels under the
// capture's own window, so the raster and the pins can never disagree about
// where the ground is.
func basemapFeatures(city arcgismap.City, capture *arcgismap.Capture) []basemap.Feature {
	curated := make(map[string]*arcgismap.Dataset, len(city.Datasets))
	for at := range city.Datasets {
		curated[city.Datasets[at].Slug] = &city.Datasets[at]
	}
	var features []basemap.Feature
	for _, dataset := range capture.Datasets {
		table := curated[dataset.Slug]
		if table == nil || table.Role == "" {
			continue
		}
		role := basemap.Role(table.Role)
		for _, feature := range dataset.Features {
			emphasis := 0.0
			if table.Emphasis != nil {
				emphasis = table.Emphasis(feature.Fields)
			}
			switch feature.Geometry.Type {
			case arcgismap.GeometryRings:
				// Each polygon draws on its own, so winding normalization
				// reads every first ring as ground and the rest as holes.
				for _, polygon := range feature.Geometry.Rings {
					drawn := basemap.Feature{Role: role, Emphasis: emphasis}
					for _, ring := range polygon {
						drawn.Rings = append(drawn.Rings, projectPositions(capture.Window, ring))
					}
					if len(drawn.Rings) > 0 {
						features = append(features, drawn)
					}
				}
			case arcgismap.GeometryLines:
				drawn := basemap.Feature{Role: role, Emphasis: emphasis}
				for _, line := range feature.Geometry.Lines {
					drawn.Lines = append(drawn.Lines, projectPositions(capture.Window, line))
				}
				if len(drawn.Lines) > 0 {
					features = append(features, drawn)
				}
			}
		}
	}
	return features
}

func projectPositions(window arcgismap.Window, positions [][]float64) [][2]float64 {
	out := make([][2]float64, 0, len(positions))
	for _, position := range positions {
		if len(position) < 2 {
			continue
		}
		x, y := window.WorldPixel(position[0], position[1])
		out = append(out, [2]float64{x, y})
	}
	return out
}

// arcgisArchiveID numbers ArcGIS cities and maps in the archive's registers,
// one bit above Trek's so every source's ids stay apart; see ignArchiveID
// for why the range stays far below a float64's integers.
func arcgisArchiveID(seed string) int64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int64(hash.Sum32()&0x7fffffff) | 1<<35
}
