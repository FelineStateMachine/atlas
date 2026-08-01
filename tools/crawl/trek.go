// The Trek mode fills the archive from two public planetary publications at
// once: NASA Trek's global tile mosaics for the raster, and the IAU Gazetteer
// of Planetary Nomenclature's downloadable feature list for the pins. Both
// are plainly published for taking -- the mosaic behind an open WMTS
// endpoint, the features as a shapefile whose attribute table carries name,
// place, size, and the origin story of every name -- and nothing else is
// touched: no page prose, nothing the data files do not carry.
//
// This is the curated-bodies importer, and says so. A Trek mosaic's tiling is
// asserted from its own WMTS capabilities before anything is fetched, but
// which mosaic represents a body is an editorial choice; each body in the
// table names one verified global equirectangular layer, and an unknown body
// is refused at the door rather than captured through an unverified one.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/trekmap"
)

const trekTiles = "https://trek.nasa.gov/tiles"

// trekDefaultZoom is how deep a crawl goes unasked, in the pipeline's square
// world where level z holds Trek's z-1. Level 6 is Trek's 5: 2048 tiles for
// the deepest level and a third of that above it, a courteous take of a
// pyramid whose deepest published levels run to hundreds of thousands.
const trekDefaultZoom = 6

// trekBody is one body the importer knows: how Trek's paths spell it, which
// global mosaic pictures it, the sibling mosaics captured beside that one,
// and where the Gazetteer's feature list for it lives.
type trekBody struct {
	pathName   string
	layer      string
	layerTitle string
	variants   []trekLayer
	portal     string
	gazetteer  string
}

// trekLayer names one verified sibling mosaic and how a person knows it.
type trekLayer struct {
	layer string
	title string
}

var trekBodies = map[string]trekBody{
	"mars": {
		pathName:   "Mars",
		layer:      "Mars_Viking_MDIM21_ClrMosaic_global_232m",
		layerTitle: "Viking MDIM 2.1",
		variants: []trekLayer{
			// The laser altimeter's colored shaded relief: the same ground as
			// height instead of photograph, the other classic way Mars is seen.
			{layer: "Mars_MGS_MOLA_ClrShade_merge_global_463m", title: "MOLA Elevation"},
		},
		portal:    "https://trek.nasa.gov/mars",
		gazetteer: "https://asc-planetarynames-data.s3.us-west-2.amazonaws.com/MARS_nomenclature_center_pts.zip",
	},
}

