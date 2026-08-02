// Package maturity scores a build of a volume.
//
// # What a score is
//
// It is unbounded, additive, and monotone. Every feature earns points for each
// quality it verifiably has -- a name, prose, prose with something in it,
// resolved coordinates, a membership its ground was joined to, a collection
// that resolves artwork, geometry actually drawn, another reading that
// corroborates it -- and those sums roll up through collections and worlds to
// the volume. Collections earn for the conventions they declare; worlds earn
// for what they say about their surface and for how much raster a reader can
// actually get to.
//
// There are no denominators and no ceilings, and that is the whole of why this
// scores points rather than percentages: a share moves when its denominator
// moves, so a build that added five hundred features and described half of them
// reads as a regression. The five absolute axes survive as diagnostics -- good
// for reading, useless as a gate -- and a described *share* above 100% is not
// a defect that can be reached from here, because nothing is divided by
// anything.
//
// # The gate
//
// An enrichment build whose score declines fails (see [Gate]). The comparison
// is only ever between builds scored under the same table version: a
// re-weighting is a new version of the table, not a mass failure of every build
// in the library. A decrease that the ledger accounts for -- data removed
// because it was wrong -- is permitted, because the gate exists to reward good
// data and never to punish a correction.
package maturity

import (
	"archive/zip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
)

//go:embed points.json
var embeddedTable []byte

// Table is the versioned point table: what each quality is worth.
type Table struct {
	Schema  int `json:"schema"`
	Version int `json:"version"`
	Feature struct {
		Name           int `json:"name"`
		Prose          int `json:"prose"`
		ProseSubstance int `json:"proseSubstance"`
		SubstanceChars int `json:"substanceChars"`
		Geo            int `json:"geo"`
		Membership     int `json:"membership"`
		Icon           int `json:"icon"`
		GeometryLog    int `json:"geometryLog"`
		Corroboration  int `json:"corroboration"`
	} `json:"feature"`
	Collection struct {
		Convention int `json:"convention"`
	} `json:"collection"`
	World struct {
		Convention int `json:"convention"`
		Lens       int `json:"lens"`
		LensZoom   int `json:"lensZoom"`
	} `json:"world"`
}

// TableSchema is the file layout this package reads.
const TableSchema = 1

// Points reads the embedded point table.
func Points() (Table, error) { return ReadTable(embeddedTable) }

// ReadTable reads a point table from bytes, for a test that wants to state its
// own weights.
func ReadTable(data []byte) (Table, error) {
	var table Table
	if err := json.Unmarshal(data, &table); err != nil {
		return Table{}, fmt.Errorf("decode point table: %w", err)
	}
	if table.Schema != TableSchema {
		return Table{}, fmt.Errorf("point table schema %d, want %d", table.Schema, TableSchema)
	}
	if table.Version <= 0 {
		return Table{}, fmt.Errorf("point table declares no version")
	}
	if table.Feature.SubstanceChars <= 0 {
		return Table{}, fmt.Errorf("point table declares no substance threshold")
	}
	return table, nil
}

// MembershipKeys are the attributes that say a feature's ground was joined to
// somewhere. One point each; the list grows with the joins.
var MembershipKeys = []string{semconv.KeyHydroHUC12}

// Score is one build, scored.
type Score struct {
	// TableVersion is which point table produced these numbers. Two scores are
	// comparable only when it agrees.
	TableVersion int

	File      string
	Path      string
	Volume    string
	Title     string
	Stamp     string
	CreatedAt string
	Revision  int
	// Enriched reports that this build's revision was written by the enrich
	// lane, and under which enrich policy.
	Enriched     bool
	EnrichPolicy int

	Total  int
	Worlds []WorldScore

	// Axes are the absolute measurements, kept as diagnostics for a reader
	// comparing two builds. Nothing gates on them.
	Axes Axes

	// Ledger is every provenance account the build's payloads carry.
	Ledger []LedgerLine
}

// WorldScore is one world's contribution to the total.
type WorldScore struct {
	Slug        string
	Total       int
	Features    int
	Collections int
	World       int
}

// LedgerLine is one account, with the world it belongs to.
type LedgerLine struct {
	World   string
	Account enrich.Account
}

