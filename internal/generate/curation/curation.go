// Package curation holds the generate lane's editorial decisions as data.
//
// Some things about a volume cannot be derived from its captures: which of a
// game's maps a reader should open on, whether its markers need a light rim or
// a dark one against its art, whether one sheet is really eight places printed
// side by side, and where two sources spell one concept differently. Those are
// judgements about particular archives, and a judgement belongs in a reviewed
// data file where it can be read and argued with, not in a table halfway down a
// program.
//
// The file is curation.json, embedded so a build carries its own curation and a
// composed bundle cannot depend on what happened to be on the operator's disk.
// Its schema is documented in docs/generate.md; this package is the reader and
// the checker.
//
// Every key here is spelled in Atlas's vocabulary -- volume slug, or
// "<volume>/<world>" -- never in a source's identifiers. The tables this
// replaced were keyed by upstream numeric ids, which meant a curation entry was
// unreadable without the source's database in front of you and unmovable to a
// second source describing the same ground.
package curation

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed curation.json
var embedded []byte

// ShardMode says what becomes of a sheet holding several separate places.
type ShardMode string

const (
	// ShardNone is every world nobody curated: one sheet, one world.
	ShardNone ShardMode = ""
	// ShardIntoWorlds gives each piece its own entry in the world picker.
	ShardIntoWorlds ShardMode = "worlds"
	// ShardIntoLenses keeps one world and offers its pieces one at a time, as
	// lenses on the same ground.
	ShardIntoLenses ShardMode = "lenses"
)

// Tables is the whole curated corpus, read.
type Tables struct {
	// Window is the source tile window a world is cut from unless it declares
	// one of its own.
	Window Window

	preferredWorlds map[string][]string
	newestFirst     map[string]bool
	outsetByVolume  map[string]string
	outsetByWorld   map[string]string
	pixelArt        map[string]bool
	shard           map[string]ShardMode
	equivalents     map[string]map[string]string
}

// Window is a tile window: the zoom it is measured at and its first tile.
type Window struct {
	SourceZoom int
	FirstTile  int
}

// Load reads the embedded curation. It is the only way to get Tables, so no
// caller can compose against curation nobody reviewed.
func Load() (Tables, error) { return parse(embedded) }

// LoadFrom reads curation from bytes, for a test that wants to state its own
// corpus rather than the real one.
func LoadFrom(data []byte) (Tables, error) { return parse(data) }

type wire struct {
	Schema int `json:"schema"`
	Window struct {
		SourceZoom int `json:"sourceZoom"`
		FirstTile  int `json:"firstTile"`
	} `json:"window"`
	WorldOrder struct {
		Preferred   map[string][]string `json:"preferred"`
		NewestFirst struct {
			Volumes []string `json:"volumes"`
		} `json:"newestFirst"`
	} `json:"worldOrder"`
	IconOutset struct {
		ByVolume map[string]string `json:"byVolume"`
		ByWorld  map[string]string `json:"byWorld"`
	} `json:"iconOutset"`
	PixelArt struct {
		Volumes []string `json:"volumes"`
	} `json:"pixelArt"`
	Shard struct {
		Worlds map[string]ShardMode `json:"worlds"`
	} `json:"shard"`
	CollectionEquivalents map[string]json.RawMessage `json:"collectionEquivalents"`
}

// Schema is the curation file layout this package reads. It moves when an
// existing field's meaning breaks.
const Schema = 1

func parse(data []byte) (Tables, error) {
	var raw wire
	if err := json.Unmarshal(data, &raw); err != nil {
		return Tables{}, fmt.Errorf("decode curation: %w", err)
	}
	if raw.Schema != Schema {
		return Tables{}, fmt.Errorf("curation schema %d, want %d", raw.Schema, Schema)
	}
	if raw.Window.SourceZoom <= 0 || raw.Window.FirstTile < 0 {
		return Tables{}, fmt.Errorf("curation declares no shared tile window")
	}
	out := Tables{
		Window:          Window{SourceZoom: raw.Window.SourceZoom, FirstTile: raw.Window.FirstTile},
		preferredWorlds: raw.WorldOrder.Preferred,
		newestFirst:     make(map[string]bool, len(raw.WorldOrder.NewestFirst.Volumes)),
		outsetByVolume:  raw.IconOutset.ByVolume,
		outsetByWorld:   raw.IconOutset.ByWorld,
		pixelArt:        make(map[string]bool, len(raw.PixelArt.Volumes)),
		shard:           raw.Shard.Worlds,
		equivalents:     make(map[string]map[string]string, len(raw.CollectionEquivalents)),
	}
	for _, slug := range raw.WorldOrder.NewestFirst.Volumes {
		out.newestFirst[slug] = true
	}
	for _, slug := range raw.PixelArt.Volumes {
		out.pixelArt[slug] = true
	}
	// The equivalents table carries prose keys alongside its volumes, the way
	// every other section does, so a value that is not an object is commentary
	// and not a volume.
	for key, value := range raw.CollectionEquivalents {
		var table map[string]string
		if err := json.Unmarshal(value, &table); err != nil {
			continue
		}
		out.equivalents[key] = table
	}
	for key, mode := range out.shard {
		switch mode {
		case ShardIntoWorlds, ShardIntoLenses:
		default:
			return Tables{}, fmt.Errorf("curation declares shard mode %q for %s", mode, key)
		}
		if !strings.Contains(key, "/") {
			return Tables{}, fmt.Errorf("curation shards %q, which names no world", key)
		}
	}
	for key := range out.outsetByWorld {
		if !strings.Contains(key, "/") {
			return Tables{}, fmt.Errorf("curation sets an outset on %q, which names no world", key)
		}
	}
	return out, nil
}

// PreferredWorlds is the volume's curated world order: the slugs that come
// first, in the order they come. Everything else follows in title order.
func (t Tables) PreferredWorlds(volume string) []string { return t.preferredWorlds[volume] }

// NewestFirst reports whether a volume's worlds are a version history, which
// reads backward: the present opens, the past waits below it.
func (t Tables) NewestFirst(volume string) bool { return t.newestFirst[volume] }

// IconOutset is the rim a world's markers wear, or the empty string where
// nothing is curated. A world's own entry wins over its volume's.
func (t Tables) IconOutset(volume, world string) string {
	if outset, ok := t.outsetByWorld[volume+"/"+world]; ok {
		return outset
	}
	return t.outsetByVolume[volume]
}

// PixelArt reports whether a volume is drawn on a pixel grid, which decides how
// its pyramids are reduced and whether a reader smooths them when magnified.
func (t Tables) PixelArt(volume string) bool { return t.pixelArt[volume] }

// Shard is what becomes of a sheet holding several places, or ShardNone.
func (t Tables) Shard(volume, world string) ShardMode { return t.shard[volume+"/"+world] }

// CollectionEquivalent is the shared name a volume's collection meets other
// sources under, keyed by the collection's artwork key, or the empty string.
func (t Tables) CollectionEquivalent(volume, iconKey string) string {
	return t.equivalents[volume][iconKey]
}
