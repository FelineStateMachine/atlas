// Command generate turns an FMG archive and the generated raster manifest
// into one .atlas bundle per game. The source archive stays outside the Go
// module; everything the application serves for a game travels in its bundle.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/arcgismap"
	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/icons"
	"github.com/FelineStateMachine/atlas/internal/ignmap"
	"github.com/FelineStateMachine/atlas/internal/pbmap"
	"github.com/FelineStateMachine/atlas/internal/semconv"
	"github.com/FelineStateMachine/atlas/internal/trekmap"
)

type archive struct {
	Games []archiveGame `json:"games"`
}

type archiveGame struct {
	Directory string `json:"directory"`
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	// Source names the crawler that filled this directory. Older archives
	// predate the field; an empty value is MapGenie, the original source.
	Source string `json:"source"`
}

type snapshotIndex struct {
	CapturedAt  string `json:"capturedAt"`
	ContentHash string `json:"contentHash"`
	Kind        string `json:"kind"`
}

type rawMap struct {
	ID               int64             `json:"id"`
	Title            string            `json:"title"`
	Slug             string            `json:"slug"`
	InitialLatitude  float64           `json:"initial_latitude"`
	InitialLongitude float64           `json:"initial_longitude"`
	InitialZoom      float64           `json:"initial_zoom"`
	Config           rawConfig         `json:"config"`
	Game             rawGame           `json:"game"`
	Groups           []rawGroup        `json:"groups"`
	Regions          []rawRegion       `json:"regions"`
	Attrs            map[string]string `json:"atlas_attrs"`
}

type rawGame struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type rawConfig struct {
	TileSets []rawTileSet `json:"tile_sets"`
}

type rawTileSet struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type rawGroup struct {
	ID         int64         `json:"id"`
	Title      string        `json:"title"`
	Color      string        `json:"color"`
	IconColor  string        `json:"icon_color"`
	Categories []rawCategory `json:"categories"`
}

type rawCategory struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Icon        string            `json:"icon"`
	Color       string            `json:"color"`
	IconColor   string            `json:"icon_color"`
	DisplayType string            `json:"display_type"`
	Visible     bool              `json:"visible"`
	Locations   []rawLocation     `json:"locations"`
	Attrs       map[string]string `json:"atlas_attrs"`
}

type rawLocation struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Latitude    json.RawMessage   `json:"latitude"`
	Longitude   json.RawMessage   `json:"longitude"`
	RegionID    *int64            `json:"region_id"`
	Attrs       map[string]string `json:"atlas_attrs"`
}

type rawRegion struct {
	ID             int64             `json:"id"`
	Title          string            `json:"title"`
	Subtitle       string            `json:"subtitle"`
	Description    string            `json:"description"`
	ParentRegionID *int64            `json:"parent_region_id"`
	CenterX        json.RawMessage   `json:"center_x"`
	CenterY        json.RawMessage   `json:"center_y"`
	Features       []rawFeature      `json:"features"`
	Attrs          map[string]string `json:"atlas_attrs"`
}

type rawFeature struct {
	Geometry geometry `json:"geometry"`
}

type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// catalog is the generator's working set: everything built from the archive,
// held only long enough to be written out one bundle per game.
type catalog struct {
	TileGrid tileGrid
	Volumes  []catalogVolume
}

type tileGrid struct {
	SourceZoom int `json:"sourceZoom"`
	FirstTile  int `json:"firstTile"`
	TileSize   int `json:"tileSize"`
	Size       int `json:"size"`
}

// worldGrid is where one map's world space sits in the source tile grid. Most
// maps sit in the shared window and say nothing; a map cut from a window of its
// own carries the two numbers that differ, and the catalog's grid answers for
// the rest.
type worldGrid struct {
	SourceZoom int `json:"sourceZoom"`
	FirstTile  int `json:"firstTile"`
}

type catalogVolume struct {
	ID     int64          `json:"id"`
	Title  string         `json:"title"`
	Slug   string         `json:"slug"`
	Worlds []catalogWorld `json:"worlds"`
	// Icons carries the game's category icons by bundle-relative name, read
	// once from the archive so every writer -- the embedded tree today, the
	// game's bundle -- draws on the same bytes.
	Icons map[string][]byte `json:"-"`
}

