package main

import (
	"fmt"
	"strings"
)

// Component is one kind of thing a source can contribute to a game's bundle.
type Component string

const (
	// ComponentRaster is the tile pyramids a map is drawn from.
	ComponentRaster Component = "raster"
	// ComponentIcons is the marker artwork a legend draws.
	ComponentIcons Component = "icons"
	// ComponentLocations is the pins themselves: titles, coordinates, notes.
	ComponentLocations Component = "locations"
	// ComponentMetadata is the structure around the pins: categories, groups,
	// zones, the map's own configuration.
	ComponentMetadata Component = "metadata"
)

// ComponentSet is the components a source produces, in a fixed display
// order.
type ComponentSet []Component

// Has reports whether the set carries the component.
func (s ComponentSet) Has(c Component) bool {
	for _, held := range s {
		if held == c {
			return true
		}
	}
	return false
}

// Source is one place the collection draws from. A source declares its
// identity, what it contributes, and how a fetch is spoken to tools/crawl;
// everything downstream of the fetch -- translation, pyramids, composition
// -- is the pipeline's and looks the same whichever source fed it.
//
// Sources are additive. Each contributes components to a game's .atlas, and
// composition merges contributions by title and slug with full provenance in
// the map payload's merged block; no source replaces another's work. The
// additive contract extends to repetition: re-running a source that returns
// unchanged data is a no-op end to end, because captures are content-
// addressed in the archive, bundle stamps are deterministic functions of the
// capture, and the merge ledger is derived from immutable observations --
// the same data can only produce the build that already exists.
type Source interface {
	// Name is the source as a person knows it.
	Name() string
	// Slug is the source as merge ledgers and archives record it.
	Slug() string
	// Components is what this source produces, as a typed set.
	Components() ComponentSet
	// Description says in a sentence or two what the source is and any
	// terms it imposes.
	Description() string
	// TargetHint says what shape a fetch target takes, for forms and errors.
	TargetHint() string
	// FetchArgs returns the tools/crawl arguments selecting this source and
	// target, or an error naming what is wrong with the target. It builds
	// argv and performs no fetch itself.
	FetchArgs(target string) ([]string, error)
}

// crawlSource is a source whose fetch is one tools/crawl flag and a target.
// All three registered sources take this shape; a source that needed its own
// fetcher would implement Source itself.
type crawlSource struct {
	name        string
	slug        string
	description string
	flag        string
	hint        string
	// pair marks a source addressed as two slugs joined by a slash rather
	// than one.
	pair       bool
	components ComponentSet
}

func (s crawlSource) Name() string             { return s.name }
func (s crawlSource) Slug() string             { return s.slug }
func (s crawlSource) Components() ComponentSet { return s.components }
func (s crawlSource) Description() string      { return s.description }
func (s crawlSource) TargetHint() string       { return s.hint }

func (s crawlSource) FetchArgs(target string) ([]string, error) {
	if err := validTarget(target, s.pair); err != nil {
		return nil, fmt.Errorf("%s target %q: %w (expected %s)", s.name, target, err, s.hint)
	}
	return []string{s.flag, target}, nil
}

// validTarget admits targets that are safe as command arguments and as the
// slugs the sources actually use: lowercase names, digits, separators, and
// for pair-addressed sources exactly one slash. A leading dash is refused so
// a target can never be read as a flag.
func validTarget(target string, pair bool) error {
	if target == "" {
		return fmt.Errorf("empty")
	}
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("starts with a dash")
	}
	slashes := 0
	for _, r := range target {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		case r == '/':
			slashes++
		default:
			return fmt.Errorf("contains %q", r)
		}
	}
	if pair && slashes != 1 {
		return fmt.Errorf("needs exactly one slash")
	}
	if !pair && slashes != 0 {
		return fmt.Errorf("takes no slash")
	}
	if strings.HasPrefix(target, "/") || strings.HasSuffix(target, "/") {
		return fmt.Errorf("has an empty half")
	}
	return nil
}

// fullSet is every component: all three registered sources publish complete
// maps, differing in quality per component rather than in kind.
var fullSet = ComponentSet{ComponentRaster, ComponentIcons, ComponentLocations, ComponentMetadata}

// sources is the registry, in the order pages list them. The names here are
// the same names merge ledgers carry, so a ledger line and a plugin card
// point at each other.
var sources = []Source{
	crawlSource{
		name: "MapGenie",
		slug: "mapgenie",
		description: "Community-built interactive maps for hundreds of games: " +
			"the collection's founding source, with deep pyramids and rich category trees.",
		flag:       "-game",
		hint:       "game slug, e.g. cyberpunk-2077",
		components: fullSet,
	},
	crawlSource{
		name: "IGN Wiki",
		slug: "ign-wiki",
		description: "IGN's native wikimaps only -- maps IGN draws itself, not embeds. " +
			"Pins arrive named but largely unwritten; wiki links stay behind, since the reader runs offline.",
		flag:       "-ign",
		hint:       "objectSlug/mapSlug, e.g. cyberpunk-2077/night-city",
		pair:       true,
		components: fullSet,
	},
	crawlSource{
		name: "Piggyback",
		slug: "piggyback",
		description: "The official guide house's Cyberpunk 2077 map, with prose on nearly every pin. " +
			"A deliberately game-specific importer: each Piggyback map projects through a transformation " +
			"buried in its own scripts, and only Cyberpunk's is verified -- other games are refused. " +
			"Its CDN answers only requests carrying a Referer, and the free tier ceilings pyramids at zoom 7.",
		flag:       "-piggyback",
		hint:       "cyberpunk-2077/night-city (the one supported game)",
		pair:       true,
		components: fullSet,
	},
	crawlSource{
		name: "NASA Trek",
		slug: "nasa-trek",
		description: "Planetary maps, proving the format was never about games: NASA Trek's " +
			"global mosaics for the raster, the IAU Gazetteer of Planetary Nomenclature for " +
			"the pins -- every named feature with its place, size, and the origin of its name. " +
			"All public domain. A curated importer: each body names one verified global " +
			"mosaic, and only Mars is curated so far. No icon artwork; the viewer's fallback " +
			"glyphs stand in.",
		flag: "-trek",
		hint: "body, e.g. mars (the one curated body)",
		components: ComponentSet{
			ComponentRaster, ComponentLocations, ComponentMetadata,
		},
	},
	crawlSource{
		name: "ArcGIS Open Data",
		slug: "arcgis-hub",
		description: "A city's ArcGIS Hub open-data site, bending the bundle's words to civic " +
			"ground: the city is the \"game\", each crawl day registers a dated map, and the " +
			"picker reads as the city's version history. Zoning, annexations and wetlands ride " +
			"the zones, named places ride the pins, and the one raster layer is a basemap " +
			"rendered offline from the city's own streets and trails. A curated importer: each " +
			"city names verified datasets and a verified window, and only Bend, Oregon is " +
			"curated so far. Standard-library icons; no source artwork.",
		flag: "-arcgis",
		hint: "city, e.g. bend-or (the one curated city)",
		components: ComponentSet{
			ComponentRaster, ComponentLocations, ComponentMetadata,
		},
	},
}

// sourceBySlug finds one registered source.
func sourceBySlug(slug string) (Source, bool) {
	for _, s := range sources {
		if s.Slug() == slug {
			return s, true
		}
	}
	return nil, false
}
