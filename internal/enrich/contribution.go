package enrich

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// A contribution is what an enricher has to say, written down.
//
// The interface below hands one back as a Go value, but the value is a
// convenience over this: a contribution has a canonical serialized form, and
// that form is the contract. It can be written to a file, diffed between two
// runs, replayed against the same volume years later, or handed to a reviewer
// who wants to see what an enricher would do before it does it.
//
// The operation vocabulary is deliberately additive. There is no operation that
// removes a feature, empties a collection, or deletes a world, so "never remove
// or rewrite what a source said" is not a rule an enricher has to remember --
// it is a property of what an enricher is able to express. The two operations
// that touch existing data, setting an attribute and setting prose, refuse to
// overwrite; they only ever fill something empty.

// The canonical form's identity.
const (
	ContributionDoc     = "atlas-enrich-contribution"
	ContributionVersion = 1
)

// Contribution is one enricher's whole offering to one volume.
type Contribution struct {
	Contribution string `json:"contribution"`
	Version      int    `json:"version"`
	// Enricher is the name of whoever made it.
	Enricher string `json:"enricher"`
	Volume   string `json:"volume"`
	Ops      []Op   `json:"ops,omitempty"`
}

// The operations an enricher may express.
const (
	// OpSetAttr writes one semantic-conventions attribute onto a world, a
	// collection, or a feature. It fills an empty key; it never overwrites.
	OpSetAttr = "set-attr"
	// OpSetProse writes a feature's description, where the feature has none.
	OpSetProse = "set-prose"
	// OpAddFeature adds a feature to a collection the world already has.
	OpAddFeature = "add-feature"
	// OpAddCollection adds a whole collection, features and all.
	OpAddCollection = "add-collection"
	// OpAddWorld adds a whole ground the volume did not picture.
	OpAddWorld = "add-world"
	// OpSetIcon resolves a collection's artwork: the asset it carries and
	// whether that asset is a picture or a glyph.
	OpSetIcon = "set-icon"
	// OpAddAsset carries one piece of artwork into the volume.
	OpAddAsset = "add-asset"
	// OpSetLens attaches a picture of a world, or updates one already attached.
	OpSetLens = "set-lens"
	// OpLedger writes one account of the contribution onto a world.
	OpLedger = "ledger"
)

// Op is one operation. It is one struct rather than an interface because the
// canonical form is the contract: a flat, ordered list of records with a named
// kind is what serializes, diffs and replays predictably, and what a reviewer
// can read.
type Op struct {
	Kind  string `json:"op"`
	World string `json:"world,omitempty"`
	// Collection and Feature name what the operation is about, by identity.
	Collection int64 `json:"collection,omitempty"`
	Feature    int64 `json:"feature,omitempty"`
	// Entity says which entity an attribute attaches to, in the conventions'
	// own words.
	Entity semconv.Entity `json:"entity,omitempty"`
	Key    string         `json:"key,omitempty"`
	Value  string         `json:"value,omitempty"`
	// Picture says an icon asset is drawn as-is rather than tinted.
	Picture bool `json:"picture,omitempty"`

	NewCollection *Collection `json:"newCollection,omitempty"`
	NewFeature    *Feature    `json:"newFeature,omitempty"`
	NewWorld      *World      `json:"newWorld,omitempty"`
	Lens          *Lens       `json:"lens,omitempty"`
	Asset         *Icon       `json:"asset,omitempty"`
	Account       *Account    `json:"account,omitempty"`
}

// Empty reports a contribution that changes nothing. No change is no build:
// a volume nothing has to say about is left exactly where it was.
func (c Contribution) Empty() bool { return len(c.Ops) == 0 }

// Marshal writes the canonical form: indented, with a trailing newline, keys in
// declaration order, and maps sorted by the encoder. Two runs of the same
// enricher over the same volume write the same bytes.
func (c Contribution) Marshal() ([]byte, error) {
	if c.Contribution == "" {
		c.Contribution = ContributionDoc
	}
	if c.Version == 0 {
		c.Version = ContributionVersion
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	// Escaping HTML would rewrite an ampersand in somebody's title, and a
	// canonical form that alters what it carries is not one.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(c); err != nil {
		return nil, fmt.Errorf("marshal contribution: %w", err)
	}
	return out.Bytes(), nil
}