type catalogWorld struct {
	ID         int64      `json:"id"`
	Title      string     `json:"title"`
	Slug       string     `json:"slug"`
	IconOutset string     `json:"iconOutset,omitempty"`
	Center     coordinate `json:"center"`
	// Attrs is the map's account of itself in the shared conventions --
	// geometry, marker outset -- validated against the registry before a
	// bundle is written.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Grid is carried only by a map whose window is not the shared one, so the
	// catalog reads the same as it did for every map that is.
	Grid   *worldGrid     `json:"grid,omitempty"`
	Lenses []lens         `json:"lenses"`
	Groups []catalogGroup `json:"groups"`
	Zones  []zone         `json:"zones,omitempty"`
	// Parent names the map this one was split out of, so an inset sorts with
	// the sheet it came from rather than alphabetically among unrelated maps.
	Parent    string `json:"parent,omitempty"`
	PinCount  int    `json:"pinCount"`
	UpdatedAt string `json:"updatedAt"`
	// Merged is the account of every other source folded into this map:
	// what matched, what was added, what was held back and why. It rides in
	// the payload as provenance.
	Merged []mergedSource `json:"merged,omitempty"`
}

type coordinate struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
}

type lens struct {
	Name       string         `json:"name"`
	Tiles      string         `json:"tiles"`
	MinZoom    int            `json:"minZoom"`
	MaxZoom    int            `json:"maxZoom"`
	FullZoom   int            `json:"fullZoom"`
	SourceZoom int            `json:"sourceZoom"`
	Formats    []string       `json:"formats"`
	Bounds     *contentBounds `json:"bounds,omitempty"`
	// Surface is the ground the map covers, where Bounds is the window cut from
	// the tile pyramid to draw it. On a piece of a sheet the window is grown to
	// take in the title drawn beside it, so anything dividing the map up -- the
	// geohash grid -- measures the surface instead and leaves no cell on margin.
	Surface     *contentBounds            `json:"surface,omitempty"`
	Interpolate bool                      `json:"interpolate"`
	Background  string                    `json:"background,omitempty"`
	Shard       int64                     `json:"shard,omitempty"`
	Coverage    map[string]*levelCoverage `json:"coverage,omitempty"`
}

type levelCoverage struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Bits string `json:"bits"`
}

type contentBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type tileManifest struct {
	TileSize int                `json:"tileSize"`
	Size     int                `json:"size"`
	Lenses   []tileLensManifest `json:"lenses"`
}

type tileLensManifest struct {
	SourcePath  string                    `json:"sourcePath"`
	AssetPath   string                    `json:"assetPath"`
	Stamp       string                    `json:"stamp"`
	MinZoom     int                       `json:"minZoom"`
	MaxZoom     int                       `json:"maxZoom"`
	FullZoom    int                       `json:"fullZoom"`
	SourceZoom  int                       `json:"sourceZoom"`
	Grid        worldGrid                 `json:"grid"`
	Formats     []string                  `json:"formats"`
	Bounds      *contentBounds            `json:"bounds"`
	Interpolate bool                      `json:"interpolate"`
	Background  string                    `json:"background"`
	Coverage    map[string]*levelCoverage `json:"coverage"`
	// Name and AlignedWith mark a pyramid another source's raster was
	// resampled into this map's world from: it attaches as an additional
	// variant of whichever map draws the layer AlignedWith names.
	Name        string `json:"name"`
	AlignedWith string `json:"alignedWith"`
}

type catalogGroup struct {
	ID         int64             `json:"id"`
	Title      string            `json:"title"`
	Categories []catalogCategory `json:"categories"`
}