// Axes are the five absolute measurement axes, kept as a diagnostic.
type Axes struct {
	// Annotation.
	Points int
	// Features is every feature of every kind, and it is the denominator a
	// described share is actually a share of: prose defers into one text
	// payload whatever kind of feature wrote it, so dividing that whole by the
	// point features alone is how a city whose zones are all described comes
	// to report that 235% of it is described. Scoring in points retired the
	// consequence; naming the right denominator here retires the arithmetic.
	Features     int
	Described    int
	MedianLength int
	// Cartography.
	TileCount   int
	RasterBytes int64
	Depth       int
	DepthTiles  int
	// Structure.
	Collections int
	Groups      int
	TextSets    int
	Shapes      int
	Vertices    int
	Lenses      int
	// Icons.
	IconsCarried int
	IconsWanted  int
	// Conventions.
	Conventions    int
	RenderDeclared int
	StdIcons       int
	GeoFeatures    int
	Memberships    int
	Geometry       string
	UnknownAttrs   int
}

// DescribedShare, IconShare and RenderShare are diagnostics, printed and never
// gated on. They are what percentages are for: reading, not deciding.
func (a Axes) DescribedShare() string { return share(a.Described, a.Features) }
func (a Axes) IconShare() string      { return share(a.IconsCarried, a.IconsWanted) }
func (a Axes) RenderShare() string    { return share(a.RenderDeclared, a.Collections) }

// RasterMB is the unique raster weight in megabytes, one decimal.
func (a Axes) RasterMB() string { return fmt.Sprintf("%.1f", float64(a.RasterBytes)/1e6) }

func share(part, whole int) string {
	if whole == 0 {
		return "—"
	}
	return strconv.Itoa(part*100/whole) + "%"
}

// the sliver of a world payload a score reads.
type worldPayload struct {
	Attrs       map[string]string `json:"attrs"`
	Collections []struct {
		ID        int64             `json:"id"`
		Title     string            `json:"title"`
		Group     string            `json:"group"`
		Kind      string            `json:"kind"`
		IconAsset string            `json:"iconAsset"`
		Attrs     map[string]string `json:"attrs"`
		Features  []struct {
			ID       int64             `json:"id"`
			Title    string            `json:"title"`
			HasText  bool              `json:"hasText"`
			Attrs    map[string]string `json:"attrs"`
			Geometry []struct {
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	} `json:"collections"`
	Lenses []struct {
		Name    string `json:"name"`
		MinZoom int    `json:"minZoom"`
		MaxZoom int    `json:"maxZoom"`
	} `json:"lenses"`
	Merged []enrich.Account `json:"merged"`
}

type featureText struct {
	Description string            `json:"d"`
	Attrs       map[string]string `json:"a"`
}

// WorldParts is one world's three payloads, as a score reads them. It exists
// so that scoring is a pure function of bytes: [Measure] gets them out of an
// archive, and a test gets them out of a fixture directory, and both are
// scoring the same thing.
type WorldParts struct {
	Slug string
	// Payload and Text are worlds/<slug>.json and worlds/<slug>.text, verbatim.
	Payload []byte
	Text    []byte
	// Locations are the world's packed point features, unpacked.
	Locations []bundle.Location
}

// Measure scores one .atlas file.
//
// It reads the archive directly rather than only through the reader, because
// the cartography diagnostic needs what only the table of contents knows: every
// tile entry's checksum and size, for de-duplicating filler.
func Measure(path string, table Table) (*Score, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	entries := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		entries[file.Name] = file
	}
	var manifest bundle.Manifest
	if err := readEntry(entries, bundle.ManifestName, &manifest); err != nil {
		return nil, err
	}

	parts := make([]WorldParts, 0, len(manifest.Worlds))
	for _, entry := range manifest.Worlds {
		payload, err := readBytes(entries, bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix))
		if err != nil {
			return nil, err
		}
		text, err := readBytes(entries, bundle.WorldEntryName(entry.Slug, bundle.TextSuffix))
		if err != nil {
			return nil, err
		}
		packed, err := readBytes(entries, bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix))
		if err != nil {
			return nil, err
		}
		locations, err := bundle.UnpackLocations(packed)
		if err != nil {
			return nil, fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		parts = append(parts, WorldParts{Slug: entry.Slug, Payload: payload, Text: text, Locations: locations})
	}

	score, err := ScoreParts(manifest, parts, table)
	if err != nil {
		return nil, err
	}
	score.Path = path
	score.File = filepath.Base(path)

	seen := make(map[[2]uint64]bool)
	for name, file := range entries {
		if !strings.HasPrefix(name, "tiles/") {
			continue
		}
		score.Axes.TileCount++
		key := [2]uint64{uint64(file.CRC32), file.UncompressedSize64}
		if !seen[key] {
			seen[key] = true
			score.Axes.RasterBytes += int64(file.UncompressedSize64)
		}
		if zoom := tileZoom(name); zoom == score.Axes.Depth {
			score.Axes.DepthTiles++
		}
	}
	return score, nil
}