func runTrek(ctx context.Context, fetcher *fetcher, o options) error {
	slug := o.trek
	body, known := trekBodies[slug]
	if !known {
		bodies := make([]string, 0, len(trekBodies))
		for name := range trekBodies {
			bodies = append(bodies, name)
		}
		return fmt.Errorf("the Trek importer knows only %s: each body names one verified "+
			"global mosaic, and an unverified one could hang every pin on the wrong pixel",
			strings.Join(bodies, ", "))
	}

	// Every mosaic of the body answers for its own tiling before anything is
	// fetched, and takes its own depth: the default courtesy ceiling, under
	// whatever each one actually publishes.
	layers := append([]trekLayer{{layer: body.layer, title: body.layerTitle}}, body.variants...)
	depths := make([]int, len(layers))
	for at, entry := range layers {
		published, err := fetchTrekPyramid(ctx, fetcher, trekLayerBase(body, entry.layer))
		if err != nil {
			return fmt.Errorf("%s: %w", entry.layer, err)
		}
		depths[at] = published
		if o.maxZoom > 0 {
			depths[at] = min(depths[at], o.maxZoom)
		} else {
			depths[at] = min(depths[at], trekDefaultZoom)
		}
	}

	features, err := fetchGazetteer(ctx, fetcher, body.gazetteer)
	if err != nil {
		return err
	}

	capture := &trekmap.Capture{
		Source:   trekmap.Source,
		Body:     slug,
		Layer:    body.layer,
		MapSlug:  "global",
		MapTitle: "Global",
		Map: trekmap.MapConfig{
			MaxZoom:    depths[0],
			Extension:  "jpg",
			LayerTitle: body.layerTitle,
		},
		Features: features,
	}
	for at, entry := range layers[1:] {
		capture.Variants = append(capture.Variants, trekmap.Variant{
			Layer:     entry.layer,
			Title:     entry.title,
			MaxZoom:   depths[at+1],
			Extension: "jpg",
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
	if _, err := trekmap.Translate(raw); err != nil {
		return fmt.Errorf("capture does not translate: %w", err)
	}

	gameID := trekArchiveID("trek:" + slug)
	mapID := trekArchiveID("trek:" + slug + "/" + capture.MapSlug)
	game := &apiGameFull{ID: gameID, Title: mgdoc.SpellOut(slug),
		Config: apiGameConfig{URL: body.portal}}
	full := &apiMapFull{ID: mapID, Title: capture.MapTitle, Slug: capture.MapSlug}
	// The directory says where the capture came from even though the game
	// keeps its plain title: another source picturing the same body would
	// live in its own directory, and the two must never resolve to one.
	gameDirectory := resolveGameDirectory(o.archive,
		&apiGameFull{ID: gameID, Title: "NASA Trek " + game.Title})
	mapDirectory := resolveMapDirectory(o.archive, gameDirectory, full)
	mapDir := filepath.Join(o.archive, mapDirectory)

	fmt.Printf("\n== NASA Trek %s / %s (%d features, %d layers)\n",
		game.Title, capture.MapTitle, len(capture.Features), len(layers))

	if o.dryRun {
		fmt.Printf("   would write capture to %s\n", mapDir)
	} else if err := writeSnapshotRecord(mapDir, raw, trekmap.Kind, mapID, body.gazetteer); err != nil {
		return err
	}

	index, err := readTileIndex(mapDir)
	if err != nil {
		return err
	}
	index.mapID = mapID
	var stats captureStats
	for at, entry := range layers {
		taken, err := captureTrekTiles(ctx, fetcher, o, slug, body, entry.layer, depths[at], mapDir, index)
		if err != nil {
			return err
		}
		stats.fetched += taken.fetched
		stats.skipped += taken.skipped
		stats.absent += taken.absent
		stats.failed += taken.failed
		stats.bytes += taken.bytes
	}
	if o.dryRun {
		return nil
	}
	if err := writeTileIndex(mapDir, index); err != nil {
		return err
	}
	if err := writeMapMetadata(mapDir, game, full, index, len(capture.Features)); err != nil {
		return err
	}
	if err := registerMap(o.archive, game, full, gameDirectory, mapDirectory, "nasa-trek"); err != nil {
		return err
	}
	fmt.Printf("   %d fetched · %d held · %d not published · %d failed · %.1f MB new\n",
		stats.fetched, stats.skipped, stats.absent, stats.failed, float64(stats.bytes)/1e6)
	return nil
}

// wmtsCapabilities is the slice of a layer's WMTS document the crawl stands
// on: the tile matrices, whose sizes and origin say how the mosaic is tiled.
type wmtsCapabilities struct {
	Contents struct {
		TileMatrixSets []struct {
			Matrices []struct {
				TopLeftCorner string  `xml:"TopLeftCorner"`
				TileWidth     int     `xml:"TileWidth"`
				TileHeight    int     `xml:"TileHeight"`
				MatrixWidth   float64 `xml:"MatrixWidth"`
				MatrixHeight  float64 `xml:"MatrixHeight"`
			} `xml:"TileMatrix"`
		} `xml:"TileMatrixSet"`
	} `xml:"Contents"`
}

// fetchTrekPyramid reads the layer's own account of its tiling and holds it
// to the shape the translator promises: an equirectangular pyramid anchored
// at longitude -180, latitude 90, two tiles wide for every one tall, in
// 256-pixel tiles. It returns the deepest pipeline zoom published -- one
// above the deepest Trek matrix, since the pipeline's square world sits one
// level over the 2:1 mosaic.
func fetchTrekPyramid(ctx context.Context, f *fetcher, layerBase string) (int, error) {
	capsURL := layerBase + "/1.0.0/WMTSCapabilities.xml"
	body, _, err := f.get(ctx, capsURL)
	if err != nil {
		return 0, fmt.Errorf("capabilities: %w", err)
	}
	var caps wmtsCapabilities
	if err := xml.Unmarshal(body, &caps); err != nil {
		return 0, fmt.Errorf("decode %s: %w", capsURL, err)
	}
	if len(caps.Contents.TileMatrixSets) != 1 {
		return 0, fmt.Errorf("%s declares %d tile matrix sets, not the one global scheme",
			capsURL, len(caps.Contents.TileMatrixSets))
	}
	matrices := caps.Contents.TileMatrixSets[0].Matrices
	if len(matrices) == 0 {
		return 0, fmt.Errorf("%s declares no tile matrices", capsURL)
	}
	for level, matrix := range matrices {
		wide, tall := float64(int(2)<<level), float64(int(1)<<level)
		corner := strings.Fields(matrix.TopLeftCorner)
		if len(corner) != 2 || corner[0] != "-180.0" || corner[1] != "90.0" {
			return 0, fmt.Errorf("level %d anchors at %q, not the -180/90 corner the "+
				"projection assumes", level, matrix.TopLeftCorner)
		}
		if matrix.TileWidth != 256 || matrix.TileHeight != 256 ||
			matrix.MatrixWidth != wide || matrix.MatrixHeight != tall {
			return 0, fmt.Errorf("level %d is %vx%v tiles of %dx%d px; the projection "+
				"assumes the 2:1 equirectangular scheme", level,
				matrix.MatrixWidth, matrix.MatrixHeight, matrix.TileWidth, matrix.TileHeight)
		}
	}
	return len(matrices), nil
}

// trekLayerBase is where one of the body's mosaics actually lives, in Trek's
// own spelling of the body's name.
func trekLayerBase(body trekBody, layer string) string {
	return trekTiles + "/" + body.pathName + "/EQ/" + layer
}

// captureTrekTiles takes one layer's pyramid level by level, whole: a global
// mosaic has no empty space to prune around. Trek spells a tile as matrix/
// row/column, one zoom below and with its axes named the other way round
// from the pipeline's zoom/x/y -- so each level fetches under Trek's
// spelling and records under the canonical one, which is the address every
// reader of the archive derives the layer and its window from.
func captureTrekTiles(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	slug string,
	body trekBody,
	layer string,
	deepest int,
	mapDir string,
	index *tileIndex,
) (captureStats, error) {
	var stats captureStats
	scope := trekmap.TileSetPath(slug, layer)
	setID := index.tileSetID(scope)
	fmt.Printf("   layer %q z1-%d\n", scope, deepest)

	for zoom := 1; zoom <= deepest; zoom++ {
		maxX, maxY := trekmap.LevelExtent(zoom)
		window := tileWindow{minX: 0, minY: 0, maxX: maxX, maxY: maxY}
		record := trekTiles + "/" + scope + "/{z}/{x}/{y}.jpg"
		fetch := trekLayerBase(body, layer) + "/1.0.0/default/default028mm/" +
			strconv.Itoa(zoom-1) + "/{y}/{x}.jpg"
		results, err := fetchTemplateLevel(ctx, fetcher, o, record, fetch, "", setID, "jpg",
			mapDir, zoom, window.tiles(), index)
		if err != nil {
			return stats, err
		}
		stats.merge(results)
		fmt.Printf("     z%-2d %4d fetched · %4d held · %4d not published\n",
			zoom, results.fetched, results.skipped, results.absent)
		// The bundle is built from the deepest complete level. Holes there mean
		// the mosaic is thinner than its capabilities claim, which deserves
		// more than a counter in the summary line.
		if zoom == deepest && results.absent > 0 {
			fmt.Printf("     WARNING: %d tiles of the deepest level are not published; "+
				"the bundle will be built from a shallower level\n", results.absent)
		}
	}
	return stats, nil
}

// fetchGazetteer takes the body's feature list as the Gazetteer publishes it
// -- a zipped shapefile -- and reads the attribute table inside, which
// carries everything the capture keeps: names, places, sizes, feature types,
// and the origin text explaining each name.
func fetchGazetteer(ctx context.Context, f *fetcher, url string) ([]trekmap.Feature, error) {
	body, _, err := f.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("gazetteer: %w", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("read gazetteer archive: %w", err)
	}
	for _, file := range archive.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), ".dbf") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		table, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return nil, err
		}
		return parseNomenclature(table)
	}
	return nil, fmt.Errorf("%s holds no attribute table", url)
}