type catalogCategory struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`
	IconAsset string `json:"iconAsset,omitempty"`
	// IconPicture marks an icon that already carries its own colours, as
	// against a monochrome glyph whose silhouette is tinted with the category
	// colour. Sliced marker sprites are pictures; icon-font glyphs are not.
	IconPicture bool              `json:"iconPicture,omitempty"`
	Color       string            `json:"color,omitempty"`
	IconColor   string            `json:"iconColor,omitempty"`
	DisplayType string            `json:"displayType"`
	Visible     bool              `json:"visible"`
	Locations   []catalogLocation `json:"locations"`
	// Attrs speaks the conventions for this category -- how it renders, what
	// its icon is -- beside the legacy fields it will one day retire.
	Attrs map[string]string `json:"attrs,omitempty"`
}

type catalogLocation struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Latitude    float64 `json:"lat"`
	Longitude   float64 `json:"lng"`
	RegionID    *int64  `json:"regionId,omitempty"`
	// Shard names the layer this location belongs to on a map split into
	// layers, and is absent on maps that are not.
	Shard int64         `json:"shard,omitempty"`
	Links []catalogLink `json:"links,omitempty"`
	// Attrs never rides the detail payload -- locations there are stripped
	// -- and ships instead in the text file beside the description, read
	// when a pin is opened.
	Attrs map[string]string `json:"-"`
}

// catalogLink is a cross-reference the source wrote as a mapgenie URL, resolved
// to a location in this same map. Atlas is offline, so the URL itself is
// dropped and only the in-catalog target survives.
type catalogLink struct {
	Title      string `json:"title"`
	LocationID int64  `json:"locationId"`
}

type zone struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// Description never rides the detail payload: buildPayload defers it
	// into the text file and leaves HasText as the marker, so a reader
	// fetches a zone's prose only when its card opens.
	Description    string            `json:"-"`
	HasText        bool              `json:"hasText,omitempty"`
	ParentRegionID *int64            `json:"parentRegionId,omitempty"`
	Center         *coordinate       `json:"center,omitempty"`
	Shard          int64             `json:"shard,omitempty"`
	Features       []geometry        `json:"features"`
	Attrs          map[string]string `json:"attrs,omitempty"`
}

var errWorldNotReady = errors.New("world is not ready for embedding")

// preferredWorldOrder keeps a game's primary map ahead of secondary areas.
// Unlisted maps follow in title order, so adding one entry is enough to curate
// a game without restating its complete archive.
var preferredWorldOrder = map[string][]string{
	"fallout-new-vegas": {"mojave-wasteland"},
}

// newestFirstWorlds marks the games whose maps are dated captures of one
// ground: a version history. The picker's first entry is the map the viewer
// opens, and a version history should open on the present, so these games
// sort their date-titled maps newest first.
var newestFirstWorlds = map[string]bool{
	"bend-or": true,
}

// The raster beneath an icon decides which outline is legible, so this is
// declared rather than derived. A game whose maps are all drawn the same way
// says so once; a single map that differs from its game overrides it.
var iconOutsetByVolume = map[string]string{
	"clair-obscur-expedition-33": "dark",
	"cyberpunk-2077":             "dark", // Night City's neon-on-black art, either source
	"fallout-new-vegas":          "dark", // pale Pip-Boy rasters throughout
	"fallout76":                  "dark", // a parchment survey map, pale throughout
	"la-noire":                   "dark",
	"mars":                       "dark", // Viking's pale ochre and MOLA's bright relief wash out a light rim
	"sonic-frontiers":            "dark",
}

var iconOutsetByWorld = map[int64]string{
	3:  "dark", // Skyrim
	18: "dark", // Solstheim
}

func iconOutsetFor(raw rawMap) string {
	if outset, ok := iconOutsetByWorld[raw.ID]; ok {
		return outset
	}
	return iconOutsetByVolume[raw.Game.Slug]
}

func main() {
	source := flag.String("source", "", "path containing fmg-archive")
	tiles := flag.String("tiles", "", "generated tile manifest")
	bundles := flag.String("bundles", "",
		"registry directory receiving .atlas bundles (default: the application's own library)")
	flag.Parse()

	if *source == "" || *tiles == "" {
		fmt.Fprintln(os.Stderr, "generate: -source and -tiles are required")
		os.Exit(2)
	}
	if *bundles == "" {
		// Installing straight into the application's library is the default:
		// a dev run then serves the freshest builds with no pointing, and a
		// running application watching the directory picks them up live.
		library, err := bundle.DefaultDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "generate: resolving the bundle library:", err)
			os.Exit(1)
		}
		*bundles = library
	}
	if err := run(*source, *tiles, *bundles); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run(source, tileManifestPath, bundleDir string) error {
	archiveRoot := filepath.Join(source, "fmg-archive")
	var index archive
	if err := readJSON(filepath.Join(archiveRoot, "archive.json"), &index); err != nil {
		return err
	}
	var tiles tileManifest
	if err := readJSON(tileManifestPath, &tiles); err != nil {
		return err
	}
	tilesByPath := make(map[string]tileLensManifest, len(tiles.Lenses))
	alignedWith := make(map[string][]tileLensManifest)
	for _, variant := range tiles.Lenses {
		if variant.AlignedWith != "" {
			alignedWith[variant.AlignedWith] = append(alignedWith[variant.AlignedWith], variant)
			continue
		}
		tilesByPath[variant.SourcePath] = variant
	}

	out := catalog{
		TileGrid: tileGrid{
			SourceZoom: 13,
			FirstTile:  4064,
			TileSize:   tiles.TileSize,
			Size:       tiles.Size,
		},
	}
	for _, gameRef := range index.Games {
		game, err := buildVolume(archiveRoot, tilesByPath, alignedWith, gameRef, out.TileGrid)
		if err != nil {
			return fmt.Errorf("%s: %w", gameRef.Title, err)
		}
		if len(game.Worlds) > 0 {
			out.Volumes = append(out.Volumes, game)
		}
	}

	merged, err := mergeAcrossSources(out.Volumes, out.TileGrid)
	if err != nil {
		return err
	}
	out.Volumes = merged

	for index := range out.Volumes {
		if err := resolveStandardIcons(&out.Volumes[index]); err != nil {
			return fmt.Errorf("%s: %w", out.Volumes[index].Slug, err)
		}
	}

	sort.Slice(out.Volumes, func(i, j int) bool { return out.Volumes[i].Title < out.Volumes[j].Title })
	return writeBundles(out, tiles, filepath.Dir(tileManifestPath), bundleDir)
}

func buildVolume(
	archiveRoot string,
	tilesByPath map[string]tileLensManifest,
	alignedWith map[string][]tileLensManifest,
	ref archiveGame,
	grid tileGrid,
) (catalogVolume, error) {
	gamePath := filepath.Join(archiveRoot, ref.Directory)
	mapDirs, err := filepath.Glob(filepath.Join(gamePath, "maps", "*"))
	if err != nil {
		return catalogVolume{}, err
	}
	game := catalogVolume{ID: ref.ID, Title: ref.Title}
	for _, mapDir := range mapDirs {
		info, err := os.Stat(mapDir)
		if err != nil || !info.IsDir() {
			continue
		}
		pieces, gameSlug, err := buildWorld(mapDir, tilesByPath, alignedWith, grid)
		if err != nil {
			if errors.Is(err, errWorldNotReady) {
				continue
			}
			return catalogVolume{}, err
		}
		if game.Slug == "" {
			game.Slug = gameSlug
		}
		// Every map opens its account with where it came from, merged with
		// anything or not: provenance is part of the map, not a side effect
		// of composition. The account carries both spellings of the source:
		// the label a person reads and the canonical slug the workbench's
		// registry names it by, so a ledger line and a plugin card point at
		// each other without a translation table.
		for index := range pieces {
			pieces[index].Merged = []mergedSource{{
				Source:    sourceDisplayLabel(ref.Source),
				Slug:      canonicalSourceSlug(ref.Source),
				Origin:    true,
				DonorPins: pieces[index].PinCount,
			}}
		}
		game.Worlds = append(game.Worlds, pieces...)
	}
	sortVolumeWorlds(game.Slug, game.Worlds)
	if err := attachVolumeIcons(gamePath, &game); err != nil {
		return catalogVolume{}, err
	}
	if err := speakConventions(&game); err != nil {
		return catalogVolume{}, fmt.Errorf("%s: %w", game.Slug, err)
	}
	return game, nil
}

// speakConventions makes every map answer in the shared vocabulary,
// whatever its source knew how to say. A capture that declared its
// attributes keeps them; one that predates the conventions -- every
// MapGenie snapshot -- has them spoken for it from the same rules that
// used to be unwritten: the display type it carried, the icon it resolved
// to, the outset its curation chose. Then the whole game is held to the
// registry, so an unregistered key or a foreign value fails here, one
// build old, rather than riding into a bundle.
func speakConventions(game *catalogVolume) error {
	for mapIndex := range game.Worlds {
		m := &game.Worlds[mapIndex]
		if m.IconOutset != "" {
			m.Attrs = withAttr(m.Attrs, semconv.KeyIconOutset, m.IconOutset)
		}
		if err := semconv.Validate(semconv.EntityWorld, m.Attrs); err != nil {
			return fmt.Errorf("world %s: %w", m.Slug, err)
		}
		for zoneIndex := range m.Zones {
			if err := semconv.Validate(semconv.EntityZone, m.Zones[zoneIndex].Attrs); err != nil {
				return fmt.Errorf("world %s zone %q: %w", m.Slug, m.Zones[zoneIndex].Title, err)
			}
		}
		for groupIndex := range m.Groups {
			categories := m.Groups[groupIndex].Categories
			for categoryIndex := range categories {
				category := &categories[categoryIndex]
				if _, declared := category.Attrs[semconv.KeyRenderAs]; !declared {
					category.Attrs = withAttr(category.Attrs, semconv.KeyRenderAs,
						semconv.RenderAs(nil, category.DisplayType))
				}
				// Where two sources are known to spell one concept two ways,
				// the shared name rides the category itself, so the merge
				// reads identity off the payload rather than a table of its
				// own.
				if shared := categoryEquivalents[game.Slug][category.Icon]; shared != "" {
					if _, declared := category.Attrs[semconv.KeyCategoryKey]; !declared {
						category.Attrs = withAttr(category.Attrs, semconv.KeyCategoryKey, shared)
					}
				}
				if category.IconAsset != "" {
					kind := semconv.IconKindGlyph
					if category.IconPicture {
						kind = semconv.IconKindPicture
					}
					category.Attrs = withAttr(category.Attrs, semconv.KeyIconKind, kind)
				}
				if err := semconv.Validate(semconv.EntityCategory, category.Attrs); err != nil {
					return fmt.Errorf("world %s category %q: %w", m.Slug, category.Title, err)
				}
				for _, location := range category.Locations {
					if err := semconv.Validate(semconv.EntityLocation, location.Attrs); err != nil {
						return fmt.Errorf("world %s pin %q: %w", m.Slug, location.Title, err)
					}
				}
			}
		}
	}
	return nil
}

// withAttr sets one attribute on a copy of the map, never the map itself: a
// split sheet's pieces share their source's attributes by reference, and
// speaking for one piece must not put words in another's mouth.
func withAttr(attrs map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(attrs)+1)
	maps.Copy(out, attrs)
	out[key] = value
	return out
}

// resolveStandardIcons makes good on every atlas.icon.std declaration: the
// named library glyph lands in the game's icon set under its provenance-
// spelling name, and the category wears it exactly the way it would wear a
// source's own artwork. It runs after composition so merged-in categories
// resolve too, and only where a source's own icon has not already won the
// slot. A declaration the library cannot answer fails the build: the
// promise was made in a translator, and it is kept here or heard about.
func resolveStandardIcons(game *catalogVolume) error {
	for mapIndex := range game.Worlds {
		m := &game.Worlds[mapIndex]
		for groupIndex := range m.Groups {
			categories := m.Groups[groupIndex].Categories
			for categoryIndex := range categories {
				category := &categories[categoryIndex]
				ref := category.Attrs[semconv.KeyIconStd]
				if ref == "" || category.IconAsset != "" {
					continue
				}
				data, asset, err := icons.Standard(ref)
				if err != nil {
					return fmt.Errorf("world %s category %q: %w", m.Slug, category.Title, err)
				}
				if game.Icons == nil {
					game.Icons = make(map[string][]byte)
				}
				game.Icons[asset] = data
				category.IconAsset = asset
				category.IconPicture = false
				category.Attrs = withAttr(category.Attrs, semconv.KeyIconKind, semconv.IconKindGlyph)
			}
		}
	}
	return nil
}

func sortVolumeWorlds(gameSlug string, maps []catalogWorld) {
	order := make(map[string]int)
	for index, slug := range preferredWorldOrder[gameSlug] {
		order[slug] = index
	}
	// A version-history game reads its date titles backward: the newest
	// capture opens first, and the past waits one click below it.
	after := func(a, b string) bool { return a < b }
	if newestFirstWorlds[gameSlug] {
		after = func(a, b string) bool { return a > b }
	}
	titles := make(map[string]string, len(maps))
	for _, m := range maps {
		titles[m.Slug] = m.Title
	}
	// Maps sort as families: an inset carries its parent's position and follows
	// it, so a split sheet stays together in the picker.
	family := func(m catalogWorld) (string, string) {
		if m.Parent == "" {
			return m.Slug, m.Title
		}
		return m.Parent, titles[m.Parent]
	}
	sort.SliceStable(maps, func(i, j int) bool {
		leftSlug, leftTitle := family(maps[i])
		rightSlug, rightTitle := family(maps[j])
		left, leftPreferred := order[leftSlug]
		right, rightPreferred := order[rightSlug]
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		if leftPreferred && left != right {
			return left < right
		}
		if leftTitle != rightTitle {
			return after(leftTitle, rightTitle)
		}
		if (maps[i].Parent == "") != (maps[j].Parent == "") {
			return maps[i].Parent == ""
		}
		return after(maps[i].Title, maps[j].Title)
	})
}

// buildWorld returns one entry per map, or several when a sheet holding separate
// places is declared for splitting.
func buildWorld(
	mapDir string,
	tilesByPath map[string]tileLensManifest,
	alignedWith map[string][]tileLensManifest,
	grid tileGrid,
) ([]catalogWorld, string, error) {
	var snapshots []snapshotIndex
	indexPath := filepath.Join(mapDir, "snapshots", "index.json")
	if err := readJSON(indexPath, &snapshots); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("%w: snapshot index is missing", errWorldNotReady)
		}
		return nil, "", err
	}
	if len(snapshots) == 0 {
		return nil, "", fmt.Errorf("%w: snapshot index is empty", errWorldNotReady)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt < snapshots[j].CapturedAt })
	latest := snapshots[len(snapshots)-1]

	// A capture from another source is translated into the MapGenie shape on
	// the way in; a MapGenie snapshot passes through untouched.
	doc, err := os.ReadFile(filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json"))
	if err != nil {
		return nil, "", err
	}
	if doc, err = ignmap.MaybeTranslate(latest.Kind, doc); err != nil {
		return nil, "", err
	}
	if doc, err = pbmap.MaybeTranslate(latest.Kind, doc); err != nil {
		return nil, "", err
	}
	if doc, err = trekmap.MaybeTranslate(latest.Kind, doc); err != nil {
		return nil, "", err
	}
	if doc, err = arcgismap.MaybeTranslate(latest.Kind, doc); err != nil {
		return nil, "", err
	}
	var raw rawMap
	if err := json.Unmarshal(doc, &raw); err != nil {
		return nil, "", err
	}

	m := catalogWorld{
		ID:         raw.ID,
		Title:      raw.Title,
		Slug:       raw.Slug,
		IconOutset: iconOutsetFor(raw),
		Center:     coordinate{Latitude: raw.InitialLatitude, Longitude: raw.InitialLongitude},
		UpdatedAt:  latest.CapturedAt,
		Attrs:      raw.Attrs,
	}
	if len(raw.Config.TileSets) == 0 {
		return nil, "", fmt.Errorf("%w: no tile sets", errWorldNotReady)
	}
	// The catalog's grid is the window most maps are cut from; this map's own
	// takes its place below, and what is left of the shared one -- the size of
	// the world and of a tile, which no map differs on -- carries through.
	shared := grid
	for _, set := range raw.Config.TileSets {
		tiles, ok := tilesByPath[set.Path]
		if !ok {
			return nil, "", fmt.Errorf("%w: tile layer %s is missing", errWorldNotReady, set.Path)
		}
		// Every layer of a map is a picture of the same ground, so they agree on
		// the window it is cut from. One that does not would be a map drawn in
		// two places at once, and no pin could be placed on it.
		window := tiles.Grid
		if window == (worldGrid{}) {
			// An index written before layers carried their window: the shared
			// one is what every map in it was cut from.
			window = worldGrid{SourceZoom: shared.SourceZoom, FirstTile: shared.FirstTile}
		}
		if len(m.Lenses) == 0 {
			grid.SourceZoom, grid.FirstTile = window.SourceZoom, window.FirstTile
			if window != (worldGrid{SourceZoom: shared.SourceZoom, FirstTile: shared.FirstTile}) {
				m.Grid = &window
			}
		} else if window.SourceZoom != grid.SourceZoom || window.FirstTile != grid.FirstTile {
			return nil, "", fmt.Errorf("%w: tile layer %s sits in a different window from %s",
				errWorldNotReady, set.Path, raw.Config.TileSets[0].Path)
		}
		m.Lenses = append(m.Lenses, lens{
			Name:        set.Name,
			Tiles:       tiles.AssetPath,
			MinZoom:     tiles.MinZoom,
			MaxZoom:     tiles.MaxZoom,
			FullZoom:    tiles.FullZoom,
			SourceZoom:  tiles.SourceZoom,
			Formats:     tiles.Formats,
			Bounds:      tiles.Bounds,
			Interpolate: tiles.Interpolate,
			Background:  tiles.Background,
			Coverage:    tiles.Coverage,
		})
	}
	// Another source's raster, resampled into this map's world, arrives as
	// one more way to see the same ground. It was rendered in this map's
	// window, so it passes the same agreement every native layer passes.
	for _, set := range raw.Config.TileSets {
		for _, aligned := range alignedWith[set.Path] {
			if aligned.Grid.SourceZoom != grid.SourceZoom || aligned.Grid.FirstTile != grid.FirstTile {
				return nil, "", fmt.Errorf("aligned layer %s was rendered in a different window", aligned.SourcePath)
			}
			m.Lenses = append(m.Lenses, lens{
				Name:        aligned.Name,
				Tiles:       aligned.AssetPath,
				MinZoom:     aligned.MinZoom,
				MaxZoom:     aligned.MaxZoom,
				FullZoom:    aligned.FullZoom,
				SourceZoom:  aligned.SourceZoom,
				Formats:     aligned.Formats,
				Bounds:      aligned.Bounds,
				Interpolate: aligned.Interpolate,
				Background:  aligned.Background,
				Coverage:    aligned.Coverage,
			})
		}
	}
	for _, rawGroup := range raw.Groups {
		group := catalogGroup{ID: rawGroup.ID, Title: rawGroup.Title}
		for _, rawCategory := range rawGroup.Categories {
			category := catalogCategory{
				ID:          rawCategory.ID,
				Title:       rawCategory.Title,
				Icon:        rawCategory.Icon,
				Color:       resolvedCategoryColor(rawGroup, rawCategory),
				IconColor:   resolvedIconColor(rawGroup, rawCategory),
				DisplayType: rawCategory.DisplayType,
				Visible:     rawCategory.Visible,
				Attrs:       rawCategory.Attrs,
			}
			for _, rawLocation := range rawCategory.Locations {
				lat, err := number(rawLocation.Latitude)
				if err != nil {
					return nil, "", fmt.Errorf("location %d latitude: %w", rawLocation.ID, err)
				}
				lng, err := number(rawLocation.Longitude)
				if err != nil {
					return nil, "", fmt.Errorf("location %d longitude: %w", rawLocation.ID, err)
				}
				category.Locations = append(category.Locations, catalogLocation{
					ID:          rawLocation.ID,
					Title:       rawLocation.Title,
					Description: rawLocation.Description,
					Latitude:    lat,
					Longitude:   lng,
					RegionID:    rawLocation.RegionID,
					Attrs:       rawLocation.Attrs,
				})
				m.PinCount++
			}
			group.Categories = append(group.Categories, category)
		}
		m.Groups = append(m.Groups, group)
	}
	for _, rawRegion := range raw.Regions {
		z := zone{
			ID:             rawRegion.ID,
			Title:          rawRegion.Title,
			Subtitle:       rawRegion.Subtitle,
			Description:    rawRegion.Description,
			ParentRegionID: rawRegion.ParentRegionID,
			Attrs:          rawRegion.Attrs,
		}
		centerX, hasX, err := optionalNumber(rawRegion.CenterX)
		if err != nil {
			return nil, "", fmt.Errorf("region %d center_x: %w", rawRegion.ID, err)
		}
		centerY, hasY, err := optionalNumber(rawRegion.CenterY)
		if err != nil {
			return nil, "", fmt.Errorf("region %d center_y: %w", rawRegion.ID, err)
		}
		if hasX && hasY {
			z.Center = &coordinate{Latitude: centerY, Longitude: centerX}
		}
		for _, feature := range rawRegion.Features {
			if feature.Geometry.Type != "" && len(feature.Geometry.Coordinates) > 0 {
				z.Features = append(z.Features, feature.Geometry)
			}
		}
		if len(z.Features) > 0 {
			m.Zones = append(m.Zones, z)
		}
	}
	resolveDescriptionLinks(&m)
	pieces, err := splitWorld(m, raw, grid)
	if err != nil {
		return nil, "", err
	}
	for index := range pieces {
		markSurfaces(&pieces[index], grid)
	}
	return pieces, raw.Game.Slug, nil
}

// Labels may themselves contain a bracketed aside, as in
// "[Oh Baby! [Super Sledge]](url)", so one level of nesting is allowed.
var markdownLink = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\[\]]*\])*)\]\(([^)\s]+)[^)]*\)`)
var mapgenieLocation = regexp.MustCompile(`locationIds=(\d+)`)

