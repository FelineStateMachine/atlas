// Package compose turns an interchange document into a valid volume.
//
// It is the second half of the generate lane: a source has said what it has to
// say, and composition makes a .atlas file of it. That means normalizing to the
// format's unified model, speaking the semantic conventions on the volume's
// behalf and holding every attribute to the registry producer-strict, splitting
// what needs splitting, packing the three payload parts and the tile pyramids,
// stamping the result, and writing it into a registry so carefully that a reader
// scanning mid-write sees either no file or a whole one.
//
// Composition is single-source. What one source said travels here whole and
// unmixed; folding two sources' readings of one volume together is the enrich
// lane's work, and the goldens check the composed result of both together.
//
// # Determinism
//
// The same document and the same tile set give the same bundle, byte for byte,
// on any machine -- the same stamp, and therefore the same file name. Nothing
// here reads a clock: a build's creation time is the newest capture time in its
// document, so a rebuild years later of an archive nobody touched writes the
// file that is already there. That is the whole reason a directory of these
// files can stand as a registry.
package compose

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// PolicyRevision orders builds of the same capture. The data has not moved
// between them, but what this lane makes of a capture has -- a merge rule, a
// kept field, a dropped collection -- and among equal captures a registry
// serves the highest revision. It is bumped when a policy change should
// supersede the builds already in every library.
//
//	1  first revisioned builds
//	2  merge resolution: subset names, adoption, one-to-one matches
//	3  origin provenance on every world; overlap merges across world slugs
//	4  semantic conventions: attributes ride every payload
//	5  standard icons resolved for collections that declare one
//	6  geometry declared: spheres say so, pins carry true coordinates
//	7  attribute-level merge resolution; ledgers name canonical source slugs
//	8  shape prose defers to the text payload; shapes mark hasText
//	9  format v3: one collections array of point, path, and area features
const PolicyRevision = 9

// Options is one composition.
type Options struct {
	// Document is the source's reading of the volume.
	Document doc.Document
	// Tiles is the derived tile set the volume's lenses are drawn from.
	Tiles *tiles.Set
	// Curation is the editorial data the composition consults.
	Curation curation.Tables
	// BundleDir is the registry the finished bundle is installed into. Empty
	// composes and validates without writing anything, which is what a check
	// run wants.
	BundleDir string
	// Log is the run's event stream; nil is a logger that discards.
	Log *slog.Logger
}

// Result is what a composition produced.
type Result struct {
	Volume string
	Stamp  string
	// File is the name the build carries in a registry.
	File string
	// Path is where it was written, empty when nothing was written.
	Path string
	// Present reports that the build was already in the registry, so nothing
	// was written. It is the normal outcome of recomposing an unchanged
	// archive, and it is what makes a rebuild cheap.
	Present bool
	// Worlds, Tiles and Icons are what the bundle holds.
	Worlds, Tiles, Icons int
}

// composedWorld is one world under composition: the document's world, resolved
// against a tile set and curation, on its way to a payload.
type composedWorld struct {
	ID         int64
	Slug       string
	Title      string
	Parent     string
	IconOutset string
	Center     doc.Position
	CapturedAt string
	Grid       *worldGrid
	Lenses     []lens
	// Pyramids is the tile set entry behind each lens, in the same order.
	// A lens carries only what a payload says; a pyramid carries what a stamp
	// and a copy need.
	Pyramids    []tiles.Pyramid
	Collections []composedCollection
	Attrs       map[string]string
	Merged      []origin
}

// composedCollection is a document collection with what composition knows added
// to it: the artwork it resolved, and the numbers a wire needs.
type composedCollection struct {
	ID        int64
	Key       string
	Title     string
	Group     string
	Kind      string
	Icon      string
	Color     string
	IconColor string
	Visible   bool
	Attrs     map[string]string
	// IconAsset is the artwork's name inside the bundle, empty where the
	// document carried none.
	IconAsset string
	// IconPicture says the artwork is a picture drawn as-is rather than a glyph
	// the reader tints.
	IconPicture bool
	Features    []composedFeature
}

// composedFeature is a document feature with its place in a split world.
type composedFeature struct {
	doc.Feature
	// Shard is the layer of a split world this feature belongs to, zero when
	// the world is whole. It is composition's to decide: a source describes one
	// sheet, and whether that sheet is really several places is curation.
	Shard int64
}