// parseNomenclature reads the shapefile's dBASE attribute table. The format
// is fixed-width rows behind a field directory; the Gazetteer writes it in
// UTF-8, per the encoding sidecar in the same archive.
func parseNomenclature(table []byte) ([]trekmap.Feature, error) {
	if len(table) < 32 {
		return nil, errors.New("attribute table is truncated")
	}
	records := int(binary.LittleEndian.Uint32(table[4:8]))
	headerSize := int(binary.LittleEndian.Uint16(table[8:10]))
	recordSize := int(binary.LittleEndian.Uint16(table[10:12]))

	type column struct {
		name   string
		start  int
		length int
	}
	var columns []column
	// Each row leads with a deletion flag byte, so fields start at one.
	at := 1
	for pos := 32; ; pos += 32 {
		if pos >= len(table) || pos >= headerSize {
			return nil, errors.New("attribute table's field directory never ends")
		}
		if table[pos] == 0x0d {
			break
		}
		if pos+32 > len(table) {
			return nil, errors.New("attribute table is truncated inside a field")
		}
		name := string(bytes.TrimRight(table[pos:pos+11], "\x00"))
		length := int(table[pos+16])
		columns = append(columns, column{name: name, start: at, length: length})
		at += length
	}
	if at != recordSize {
		return nil, fmt.Errorf("fields span %d bytes of a %d-byte record", at, recordSize)
	}
	if headerSize+records*recordSize > len(table) {
		return nil, fmt.Errorf("attribute table holds fewer than its %d records", records)
	}

	wanted := map[string]bool{
		"name": true, "type": true, "code": true, "origin": true,
		"diameter": true, "center_lat": true, "center_lon": true, "link": true,
	}
	features := make([]trekmap.Feature, 0, records)
	for number := range records {
		row := table[headerSize+number*recordSize:][:recordSize]
		if row[0] == '*' {
			continue
		}
		cells := make(map[string]string, len(wanted))
		for _, col := range columns {
			if !wanted[col.name] {
				continue
			}
			cells[col.name] = strings.TrimSpace(string(row[col.start : col.start+col.length]))
		}
		feature, err := composeFeature(cells)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", number, err)
		}
		features = append(features, feature)
	}
	return dedupeFeatures(features)
}