// Some descriptions carry malformed markdown whose URL never sat inside a link,
// e.g. "[Boss] (Lv. 55) (https://…)". Nothing may ship a live URL, so any that
// survive link rewriting are removed outright.
var bareURL = regexp.MustCompile(`\s*\(?\s*https?://[^\s)]+\)?`)

// resolveDescriptionLinks strips every external URL out of location
// descriptions. Atlas ships with no network, so a mapgenie or YouTube link is
// dead weight at best. Where the link pointed at another location in this same
// map, the target is kept as a structured cross-reference the viewer can
// navigate to instead.
func resolveDescriptionLinks(m *catalogWorld) {
	known := make(map[int64]bool)
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				known[location.ID] = true
			}
		}
	}
	for groupIndex := range m.Groups {
		categories := m.Groups[groupIndex].Categories
		for categoryIndex := range categories {
			locations := categories[categoryIndex].Locations
			for locationIndex := range locations {
				location := &locations[locationIndex]
				if !strings.Contains(location.Description, "http") {
					continue
				}
				location.Description = markdownLink.ReplaceAllStringFunc(
					location.Description,
					func(match string) string {
						parts := markdownLink.FindStringSubmatch(match)
						label, target := parts[1], parts[2]
						id := mapgenieLocation.FindStringSubmatch(target)
						if id != nil && !strings.HasPrefix(label, "!") {
							if value, err := strconv.ParseInt(id[1], 10, 64); err == nil &&
								known[value] && value != location.ID {
								location.Links = append(location.Links, catalogLink{
									Title:      label,
									LocationID: value,
								})
							}
						}
						return label
					},
				)
				location.Description = strings.TrimSpace(bareURL.ReplaceAllString(location.Description, ""))
			}
		}
	}
}

