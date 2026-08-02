// Package doc defines the Atlas interchange document: what one source has to
// say about one volume, in Atlas's own vocabulary.
//
// A document sits between the two halves of the generate lane. A source reads
// archived captures and emits one; composition reads one and writes a .atlas
// bundle. It is Atlas's schema, designed backwards from what composition needs
// -- worlds, lenses, collections, features, geometry, attributes, provenance --
// and no source's shape is privileged in it. A source that cannot say something
// in this vocabulary cannot say it at all, which is the point: nothing
// downstream of a translator knows which source it read.
//
// Three properties are load-bearing:
//
//   - Deterministic. The same captures translate to the same document, byte for
//     byte, on any machine. Maps are the only unordered thing in it, and JSON
//     sorts their keys; everything else is a slice whose order the source
//     decides and composition preserves.
//   - Replayable. A document is a pure function of archived bytes, so a policy
//     change re-runs over the archive instead of over the network. Every world
//     names the capture it was read from, by content hash.
//   - Self-contained. Icon artwork travels in the document rather than as a
//     path back into an archive, so composition never reaches for a file the
//     source did not hand it.
//
// The schema is documented for humans in docs/generate.md, which is this
// package's prose twin.
//
// # Coordinates
//
// Positions are latitude and longitude in the volume's own projection -- the
// same world space format/bundle.Coordinate carries and the same one a lens's
// tile pyramid is cut in. For a game map that space is synthetic: an image was
// encoded as a slippy map long ago and the numbers are pixels wearing degrees.
// For a planet or a city it is the real thing. Either way a source publishes
// what its ground means and composition does no reprojection, so a translator
// owns its own projection and nothing else has to know about it.
//
// # Identifiers
//
// Feature and collection identifiers are int64 here and signed 32-bit on the
// wire, where zero reads as absence. A source whose captures carry native
// numeric identities passes them through; a source without them derives stable
// ones from stable names and says so in Provenance.IDSpace. Zero means "no
// such thing" everywhere in this package: Feature.Member and Feature.Parent
// use it for "contained by nothing".
package doc

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Doc and Version identify the schema. Version moves when an existing field's
// meaning breaks; a new optional field rides on the old version, because a
// document is never stored -- it is produced and consumed in one run, and the
// version exists to catch a stale debugging dump, not to support archaeology.
const (
	Doc     = "atlas-generate-doc"
	Version = 1
)

// The geometry kinds a collection may declare. Every feature in a collection is
// the kind its collection declares; nothing sniffs geometry to find out.
const (
	KindPoint = "point"
	KindPath  = "path"
	KindArea  = "area"
)

// Document is one source's whole reading of one volume.
type Document struct {
	Doc     string     `json:"doc"`
	Version int        `json:"version"`
	Volume  Volume     `json:"volume"`
	Source  Provenance `json:"source"`
	Worlds  []World    `json:"worlds"`
	// Icons is the artwork the volume's collections name, by key. A key no
	// collection names is allowed: composition packs what is used and leaves
	// the rest, so a source may hand over a whole archived icon set without
	// deciding what is worth carrying.
	Icons []Icon `json:"icons,omitempty"`
}

// Volume is the subject: a game, a planet, a city.
type Volume struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Provenance is where a document came from, in the terms a ledger and a source
// card both need. It is the only place a source names itself; nothing else in
// the document, and nothing downstream of composition, mentions a source at
// all.
type Provenance struct {
	// Name is the source's registry name -- the slug a ledger line and a
	// workbench card agree on.
	Name string `json:"name"`
	// Label is the same source spelled for a person.
	Label string `json:"label"`
	// License and Attribution are what a volume owes the people whose work it
	// carries. They ride the source registry entry and surface on its card.
	License     string `json:"license,omitempty"`
	Attribution string `json:"attribution,omitempty"`
	// IDSpace says where the identifiers in this document came from, so a
	// reader of two documents knows whether their number spaces can be
	// compared. See IDSpaceNative and IDSpaceDerived.
	IDSpace string `json:"idSpace"`
}

// The id spaces a source may declare.
const (
	// IDSpaceNative means the captures carried their own numeric identities and
	// the document passes them through. Two documents from one native source
	// name the same thing by the same number.
	IDSpaceNative = "native"
	// IDSpaceDerived means the source minted identifiers from stable names.
	// They are stable across runs and meaningless across sources.
	IDSpaceDerived = "derived"
)