// dedupeFeatures folds the table's own repeats. The Mars list has been seen
// carrying one crater twice, byte for byte; a doubled row saying the same
// thing is the source's hygiene, not a different feature, but two rows
// claiming one identifier for different facts would be, and are refused.
func dedupeFeatures(features []trekmap.Feature) ([]trekmap.Feature, error) {
	seen := make(map[int64]trekmap.Feature, len(features))
	kept := features[:0]
	for _, feature := range features {
		previous, held := seen[feature.ID]
		if !held {
			seen[feature.ID] = feature
			kept = append(kept, feature)
			continue
		}
		if previous != feature {
			return nil, fmt.Errorf("feature %d is both %q and %q", feature.ID,
				previous.Name, feature.Name)
		}
	}
	return kept, nil
}

// composeFeature turns one attribute row into the capture's terms. The
// Gazetteer's own feature identifier comes off the record's link, which is
// the only place the table spells it.
func composeFeature(cells map[string]string) (trekmap.Feature, error) {
	_, tail, found := strings.Cut(cells["link"], "/Feature/")
	if !found {
		return trekmap.Feature{}, fmt.Errorf("no feature identifier in link %q", cells["link"])
	}
	id, err := strconv.ParseInt(tail, 10, 64)
	if err != nil {
		return trekmap.Feature{}, fmt.Errorf("feature identifier %q: %w", tail, err)
	}
	latitude, err := strconv.ParseFloat(cells["center_lat"], 64)
	if err != nil {
		return trekmap.Feature{}, fmt.Errorf("latitude %q: %w", cells["center_lat"], err)
	}
	longitude, err := strconv.ParseFloat(cells["center_lon"], 64)
	if err != nil {
		return trekmap.Feature{}, fmt.Errorf("longitude %q: %w", cells["center_lon"], err)
	}
	diameter := 0.0
	if cells["diameter"] != "" {
		if diameter, err = strconv.ParseFloat(cells["diameter"], 64); err != nil {
			return trekmap.Feature{}, fmt.Errorf("diameter %q: %w", cells["diameter"], err)
		}
	}
	return trekmap.Feature{
		ID:         id,
		Name:       cells["name"],
		Type:       cells["type"],
		Code:       cells["code"],
		Latitude:   latitude,
		Longitude:  longitude,
		DiameterKM: diameter,
		Origin:     cells["origin"],
	}, nil
}

// trekArchiveID numbers Trek bodies and maps in the archive's registers, one
// bit above Piggyback's so every source's ids stay apart; see ignArchiveID
// for why the range stays far below a float64's integers.
func trekArchiveID(seed string) int64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int64(hash.Sum32()&0x7fffffff) | 1<<34
}