// ScoreParts scores a build from its manifest and its worlds' payloads. It
// touches no filesystem and reads no rasters: the cartography axis is the only
// thing an archive knows that these bytes do not, and it is a diagnostic.
func ScoreParts(manifest bundle.Manifest, parts []WorldParts, table Table) (*Score, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	policy, enriched := enrich.Enriched(manifest.Version.Revision)
	score := &Score{
		TableVersion: table.Version,
		Volume:       manifest.Volume.Slug,
		Title:        manifest.Volume.Title,
		Stamp:        manifest.Version.Stamp,
		CreatedAt:    manifest.Version.CreatedAt,
		Revision:     manifest.Version.Revision,
		Enriched:     enriched,
		EnrichPolicy: policy,
	}
	score.Axes.Conventions = manifest.Conventions
	score.Axes.Geometry = semconv.SurfacePlane + "-default"

	byWorld := make(map[string]WorldParts, len(parts))
	for _, part := range parts {
		byWorld[part.Slug] = part
	}
	var lengths []int
	// One group set for the whole volume, built once outside the loop: a group
	// title shared by thirteen worlds of a split sheet is one group, and a set
	// rebuilt per world would count it thirteen times.
	groups := map[string]bool{}
	for _, entry := range manifest.Worlds {
		part, held := byWorld[entry.Slug]
		if !held {
			return nil, fmt.Errorf("world %s carries no payload to score", entry.Slug)
		}
		var payload worldPayload
		if err := json.Unmarshal(part.Payload, &payload); err != nil {
			return nil, fmt.Errorf("world %s payload: %w", entry.Slug, err)
		}
		var text map[string]featureText
		if err := json.Unmarshal(part.Text, &text); err != nil {
			return nil, fmt.Errorf("world %s prose: %w", entry.Slug, err)
		}
		world, err := scoreWorld(score, table, entry.Slug, payload, text, part.Locations, &lengths, groups)
		if err != nil {
			return nil, err
		}
		score.Worlds = append(score.Worlds, world)
		score.Total += world.Total
	}
	sort.Ints(lengths)
	if len(lengths) > 0 {
		score.Axes.MedianLength = lengths[len(lengths)/2]
	}
	return score, nil
}

func scoreWorld(
	score *Score,
	table Table,
	slug string,
	payload worldPayload,
	text map[string]featureText,
	locations []bundle.Location,
	lengths *[]int,
	groups map[string]bool,
) (WorldScore, error) {
	out := WorldScore{Slug: slug}

	// A world earns for what it declares about its own ground, and for how much
	// picture a reader can actually get to.
	out.World += table.World.Convention * registered(payload.Attrs, semconv.EntityWorld)
	score.Axes.UnknownAttrs += unknown(payload.Attrs)
	switch payload.Attrs[semconv.KeyGeometrySurface] {
	case semconv.SurfaceSphere:
		score.Axes.Geometry = semconv.SurfaceSphere
	case semconv.SurfacePlane:
		if score.Axes.Geometry != semconv.SurfaceSphere {
			score.Axes.Geometry = semconv.SurfacePlane
		}
	}
	score.Axes.Lenses += len(payload.Lenses)
	for _, lens := range payload.Lenses {
		out.World += table.World.Lens
		if depth := lens.MaxZoom - lens.MinZoom + 1; depth > 0 {
			out.World += table.World.LensZoom * depth
		}
		score.Axes.Depth = max(score.Axes.Depth, lens.MaxZoom)
	}

	// Corroboration is read off the ledger: a serving feature another reading
	// also pinned is a feature two sources agree about.
	corroborated := make(map[int64]int)
	for _, account := range payload.Merged {
		score.Ledger = append(score.Ledger, LedgerLine{World: slug, Account: account})
		for _, pair := range account.Matched {
			corroborated[pair.Winner]++
		}
	}

	// Point collections come first in the payload and own the packed locations
	// by position.
	byOwner := make(map[int][]bundle.Location)
	for _, location := range locations {
		byOwner[int(location.Owner)] = append(byOwner[int(location.Owner)], location)
	}

	pointIndex := 0
	for _, collection := range payload.Collections {
		score.Axes.UnknownAttrs += unknown(collection.Attrs)
		out.Collections += table.Collection.Convention * registered(collection.Attrs, semconv.EntityCollection)

		resolvesIcon := collection.IconAsset != ""
		if collection.Kind == semconv.GeometryPoint {
			score.Axes.Collections++
			if !groups[collection.Group] {
				groups[collection.Group] = true
				score.Axes.Groups++
			}
			if _, declared := collection.Attrs[semconv.KeyRenderAs]; declared {
				score.Axes.RenderDeclared++
			}
			if collection.Attrs[semconv.KeyIconStd] != "" {
				score.Axes.StdIcons++
			}
			labels := semconv.RenderAs(collection.Attrs, "") == semconv.RenderAsText
			if labels {
				score.Axes.TextSets++
			} else {
				score.Axes.IconsWanted++
				if resolvesIcon {
					score.Axes.IconsCarried++
				}
			}
			for _, location := range byOwner[pointIndex] {
				score.Axes.Features++
				held := text[strconv.FormatInt(location.ID, 10)]
				out.Features += scoreFeature(score, table, featureFacts{
					title:         location.Title,
					description:   held.Description,
					attrs:         held.Attrs,
					resolvesIcon:  resolvesIcon,
					corroboration: corroborated[location.ID],
				}, lengths)
				score.Axes.Points++
			}
			pointIndex++
			continue
		}

		for _, feature := range collection.Features {
			vertices := 0
			for _, geometry := range feature.Geometry {
				vertices += countVertices(geometry.Coordinates)
			}
			score.Axes.Shapes++
			score.Axes.Features++
			score.Axes.Vertices += vertices
			description := ""
			if feature.HasText {
				description = text[strconv.FormatInt(feature.ID, 10)].Description
			}
			out.Features += scoreFeature(score, table, featureFacts{
				title:         feature.Title,
				description:   description,
				attrs:         feature.Attrs,
				resolvesIcon:  resolvesIcon,
				vertices:      vertices,
				corroboration: corroborated[feature.ID],
			}, lengths)
		}
	}
	out.Total = out.Features + out.Collections + out.World
	return out, nil
}