// Compose builds a volume and, where a registry is named, installs it.
func Compose(o Options) (Result, error) {
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With(logging.Op("compose"), logging.Volume(o.Document.Volume.Slug))

	if err := o.Document.Validate(); err != nil {
		return Result{}, fmt.Errorf("document: %w", err)
	}
	if o.Tiles == nil {
		return Result{}, fmt.Errorf("volume %s: no tile set to draw from", o.Document.Volume.Slug)
	}
	grid := worldGrid{
		SourceZoom: o.Curation.Window.SourceZoom,
		FirstTile:  o.Curation.Window.FirstTile,
	}

	var worlds []composedWorld
	for _, source := range o.Document.Worlds {
		pieces, err := resolveWorld(source, o, grid, log)
		if err != nil {
			return Result{}, fmt.Errorf("world %s: %w", source.Slug, err)
		}
		worlds = append(worlds, pieces...)
	}
	if len(worlds) == 0 {
		return Result{}, fmt.Errorf("volume %s: no world is ready to compose", o.Document.Volume.Slug)
	}
	orderWorlds(o.Curation, o.Document.Volume.Slug, worlds)
	icons := attachArtwork(o.Document, worlds)
	if err := speakConventions(o.Curation, o.Document.Volume.Slug, worlds); err != nil {
		return Result{}, fmt.Errorf("volume %s: %w", o.Document.Volume.Slug, err)
	}
	return write(o, worlds, icons, log)
}

// resolveWorld turns one document world into the worlds under composition it
// becomes: its lenses found in the tile set, its collections numbered, a curated
// sheet taken apart, the ground each piece covers measured, and every piece's
// own account of where it came from opened.
//
// It answers with a slice because one sheet may hold several places. All but one
// volume in the corpus answer with exactly one world, and the one that does not
// is declared in curation rather than detected.
func resolveWorld(source doc.World, o Options, shared worldGrid, log *slog.Logger) ([]composedWorld, error) {
	out := composedWorld{
		ID:         source.ID,
		Slug:       source.Slug,
		Title:      source.Title,
		IconOutset: o.Curation.IconOutset(o.Document.Volume.Slug, source.Slug),
		Center:     source.Center,
		CapturedAt: source.Capture.CapturedAt,
		Attrs:      source.Attrs,
	}
	window := shared
	for index, declared := range source.Lenses {
		pyramid, ok := o.Tiles.Native(declared.TileSet)
		if !ok {
			return nil, fmt.Errorf("no pyramid is derived from tile set %s", declared.TileSet)
		}
		found := worldGrid{SourceZoom: pyramid.Window.SourceZoom, FirstTile: pyramid.Window.FirstTile}
		if found == (worldGrid{}) {
			// A tile set derived before pyramids carried their window: the
			// shared one is what everything in it was cut from.
			found = shared
		}
		// Every lens of a world is a picture of the same ground, so they agree
		// on the window it is cut from. One that did not would be a world drawn
		// in two places at once, and no feature could be placed on it.
		if index == 0 {
			window = found
			if found != shared {
				grid := found
				out.Grid = &grid
			}
		} else if found != window {
			return nil, fmt.Errorf(
				"tile set %s sits in a different window from %s",
				declared.TileSet, source.Lenses[0].TileSet)
		}
		out.Lenses = append(out.Lenses, lensOf(declared.Name, pyramid, o.Document.Volume.Slug))
		out.Pyramids = append(out.Pyramids, pyramid)
		// Another source's raster, resampled into this world, arrives as one
		// more way to see the same ground. It was rendered in this world's
		// window, so it passes the same agreement every native lens passes.
		for _, aligned := range o.Tiles.Aligned(declared.TileSet) {
			alignedWindow := worldGrid{
				SourceZoom: aligned.Window.SourceZoom,
				FirstTile:  aligned.Window.FirstTile,
			}
			if alignedWindow != window {
				return nil, fmt.Errorf(
					"aligned pyramid %s was rendered in a different window", aligned.Name)
			}
			out.Lenses = append(out.Lenses, lensOf(aligned.LensName, aligned, o.Document.Volume.Slug))
			out.Pyramids = append(out.Pyramids, aligned)
		}
	}

	collections, err := numberCollections(source)
	if err != nil {
		return nil, err
	}
	out.Collections = collections

	grid := surfaceGrid{
		SourceZoom: window.SourceZoom,
		FirstTile:  window.FirstTile,
		TileSize:   o.Tiles.TileSize,
		Size:       o.Tiles.Size,
	}
	// A sheet holding several separate places is taken apart here, where the
	// window it was cut from is finally known, and before the ground each piece
	// covers is measured: a piece's surface is its own, not the sheet's.
	mode := o.Curation.Shard(o.Document.Volume.Slug, source.Slug)
	pieces, err := splitWorld(out, mode, grid)
	if err != nil {
		return nil, fmt.Errorf("splitting into %s: %w", mode, err)
	}
	for index := range pieces {
		markSurfaces(&pieces[index], grid)
		// Every world opens its account with where it came from, split or not:
		// provenance is part of a world, not a side effect of composition.
		pieces[index].Merged = []origin{{
			Source:        o.Document.Source.Label,
			Slug:          o.Document.Source.Name,
			Origin:        true,
			DonorFeatures: tally(pieces[index].Collections),
		}}
		log.Debug("world composed", logging.World(pieces[index].Slug),
			"lenses", len(pieces[index].Lenses),
			"collections", len(pieces[index].Collections))
	}
	return pieces, nil
}

