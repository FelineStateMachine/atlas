// Package measure is the one place a build of a game is judged. The
// cartograph workbench and the maturity report used to each carry their own
// copy of the same yardstick -- and the registry's ordering rule lived in
// three places at once -- so "how good is this build" could quietly mean
// different things in different rooms. Now the axes are defined once,
// against the same semantic conventions the payloads speak: the metrics
// measure adoption of the vocabulary, and the vocabulary defines what the
// metrics mean.
//
// Every number is an absolute measurement of one build, never a rank within
// the collection: ranks move when the collection does, and a judgement worth
// acting on needs figures that stand still.
package measure

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// Build is one .atlas file measured whole: its identity, the axes, and every
// merge account its payloads carry.
type Build struct {
	Path      string
	File      string
	GameSlug  string
	GameTitle string
	Stamp     string
	CreatedAt string
	Revision  int
	MapSlugs  []string

	Pins         int
	Described    int
	MedianLength int
	TileCount    int
	RasterBytes  int64
	Depth        int
	DepthTiles   int
	Categories   int
	Groups       int
	TextSets     int
	Zones        int
	Vertices     int
	Variants     int
	IconsCarried int
	IconsWanted  int

	// The conventions axes: which vocabulary the bundle declares, how much
	// of it the payloads actually speak, and whether they say anything the
	// registry does not know -- which a valid bundle never does, so a
	// non-zero count is itself a finding.
	Conventions    int
	RenderDeclared int
	StdIcons       int
	GeoPins        int
	Geometry       string
	UnknownAttrs   int

	Merges []MergeAccount
}

// MergeAccount is one source's account of its merge into one map, kept whole
// rather than counted: the pairs and the held pins are what a diff compares.
type MergeAccount struct {
	Map       string
	Source    string
	DonorPins int
	Matched   []MatchedPair
	Added     int
	Adopted   []AdoptedPin
	Held      []HeldPin
	Rejected  []HeldPin
	Alignment string
}

// MatchedPair records one place both sources pin, in the ledger's own words.
type MatchedPair struct {
	Donor      int64 `json:"d"`
	Winner     int64 `json:"w"`
	DistancePx int   `json:"px"`
	Enriched   bool  `json:"e"`
}

// AdoptedPin records a donor-only pin that joined one of the serving map's
// own categories.
type AdoptedPin struct {
	Donor int64  `json:"d"`
	Into  string `json:"into"`
}

// HeldPin is a donor pin the merge did not carry, with the reason.
type HeldPin struct {
	Donor  int64  `json:"d"`
	Title  string `json:"t"`
	Reason string `json:"why"`
}