// Digest is the canonical form's SHA-256, lowercase hex: what a log line names
// a contribution by, and what says two runs produced the same offering.
func (c Contribution) Digest() (string, error) {
	data, err := c.Marshal()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// UnmarshalContribution reads the canonical form back.
func UnmarshalContribution(data []byte) (Contribution, error) {
	var c Contribution
	if err := json.Unmarshal(data, &c); err != nil {
		return Contribution{}, fmt.Errorf("decode contribution: %w", err)
	}
	if c.Contribution != ContributionDoc {
		return Contribution{}, fmt.Errorf("contribution says %q, not %q", c.Contribution, ContributionDoc)
	}
	if c.Version != ContributionVersion {
		return Contribution{}, fmt.Errorf("contribution version %d, want %d", c.Version, ContributionVersion)
	}
	return c, nil
}

// Apply folds a contribution into a volume, in the order its operations were
// written. Every operation is checked before it lands: an attribute against the
// conventions registry and against what is already there, an identifier against
// the world's one identifier space, artwork against the bytes already carried
// under its name.
//
// A refused operation fails the whole application. A contribution is one
// enricher's account of one volume, and half of an account is not an account.
func Apply(v *Volume, c Contribution) error {
	if c.Volume != "" && c.Volume != v.Slug {
		return fmt.Errorf("contribution from %s is about volume %s, not %s", c.Enricher, c.Volume, v.Slug)
	}
	for index, op := range c.Ops {
		if err := applyOp(v, op); err != nil {
			return fmt.Errorf("%s op %d (%s): %w", c.Enricher, index, op.Kind, err)
		}
	}
	return nil
}

func applyOp(v *Volume, op Op) error {
	switch op.Kind {
	case OpAddWorld:
		return addWorld(v, op)
	case OpAddAsset:
		return addAsset(v, op)
	}
	world := v.World(op.World)
	if world == nil {
		return fmt.Errorf("volume %s pictures no world %s", v.Slug, op.World)
	}
	switch op.Kind {
	case OpSetAttr:
		return setAttr(world, op)
	case OpSetProse:
		return setProse(world, op)
	case OpAddFeature:
		return addFeature(world, op)
	case OpAddCollection:
		return addCollection(world, op)
	case OpSetIcon:
		return setIcon(world, op)
	case OpSetLens:
		return setLens(world, op)
	case OpLedger:
		if op.Account == nil {
			return fmt.Errorf("no account")
		}
		world.Ledger = append(world.Ledger, *op.Account)
		return nil
	}
	return fmt.Errorf("no such operation")
}

func setAttr(w *World, op Op) error {
	if err := semconv.Check(op.Entity, op.Key, op.Value); err != nil {
		return err
	}
	switch op.Entity {
	case semconv.EntityWorld:
		if held, taken := w.Attrs[op.Key]; taken && held != op.Value {
			return fmt.Errorf("world %s already says %s=%q", w.Slug, op.Key, held)
		}
		w.Attrs = withAttr(w.Attrs, op.Key, op.Value)
		return nil
	case semconv.EntityCollection:
		collection := w.Collection(op.Collection)
		if collection == nil {
			return fmt.Errorf("world %s has no collection %d", w.Slug, op.Collection)
		}
		if held, taken := collection.Attrs[op.Key]; taken && held != op.Value {
			return fmt.Errorf("collection %q already says %s=%q", collection.Title, op.Key, held)
		}
		collection.Attrs = withAttr(collection.Attrs, op.Key, op.Value)
		return nil
	case semconv.EntityFeature:
		_, feature := w.Feature(op.Feature)
		if feature == nil {
			return fmt.Errorf("world %s has no feature %d", w.Slug, op.Feature)
		}
		if held, taken := feature.Attrs[op.Key]; taken && held != op.Value {
			return fmt.Errorf("feature %q already says %s=%q", feature.Title, op.Key, held)
		}
		feature.Attrs = withAttr(feature.Attrs, op.Key, op.Value)
		return nil
	}
	return fmt.Errorf("attributes attach to a world, a collection or a feature, not to %q", op.Entity)
}

func setProse(w *World, op Op) error {
	_, feature := w.Feature(op.Feature)
	if feature == nil {
		return fmt.Errorf("world %s has no feature %d", w.Slug, op.Feature)
	}
	if feature.Description != "" && feature.Description != op.Value {
		return fmt.Errorf("feature %q already has prose; an enricher fills what is empty and rewrites nothing",
			feature.Title)
	}
	feature.Description = op.Value
	return nil
}

func addFeature(w *World, op Op) error {
	if op.NewFeature == nil {
		return fmt.Errorf("no feature")
	}
	collection := w.Collection(op.Collection)
	if collection == nil {
		return fmt.Errorf("world %s has no collection %d", w.Slug, op.Collection)
	}
	if _, held := w.Feature(op.NewFeature.ID); held != nil {
		return fmt.Errorf("feature id %d is already spoken for in %s", op.NewFeature.ID, w.Slug)
	}
	collection.Features = append(collection.Features, *op.NewFeature)
	return nil
}

func addCollection(w *World, op Op) error {
	if op.NewCollection == nil {
		return fmt.Errorf("no collection")
	}
	if w.Collection(op.NewCollection.ID) != nil {
		return fmt.Errorf("collection id %d is already spoken for in %s", op.NewCollection.ID, w.Slug)
	}
	for _, feature := range op.NewCollection.Features {
		if _, held := w.Feature(feature.ID); held != nil {
			return fmt.Errorf("feature id %d is already spoken for in %s", feature.ID, w.Slug)
		}
	}
	w.Collections = append(w.Collections, *op.NewCollection)
	return nil
}

func addWorld(v *Volume, op Op) error {
	if op.NewWorld == nil {
		return fmt.Errorf("no world")
	}
	if v.World(op.NewWorld.Slug) != nil {
		return fmt.Errorf("volume %s already pictures %s", v.Slug, op.NewWorld.Slug)
	}
	v.Worlds = append(v.Worlds, *op.NewWorld)
	return nil
}

// setIcon gives a collection artwork: the key it names in the volume's icon
// set, and, where the caller is working on a volume whose artwork has already
// been resolved to assets, the asset name too.
func setIcon(w *World, op Op) error {
	collection := w.Collection(op.Collection)
	if collection == nil {
		return fmt.Errorf("world %s has no collection %d", w.Slug, op.Collection)
	}
	if collection.Icon != "" && collection.Icon != op.Key {
		return fmt.Errorf("collection %q already names artwork %s", collection.Title, collection.Icon)
	}
	if collection.IconAsset != "" && collection.IconAsset != op.Value {
		return fmt.Errorf("collection %q already carries %s", collection.Title, collection.IconAsset)
	}
	if op.Key != "" {
		collection.Icon = op.Key
	}
	if op.Value != "" {
		collection.IconAsset = op.Value
		collection.IconPicture = op.Picture
	}
	return nil
}

func setLens(w *World, op Op) error {
	if op.Lens == nil {
		return fmt.Errorf("no lens")
	}
	for index := range w.Lenses {
		if w.Lenses[index].Name != op.Lens.Name {
			continue
		}
		// A lens of the same name pictures the same ground through the same
		// pyramid: updating it is re-derivation, and a new stamp is the point.
		// Pointing it at another tile set would be a rewrite of what the source
		// said its picture was.
		if w.Lenses[index].TileSet != op.Lens.TileSet {
			return fmt.Errorf("lens %q already pictures %s; an enricher does not repoint a source's lens",
				op.Lens.Name, w.Lenses[index].TileSet)
		}
		w.Lenses[index] = *op.Lens
		return nil
	}
	w.Lenses = append(w.Lenses, *op.Lens)
	return nil
}

func addAsset(v *Volume, op Op) error {
	if op.Asset == nil || op.Asset.File == "" {
		return fmt.Errorf("no asset")
	}
	for _, held := range v.Icons {
		if held.File != op.Asset.File {
			continue
		}
		if !bytes.Equal(held.Data, op.Asset.Data) {
			return fmt.Errorf("asset %s is already carried with different bytes", op.Asset.File)
		}
		return nil
	}
	v.Icons = append(v.Icons, *op.Asset)
	return nil
}

// withAttr sets one attribute on a copy of the map, never on the map itself:
// two worlds may share their source's attributes by reference, and speaking for
// one must not put words in another's mouth.
func withAttr(attrs map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(attrs)+1)
	for held, value := range attrs {
		out[held] = value
	}
	out[key] = value
	return out
}