func attachVolumeIcons(gamePath string, game *catalogVolume) error {
	if game.Slug == "" {
		return nil
	}
	game.Icons = make(map[string][]byte)
	copied := make(map[string]string)
	for mapIndex := range game.Worlds {
		for groupIndex := range game.Worlds[mapIndex].Groups {
			categories := game.Worlds[mapIndex].Groups[groupIndex].Categories
			for categoryIndex := range categories {
				category := &categories[categoryIndex]
				if !validIconKey(category.Icon) {
					continue
				}
				asset, found := copied[category.Icon]
				if !found {
					var err error
					asset, err = readVolumeIcon(gamePath, game, category.Icon)
					if err != nil {
						return err
					}
					copied[category.Icon] = asset
				}
				category.IconAsset = asset
				category.IconPicture = strings.HasSuffix(asset, ".png")
			}
		}
	}
	return nil
}

// readVolumeIcon takes whichever form the archive holds. Most games publish an
// icon font that renders to SVG; some publish a marker strip instead, which
// slices into PNG. The bytes ride the game into its bundle, named relative
// to the bundle's own icons tree.
func readVolumeIcon(gamePath string, game *catalogVolume, icon string) (string, error) {
	for _, candidate := range []string{".svg", ".png"} {
		source := filepath.Join(gamePath, "icons", icon+candidate)
		contents, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read icon %s: %w", source, err)
		}
		game.Icons[icon+candidate] = contents
		return icon + candidate, nil
	}
	return "", nil
}

func validIconKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func resolvedCategoryColor(group rawGroup, category rawCategory) string {
	if color := normalizeHexColor(category.Color); color != "" {
		return color
	}
	return normalizeHexColor(group.Color)
}

func resolvedIconColor(group rawGroup, category rawCategory) string {
	if color := normalizeHexColor(category.IconColor); color != "" {
		return color
	}
	return normalizeHexColor(group.IconColor)
}

func normalizeHexColor(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	switch len(value) {
	case 3, 4, 6, 8:
	default:
		return ""
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return ""
	}
	return "#" + strings.ToUpper(value)
}

func number(raw json.RawMessage) (float64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
		value = text
	}
	return strconv.ParseFloat(value, 64)
}

func optionalNumber(raw json.RawMessage) (float64, bool, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false, nil
	}
	n, err := number(raw)
	return n, err == nil, err
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