func lensOf(name string, p tiles.Pyramid, volume string) lens {
	return lens{
		Name:        name,
		Tiles:       tiles.LocalName(volume, p.Name),
		MinZoom:     p.MinZoom,
		MaxZoom:     p.MaxZoom,
		FullZoom:    p.FullZoom,
		SourceZoom:  p.SourceZoom,
		Formats:     p.Formats,
		Bounds:      p.Bounds,
		Interpolate: p.Interpolate,
		Background:  p.Background,
		Coverage:    p.Coverage,
	}
}

// numberCollections gives every collection a numeric identity on the wire.
// A source that has native numbers passes them through; one that declares only
// a key gets a number derived from that key and the world it sits in, because
// a reader's hide and unfold sets are keyed by number and a key is not one.
//
// The derivation is FNV-1a over "<world id>:collection:<key>", masked into the
// positive range of a signed 32-bit integer, with zero -- which the wire reads
// as absence -- moved to one. A collision with a number the source already
// claimed fails the build rather than quietly renaming somebody's collection.
func numberCollections(source doc.World) ([]composedCollection, error) {
	used := make(map[int64]string, len(source.Collections))
	for _, collection := range source.Collections {
		if collection.ID != 0 {
			used[collection.ID] = collection.Title
		}
	}
	out := make([]composedCollection, 0, len(source.Collections))
	for _, collection := range source.Collections {
		id := collection.ID
		if id == 0 {
			if collection.Key == "" {
				return nil, fmt.Errorf("collection %q carries neither an id nor a key", collection.Title)
			}
			claimed, err := claimID(source.ID, collection.Key, used)
			if err != nil {
				return nil, err
			}
			id = claimed
		}
		features := make([]composedFeature, 0, len(collection.Features))
		for _, feature := range collection.Features {
			features = append(features, composedFeature{Feature: feature})
		}
		out = append(out, composedCollection{
			ID:        id,
			Key:       collection.Key,
			Title:     collection.Title,
			Group:     collection.Group,
			Kind:      collection.Kind,
			Icon:      collection.Icon,
			Color:     collection.Color,
			IconColor: collection.IconColor,
			Visible:   collection.Visible,
			Attrs:     collection.Attrs,
			Features:  features,
		})
	}
	return out, nil
}

// orderWorlds puts a volume's primary world first and keeps a split sheet's
// pieces with the sheet they came from.
func orderWorlds(tables curation.Tables, volume string, worlds []composedWorld) {
	order := make(map[string]int)
	for index, slug := range tables.PreferredWorlds(volume) {
		order[slug] = index
	}
	// A version-history volume reads its date titles backward: the newest
	// capture opens, and the past waits one click below it.
	before := func(a, b string) bool { return a < b }
	if tables.NewestFirst(volume) {
		before = func(a, b string) bool { return a > b }
	}
	titles := make(map[string]string, len(worlds))
	for _, world := range worlds {
		titles[world.Slug] = world.Title
	}
	// Worlds sort as families: a piece carries its sheet's position and follows
	// it, so a split sheet stays together in the picker.
	family := func(w composedWorld) (string, string) {
		if w.Parent == "" {
			return w.Slug, w.Title
		}
		return w.Parent, titles[w.Parent]
	}
	sort.SliceStable(worlds, func(i, j int) bool {
		leftSlug, leftTitle := family(worlds[i])
		rightSlug, rightTitle := family(worlds[j])
		left, leftPreferred := order[leftSlug]
		right, rightPreferred := order[rightSlug]
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		if leftPreferred && left != right {
			return left < right
		}
		if leftTitle != rightTitle {
			return before(leftTitle, rightTitle)
		}
		if (worlds[i].Parent == "") != (worlds[j].Parent == "") {
			return worlds[i].Parent == ""
		}
		return before(worlds[i].Title, worlds[j].Title)
	})
}

// attachArtwork resolves every point collection's artwork against the
// document's icon set, and reports the assets the bundle will carry. Artwork
// nothing names is left behind: a source may hand over a whole archived icon
// set, and a bundle carries what is used.
func attachArtwork(d doc.Document, worlds []composedWorld) map[string][]byte {
	byKey := d.IconByKey()
	out := make(map[string][]byte)
	for worldIndex := range worlds {
		collections := worlds[worldIndex].Collections
		for index := range collections {
			collection := &collections[index]
			if collection.Kind != doc.KindPoint || collection.Icon == "" {
				continue
			}
			icon, held := byKey[collection.Icon]
			if !held {
				continue
			}
			out[icon.File] = icon.Data
			collection.IconAsset = icon.File
			collection.IconPicture = isPicture(icon.File)
		}
	}
	return out
}

// isPicture reads the one thing an asset's name decides: whether the artwork is
// a glyph a reader may tint, or a picture that must be drawn as it is.
func isPicture(file string) bool {
	return len(file) >= 4 && file[len(file)-4:] == ".png"
}