type featureFacts struct {
	title         string
	description   string
	attrs         map[string]string
	resolvesIcon  bool
	vertices      int
	corroboration int
}

// scoreFeature is the whole per-feature rule, in one place: every point a
// feature can earn, and nothing it can lose.
func scoreFeature(score *Score, table Table, f featureFacts, lengths *[]int) int {
	points := 0
	if strings.TrimSpace(f.title) != "" {
		points += table.Feature.Name
	}
	cleaned := strings.Join(strings.Fields(f.description), " ")
	if cleaned != "" {
		points += table.Feature.Prose
		if len(cleaned) > table.Feature.SubstanceChars {
			points += table.Feature.ProseSubstance
		}
		score.Axes.Described++
		*lengths = append(*lengths, len(cleaned))
	}
	if f.attrs[semconv.KeyGeoLat] != "" && f.attrs[semconv.KeyGeoLon] != "" {
		points += table.Feature.Geo
		score.Axes.GeoFeatures++
	}
	for _, key := range MembershipKeys {
		if f.attrs[key] != "" {
			points += table.Feature.Membership
			score.Axes.Memberships++
		}
	}
	if f.resolvesIcon {
		points += table.Feature.Icon
	}
	if f.vertices > 0 {
		points += table.Feature.GeometryLog * int(math.Log2(float64(1+f.vertices)))
	}
	points += table.Feature.Corroboration * f.corroboration
	score.Axes.UnknownAttrs += unknown(f.attrs)
	return points
}

// registered counts the attributes that are this entity's registered
// vocabulary. An unregistered key earns nothing: the score measures adoption of
// the shared vocabulary, and a private key is not that.
func registered(attrs map[string]string, entity semconv.Entity) int {
	count := 0
	for key := range attrs {
		if held, known := semconv.EntityOf(key); known && held == entity {
			count++
		}
	}
	return count
}

// unknown counts the atlas-namespace keys the registry does not know.
// Validation refuses them at the producer, so a count above zero means a bundle
// written by a newer vocabulary than this build of the tools.
func unknown(attrs map[string]string) int {
	count := 0
	for key := range attrs {
		if !strings.HasPrefix(key, semconv.Namespace) {
			continue
		}
		if _, known := semconv.EntityOf(key); !known {
			count++
		}
	}
	return count
}

func readEntry(entries map[string]*zip.File, name string, into any) error {
	data, err := readBytes(entries, name)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func readBytes(entries map[string]*zip.File, name string) ([]byte, error) {
	file, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("%s is missing", name)
	}
	opened, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	return io.ReadAll(opened)
}

// tileZoom reads the level out of tiles/<pyramid>/<zoom>/<x>/<y>.<ext>.
func tileZoom(name string) int {
	parts := strings.Split(name, "/")
	if len(parts) < 5 {
		return -1
	}
	zoom, err := strconv.Atoi(parts[len(parts)-3])
	if err != nil {
		return -1
	}
	return zoom
}

// countVertices counts coordinate pairs through however many rings and
// nestings a geometry holds.
func countVertices(raw json.RawMessage) int {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0
	}
	numbers := 0
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case float64:
			numbers++
		}
	}
	walk(value)
	return numbers / 2
}