// Capture names the archived bytes a world was read from. It is provenance and
// a determinism receipt: two runs that name the same content hash read the same
// input, whatever the archive looked like around it.
type Capture struct {
	// Kind is the source's own name for what these bytes are. It is the one
	// place a source's vocabulary is allowed to show through, because it names
	// bytes the source captured and nothing else can describe them.
	Kind string `json:"kind"`
	// ID is the capture's identity in the source's own id space.
	ID int64 `json:"id,omitempty"`
	// Locator is where the source fetched the bytes. It is provenance only --
	// no payload ever carries it, and the offline invariant is checked at the
	// bundle, not here.
	Locator string `json:"locator,omitempty"`
	// ContentHash is the archive's content address of the bytes: SHA-256,
	// lowercase hex.
	ContentHash string `json:"contentHash"`
	// CapturedAt is when the archive first saw these bytes, RFC 3339. It is
	// what a build's creation time is derived from, never the build clock.
	CapturedAt string `json:"capturedAt"`
}

// World is one ground within the volume.
type World struct {
	ID    int64  `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// Center is where a reader opening this world is put.
	Center  Position `json:"center"`
	Capture Capture  `json:"capture"`
	// Lenses are the pictures of this ground, in the order a reader is offered
	// them. A lens here says only what a source knows: what it is called and
	// which tile set pictures it. Everything else about a pyramid -- its zoom
	// range, formats, coverage -- belongs to whatever derived it.
	Lenses []Lens `json:"lenses"`
	// Collections is the world's whole structure in one ordered array. The
	// order is the legend order and it is significant: the packed payload's
	// owner column indexes the point collections by position.
	Collections []Collection `json:"collections"`
	// Attrs is the world speaking the semantic conventions. Producer-strict:
	// every atlas.* key here is checked against the registry before a bundle
	// is written.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Position is a point in the volume's own projection.
type Position struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Lens is one picture of a world, as its source names it.
type Lens struct {
	// Name is what the reader's lens picker calls it.
	Name string `json:"name"`
	// TileSet is the path the source captured this picture's tiles under. It
	// is how composition finds the derived pyramid; it is a key into a tile
	// set, never a URL.
	TileSet string `json:"tileSet"`
}

// Collection is an ordered group of features of one kind.
type Collection struct {
	// ID is the collection's numeric identity on the wire. Zero asks
	// composition to derive one from Key.
	ID int64 `json:"id,omitempty"`
	// Key is the collection's curation and merge identity: the slug
	// collections from different sources meet under. A source whose captures
	// have no such concept leaves it empty and is met by its icon key instead.
	Key   string `json:"key,omitempty"`
	Title string `json:"title"`
	// Group is the legend section this collection files under, as free text.
	// It is a heading, not an entity: nothing hangs off it.
	Group string `json:"group,omitempty"`
	// Kind is KindPoint, KindPath or KindArea.
	Kind string `json:"kind"`
	// Icon names artwork in the document's icon set.
	Icon string `json:"icon,omitempty"`
	// Color and IconColor are the collection's resolved colours, "#RRGGBB"
	// upper case. A source resolves whatever inheritance its captures have;
	// composition inherits nothing.
	Color     string `json:"color,omitempty"`
	IconColor string `json:"iconColor,omitempty"`
	// Visible is whether the collection is drawn before a reader touches
	// anything.
	Visible bool              `json:"visible"`
	Attrs   map[string]string `json:"attrs,omitempty"`

	Features []Feature `json:"features,omitempty"`
}

// Feature is one thing on a world, whatever its dimensionality.
type Feature struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Description string `json:"description,omitempty"`
	// At is where a point feature stands. Nil for paths and areas.
	At *Position `json:"at,omitempty"`
	// Geometry is what a path or area draws, as GeoJSON parts. Coordinates are
	// carried opaquely: composition never rewrites them, so a source's numbers
	// reach the payload unrounded.
	Geometry []Geometry `json:"geometry,omitempty"`
	// Center is where a shape's label sits, where the source knows better than
	// the centroid.
	Center *Position `json:"center,omitempty"`
	// Member is the area feature whose ground this feature stands on; Parent is
	// the area feature this area sits inside. Zero is neither.
	Member int64 `json:"member,omitempty"`
	Parent int64 `json:"parent,omitempty"`
	// Links are cross-references to other features of the same world. A source
	// resolves whatever link syntax its captures use into these; nothing
	// outward-facing survives, because a bundle serves offline.
	Links []Link            `json:"links,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Geometry is one GeoJSON part of a shape feature.
type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// Link is a cross-reference to another feature of the same world.
type Link struct {
	Title   string `json:"title"`
	Feature int64  `json:"feature"`
}

// Icon is one piece of collection artwork, carried whole.
type Icon struct {
	// Key is what a collection names.
	Key string `json:"key"`
	// File is the asset's name inside a bundle's icons directory, including its
	// extension. The extension is load-bearing: it decides whether the artwork
	// is a glyph a reader tints or a picture it draws as-is.
	File string `json:"file"`
	Data []byte `json:"data"`
}

// Validate reports the first thing structurally wrong with a document. It is
// the shape check a source owes its consumer: composition may assume everything
// here held, and says so by refusing a document that fails.
//
// It deliberately says nothing about semantic conventions. Those are checked
// against the registry at composition, producer-strict, where a failure can
// name the bundle it would have been written into.
func (d Document) Validate() error {
	if d.Doc != Doc {
		return fmt.Errorf("document says %q, not %q", d.Doc, Doc)
	}
	if d.Version != Version {
		return fmt.Errorf("document version %d, want %d", d.Version, Version)
	}
	if d.Volume.Slug == "" {
		return fmt.Errorf("document names no volume")
	}
	if d.Source.Name == "" {
		return fmt.Errorf("document names no source")
	}
	switch d.Source.IDSpace {
	case IDSpaceNative, IDSpaceDerived:
	default:
		return fmt.Errorf("source %s declares id space %q, which is neither %q nor %q",
			d.Source.Name, d.Source.IDSpace, IDSpaceNative, IDSpaceDerived)
	}
	if len(d.Worlds) == 0 {
		return fmt.Errorf("volume %s carries no world", d.Volume.Slug)
	}
	icons := make(map[string]bool, len(d.Icons))
	for _, icon := range d.Icons {
		if icon.Key == "" || icon.File == "" {
			return fmt.Errorf("icon %q carries no key or no file name", icon.Key)
		}
		if icons[icon.Key] {
			return fmt.Errorf("icon %q is carried twice", icon.Key)
		}
		icons[icon.Key] = true
	}
	slugs := make(map[string]bool, len(d.Worlds))
	for _, world := range d.Worlds {
		if world.Slug == "" {
			return fmt.Errorf("world %q carries no slug", world.Title)
		}
		if slugs[world.Slug] {
			return fmt.Errorf("world %s appears twice", world.Slug)
		}
		slugs[world.Slug] = true
		if err := world.validate(); err != nil {
			return fmt.Errorf("world %s: %w", world.Slug, err)
		}
	}
	return nil
}

func (w World) validate() error {
	if w.Capture.ContentHash == "" {
		return fmt.Errorf("names no capture")
	}
	if w.Capture.CapturedAt == "" {
		return fmt.Errorf("capture %s carries no time", w.Capture.ContentHash)
	}
	if len(w.Lenses) == 0 {
		return fmt.Errorf("has no lens")
	}
	for _, lens := range w.Lenses {
		if lens.TileSet == "" {
			return fmt.Errorf("lens %q names no tile set", lens.Name)
		}
	}
	// One id space per world: the text payload is keyed by feature id across
	// every collection and kind, so two features sharing a number would lose
	// one of their descriptions.
	seen := make(map[int64]string)
	for _, collection := range w.Collections {
		switch collection.Kind {
		case KindPoint, KindPath, KindArea:
		default:
			return fmt.Errorf("collection %q declares kind %q", collection.Title, collection.Kind)
		}
		for _, feature := range collection.Features {
			if feature.ID == 0 {
				return fmt.Errorf("collection %q holds a feature with no id", collection.Title)
			}
			if held, taken := seen[feature.ID]; taken {
				return fmt.Errorf("features %q and %q share id %d", held, feature.Title, feature.ID)
			}
			seen[feature.ID] = feature.Title
			if collection.Kind == KindPoint && feature.At == nil {
				return fmt.Errorf("point feature %q stands nowhere", feature.Title)
			}
			if collection.Kind != KindPoint && len(feature.Geometry) == 0 {
				return fmt.Errorf("%s feature %q draws nothing", collection.Kind, feature.Title)
			}
		}
	}
	return nil
}

// NewestCapture is the newest capture time across a document's worlds: the time
// a build of it is versioned by. Times compare as strings, which is the order
// that means something for RFC 3339 and needs no parsing.
func (d Document) NewestCapture() string {
	newest := ""
	for _, world := range d.Worlds {
		if world.Capture.CapturedAt > newest {
			newest = world.Capture.CapturedAt
		}
	}
	return newest
}

// IconByKey indexes the document's artwork.
func (d Document) IconByKey() map[string]Icon {
	out := make(map[string]Icon, len(d.Icons))
	for _, icon := range d.Icons {
		out[icon.Key] = icon
	}
	return out
}

// Marshal encodes a document the one way it is ever written: indented, with a
// trailing newline, so a debugging dump is readable and two dumps diff line by
// line. Nothing stamps over these bytes -- a bundle's stamp is taken over the
// bundle -- so the encoding is chosen for people.
func (d Document) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}
	return append(data, '\n'), nil
}

// Unmarshal reads a document back and holds it to Validate.
func Unmarshal(data []byte) (Document, error) {
	var d Document
	if err := json.Unmarshal(data, &d); err != nil {
		return Document{}, fmt.Errorf("decode document: %w", err)
	}
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	return d, nil
}

// AttrKeys lists an attribute set's keys in sorted order. Producers walk
// attributes this way so a report about two documents reads the same twice.
func AttrKeys(attrs map[string]string) []string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