// mapDetail is the sliver of a map payload measurement reads.
type mapDetail struct {
	Attrs  map[string]string `json:"attrs"`
	Groups []struct {
		Categories []struct {
			IconAsset   string            `json:"iconAsset"`
			DisplayType string            `json:"displayType"`
			Attrs       map[string]string `json:"attrs"`
		} `json:"categories"`
	} `json:"groups"`
	Merged []struct {
		Source    string        `json:"source"`
		DonorPins int           `json:"donorPins"`
		Matched   []MatchedPair `json:"matched"`
		Added     int           `json:"added"`
		Adopted   []AdoptedPin  `json:"adopted"`
		Held      []HeldPin     `json:"held"`
		Rejected  []HeldPin     `json:"rejected"`
		Alignment string        `json:"alignment"`
	} `json:"merged"`
	Zones []struct {
		Features []struct {
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"features"`
	} `json:"zones"`
	Variants []struct {
		Name    string `json:"name"`
		MaxZoom int    `json:"maxZoom"`
	} `json:"variants"`
}

// MedianMatchPx is the middle distance of the account's matched pairs: the
// figure that says how well the alignment held, unmoved by one outlier.
func (m MergeAccount) MedianMatchPx() int {
	if len(m.Matched) == 0 {
		return 0
	}
	distances := make([]int, 0, len(m.Matched))
	for _, pair := range m.Matched {
		distances = append(distances, pair.DistancePx)
	}
	sort.Ints(distances)
	return distances[len(distances)/2]
}

func (m MergeAccount) MatchedN() int  { return len(m.Matched) }
func (m MergeAccount) AdoptedN() int  { return len(m.Adopted) }
func (m MergeAccount) HeldN() int     { return len(m.Held) }
func (m MergeAccount) RejectedN() int { return len(m.Rejected) }

// DescribedPct is the annotation share as pages print it.
func (b *Build) DescribedPct() string { return Percent(b.Described, b.Pins) }

// IconPct is the icon coverage as pages print it.
func (b *Build) IconPct() string { return Percent(b.IconsCarried, b.IconsWanted) }

// RenderPct is the share of categories that declare how they render.
func (b *Build) RenderPct() string { return Percent(b.RenderDeclared, b.Categories) }

// RasterMB is the unique raster weight in megabytes, one decimal.
func (b *Build) RasterMB() string { return fmt.Sprintf("%.1f", float64(b.RasterBytes)/1e6) }

// ShortStamp is the stamp as pages carry it.
func (b *Build) ShortStamp() string { return bundle.ShortStamp(b.Stamp) }

// SourcesSeen lists the distinct sources the build's merge accounts name,
// sorted. A bundle is source-agnostic by design, so the ledger is the only
// place a source's name survives into a build.
func (b *Build) SourcesSeen() []string {
	seen := make(map[string]bool)
	var names []string
	for _, account := range b.Merges {
		if !seen[account.Source] {
			seen[account.Source] = true
			names = append(names, account.Source)
		}
	}
	sort.Strings(names)
	return names
}

// Newer reports whether a should stand before b: the registry's own fold,
// bundle.MoreRecent, restated over measurements so every report lists builds
// in the order the reader would actually serve them. This is the one copy
// outside the registry itself.
func Newer(a, b *Build) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	if a.Revision != b.Revision {
		return a.Revision > b.Revision
	}
	if a.Stamp != b.Stamp {
		return a.Stamp > b.Stamp
	}
	return a.Path > b.Path
}

// Percent is the share as every page and report prints it, with an em dash
// where there is nothing to take a share of.
func Percent(part, whole int) string {
	if whole == 0 {
		return "—"
	}
	return strconv.Itoa(part*100/whole) + "%"
}

// MeasureBundle reads one .atlas whole and returns its measurements. It
// reads the zip directly rather than through bundle.Open because the
// cartography axis needs what only the archive's table of contents knows:
// every tile entry's checksum and size, for de-duplicating filler.
func MeasureBundle(path string) (*Build, error) {
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
	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	b := &Build{
		Path:        path,
		File:        filepath.Base(path),
		GameSlug:    manifest.Game.Slug,
		GameTitle:   manifest.Game.Title,
		Stamp:       manifest.Version.Stamp,
		CreatedAt:   manifest.Version.CreatedAt,
		Revision:    manifest.Version.Revision,
		Conventions: manifest.Conventions,
		Geometry:    "plane-default",
	}

	// Annotation: how thoroughly the locations are explained in writing.
	var lengths []int
	for _, entry := range manifest.Maps {
		b.MapSlugs = append(b.MapSlugs, entry.Slug)
		b.Pins += entry.PinCount
		var text map[string]struct {
			Description string            `json:"d"`
			Attrs       map[string]string `json:"a"`
		}
		if err := readEntry(entries, "maps/"+entry.Slug+".text", &text); err != nil {
			return nil, err
		}
		for _, held := range text {
			if held.Attrs[semconv.KeyGeoLat] != "" && held.Attrs[semconv.KeyGeoLon] != "" {
				b.GeoPins++
			}
			b.UnknownAttrs += unknownAttrs(held.Attrs)
			cleaned := strings.Join(strings.Fields(held.Description), " ")
			if cleaned == "" {
				continue
			}
			b.Described++
			lengths = append(lengths, len(cleaned))
		}

		var detail mapDetail
		if err := readEntry(entries, "maps/"+entry.Slug+".json", &detail); err != nil {
			return nil, err
		}
		b.UnknownAttrs += unknownAttrs(detail.Attrs)
		switch detail.Attrs[semconv.KeyGeometrySurface] {
		case semconv.SurfaceSphere:
			b.Geometry = semconv.SurfaceSphere
		case semconv.SurfacePlane:
			if b.Geometry != semconv.SurfaceSphere {
				b.Geometry = semconv.SurfacePlane
			}
		}
		b.Variants += len(detail.Variants)
		for _, variant := range detail.Variants {
			b.Depth = max(b.Depth, variant.MaxZoom)
		}
		b.Zones += len(detail.Zones)
		for _, zone := range detail.Zones {
			for _, feature := range zone.Features {
				b.Vertices += countVertices(feature.Coordinates)
			}
		}
		for _, group := range detail.Groups {
			b.Groups++
			for _, category := range group.Categories {
				b.Categories++
				b.UnknownAttrs += unknownAttrs(category.Attrs)
				if _, declared := category.Attrs[semconv.KeyRenderAs]; declared {
					b.RenderDeclared++
				}
				if category.Attrs[semconv.KeyIconStd] != "" {
					b.StdIcons++
				}
				// The conventions decide what a category is, so the metric
				// judging it reads the same attribute the viewer does.
				if semconv.RenderAs(category.Attrs, category.DisplayType) == semconv.RenderAsText {
					b.TextSets++
					continue
				}
				b.IconsWanted++
				if category.IconAsset != "" {
					b.IconsCarried++
				}
			}
		}
		for _, merged := range detail.Merged {
			b.Merges = append(b.Merges, MergeAccount{
				Map:       entry.Slug,
				Source:    merged.Source,
				DonorPins: merged.DonorPins,
				Matched:   merged.Matched,
				Added:     merged.Added,
				Adopted:   merged.Adopted,
				Held:      merged.Held,
				Rejected:  merged.Rejected,
				Alignment: merged.Alignment,
			})
		}
	}
	sort.Ints(lengths)
	if len(lengths) > 0 {
		b.MedianLength = lengths[len(lengths)/2]
	}

	// Cartography: the stored representation, de-duplicated -- a border of
	// repeated filler tiles is one tile's worth of information however many
	// times it appears.
	seen := make(map[[2]uint64]bool)
	for name, file := range entries {
		if !strings.HasPrefix(name, "tiles/") {
			continue
		}
		b.TileCount++
		key := [2]uint64{uint64(file.CRC32), file.UncompressedSize64}
		if !seen[key] {
			seen[key] = true
			b.RasterBytes += int64(file.UncompressedSize64)
		}
		if zoom := tileZoom(name); zoom == b.Depth {
			b.DepthTiles++
		}
	}
	return b, nil
}

// unknownAttrs counts the atlas-namespace keys the registry does not know.
// Validation refuses them at the producer, so a count above zero means a
// bundle written by a newer vocabulary than this build of the tools.
func unknownAttrs(attrs map[string]string) int {
	unknown := 0
	for key := range attrs {
		if !strings.HasPrefix(key, "atlas.") {
			continue
		}
		if _, known := semconv.EntityOf(key); !known {
			unknown++
		}
	}
	return unknown
}

// LoadPins reads the packed locations of every map, keyed the way a diff
// compares them: by map slug, then pin id.
func LoadPins(path string, mapSlugs []string) (map[string]map[int64]string, error) {
	opened, err := bundle.Open(path)
	if err != nil {
		return nil, err
	}
	defer opened.Close()

	pins := make(map[string]map[int64]string, len(mapSlugs))
	for _, slug := range mapSlugs {
		packed, err := opened.ReadEntry("maps/" + slug + ".bin")
		if err != nil {
			return nil, fmt.Errorf("map %s: %w", slug, err)
		}
		locations, err := bundle.UnpackLocations(packed)
		if err != nil {
			return nil, fmt.Errorf("map %s: %w", slug, err)
		}
		titles := make(map[int64]string, len(locations))
		for _, location := range locations {
			titles[location.ID] = location.Title
		}
		pins[slug] = titles
	}
	return pins, nil
}

func readEntry(entries map[string]*zip.File, name string, into any) error {
	file, ok := entries[name]
	if !ok {
		return fmt.Errorf("%s is missing", name)
	}
	opened, err := file.Open()
	if err != nil {
		return err
	}
	defer opened.Close()
	return json.NewDecoder(opened).Decode(into)
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
