package compose

import (
	"encoding/json"
	"strconv"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// The wire shape of a world payload. Its field names, its field order, and
// which fields are omitted when empty all feed the bundle's stamp, so this is
// frozen with the format version: a change here restamps every bundle in every
// library. docs/format.md is normative for what these mean; this file is where
// composition writes them.
//
// A world travels in three parts because they are wanted at three different
// moments. The payload proper is read when a world opens; the packed locations
// are read with it and hold the point features, which are overwhelmingly
// numbers and cost several times their weight written as text; the prose is
// read one feature at a time, when a card opens, and is half a world's bulk.

// worldPayload is everything needed to draw a world except its point features,
// which travel packed alongside.
type worldPayload struct {
	// Grid travels only with a world cut from a window of its own; every other
	// world sits in the volume's shared window and says nothing.
	Grid        *worldGrid       `json:"grid,omitempty"`
	Lenses      []lens           `json:"lenses"`
	Collections []wireCollection `json:"collections"`
	// Attrs is the world speaking the shared conventions.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Merged is the world's provenance: one account per contributor, the
	// world's own origin account first.
	//
	// A single-source build carries exactly one entry, which composition writes
	// itself from the origin struct below. Cross-source composition is the
	// enrich lane's work and its ledger vocabulary is the enrich lane's to
	// define, so the accounts travel here already serialized: composition
	// splices in what it was handed, verbatim, and never learns what a matched
	// pair or a held feature is. That is what keeps the origin account's bytes
	// exactly where they were while the ledger beside it grows.
	Merged []json.RawMessage `json:"merged,omitempty"`
}

type worldGrid struct {
	SourceZoom int `json:"sourceZoom"`
	FirstTile  int `json:"firstTile"`
}

// lens is one picture of a world: which pyramid draws it, over what zooms, and
// which of its tiles exist.
type lens struct {
	Name       string     `json:"name"`
	Tiles      string     `json:"tiles"`
	MinZoom    int        `json:"minZoom"`
	MaxZoom    int        `json:"maxZoom"`
	FullZoom   int        `json:"fullZoom"`
	SourceZoom int        `json:"sourceZoom"`
	Formats    []string   `json:"formats"`
	Bounds     *tiles.Box `json:"bounds,omitempty"`
	// Surface is the ground the world covers, where Bounds is the window cut
	// from the pyramid to draw it. On a piece of a split sheet the window is
	// grown to take in the title printed beside it, so anything dividing the
	// world up measures the surface instead and leaves no cell on blank margin.
	Surface     *tiles.Box                 `json:"surface,omitempty"`
	Interpolate bool                       `json:"interpolate"`
	Background  string                     `json:"background,omitempty"`
	Shard       int64                      `json:"shard,omitempty"`
	Coverage    map[string]*tiles.Coverage `json:"coverage,omitempty"`
}

// wireCollection is one collection as the payload lists it. A point collection
// carries no inline features -- its members ride the packed payload, owned by
// this collection's index -- where a path or area collection inlines its
// features whole.
type wireCollection struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Group       string            `json:"group,omitempty"`
	Kind        string            `json:"kind"`
	Icon        string            `json:"icon,omitempty"`
	IconAsset   string            `json:"iconAsset,omitempty"`
	IconPicture bool              `json:"iconPicture,omitempty"`
	Color       string            `json:"color,omitempty"`
	IconColor   string            `json:"iconColor,omitempty"`
	Visible     bool              `json:"visible"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Features    []wireFeature     `json:"features,omitempty"`
}

// wireFeature is one shape feature as the payload inlines it. Its prose defers
// into the text payload behind the hasText marker, exactly the way a point
// feature's does; its attributes stay inline, because a card needs them the
// moment ground is asked about.
type wireFeature struct {
	ID       int64             `json:"id"`
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle,omitempty"`
	HasText  bool              `json:"hasText,omitempty"`
	Parent   *int64            `json:"parent,omitempty"`
	Center   *coordinate       `json:"center,omitempty"`
	Shard    int64             `json:"shard,omitempty"`
	Geometry []doc.Geometry    `json:"geometry"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

type coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// featureText holds what only a selected feature needs.
type featureText struct {
	Description string `json:"d,omitempty"`
	Links       []link `json:"l,omitempty"`
	// Attrs carries a point feature's own conventions -- its true planetary
	// coordinates, where a source published them -- read when its card opens,
	// like everything else here.
	Attrs map[string]string `json:"a,omitempty"`
}

// link is a cross-reference to another feature of the same world.
type link struct {
	Title      string `json:"title"`
	LocationID int64  `json:"locationId"`
}

// origin is a world's account of where it came from. Every world opens one,
// merged with anything or not: provenance is part of a world, not a side effect
// of composition. It carries both spellings of the source -- the label a person
// reads and the slug a registry names it by -- so a ledger line and a source
// card point at each other without a translation table.
type origin struct {
	Source string `json:"source"`
	Slug   string `json:"slug,omitempty"`
	// Origin marks the account of the source the world itself came from, as
	// against a donor folded in later.
	Origin bool `json:"origin,omitempty"`
	// DonorFeatures on an origin account is simply the world's own tally at
	// composition.
	DonorFeatures counts `json:"donorFeatures"`
	Added         int    `json:"added"`
}

// counts is features by kind, as a ledger speaks of them.
type counts struct {
	Point int `json:"point"`
	Path  int `json:"path"`
	Area  int `json:"area"`
}

// tally counts a world's features by kind: the manifest's per-kind counts, the
// origin account's view of what it holds, and the yardstick a later merge audit
// measures the composed world against.
func tally(collections []composedCollection) counts {
	var out counts
	for _, collection := range collections {
		switch collection.Kind {
		case doc.KindPoint:
			out.Point += len(collection.Features)
		case doc.KindPath:
			out.Path += len(collection.Features)
		default:
			out.Area += len(collection.Features)
		}
	}
	return out
}

// buildPayload splits one world three ways: its lenses and collections as a
// payload, its point features packed, and every kind's prose keyed by feature
// id. Feature identifiers share one space per world, so one world never keys
// two things alike.
//
// Point collections are written first and shape collections after, so a packed
// location's owner is simply the ordinal of its collection among the points --
// and the wire order is the legend order: collections as the source ordered
// them, points before shapes.
func buildPayload(w composedWorld) (worldPayload, []byte, map[string]featureText) {
	payload := worldPayload{
		Grid: w.Grid,
		// The collections array is always present, even empty: a raster-only
		// world holds none, and a reader iterating the legend should meet a
		// list rather than a null.
		Collections: []wireCollection{},
		Lenses:      w.Lenses,
		Attrs:       w.Attrs,
		Merged:      w.Merged,
	}
	var locations []bundle.Location
	text := make(map[string]featureText)

	for _, collection := range w.Collections {
		if collection.Kind != doc.KindPoint {
			continue
		}
		owner := uint16(len(payload.Collections))
		payload.Collections = append(payload.Collections, listed(collection))
		for _, pin := range collection.Features {
			var lat, lng float64
			if pin.At != nil {
				lat, lng = pin.At.Lat, pin.At.Lng
			}
			locations = append(locations, bundle.Location{
				ID:     pin.ID,
				Title:  pin.Title,
				Lat:    lat,
				Lng:    lng,
				Member: pin.Member,
				Shard:  pin.Shard,
				Owner:  owner,
			})
			if pin.Description != "" || len(pin.Links) > 0 || len(pin.Attrs) > 0 {
				text[strconv.FormatInt(pin.ID, 10)] = featureText{
					Description: pin.Description,
					Links:       links(pin.Links),
					Attrs:       pin.Attrs,
				}
			}
		}
	}
	for _, collection := range w.Collections {
		if collection.Kind == doc.KindPoint {
			continue
		}
		entry := listed(collection)
		for _, shape := range collection.Features {
			feature := wireFeature{
				ID:       shape.ID,
				Title:    shape.Title,
				Subtitle: shape.Subtitle,
				Parent:   optionalID(shape.Parent),
				Center:   position(shape.Center),
				Shard:    shape.Shard,
				Geometry: shape.Geometry,
				Attrs:    shape.Attrs,
			}
			if shape.Description != "" {
				text[strconv.FormatInt(shape.ID, 10)] = featureText{Description: shape.Description}
				feature.HasText = true
			}
			entry.Features = append(entry.Features, feature)
		}
		payload.Collections = append(payload.Collections, entry)
	}
	return payload, bundle.PackLocations(locations), text
}

func listed(collection composedCollection) wireCollection {
	return wireCollection{
		ID:          collection.ID,
		Title:       collection.Title,
		Group:       collection.Group,
		Kind:        collection.Kind,
		Icon:        collection.Icon,
		IconAsset:   collection.IconAsset,
		IconPicture: collection.IconPicture,
		Color:       collection.Color,
		IconColor:   collection.IconColor,
		Visible:     collection.Visible,
		Attrs:       collection.Attrs,
	}
}

func links(in []doc.Link) []link {
	if len(in) == 0 {
		return nil
	}
	out := make([]link, 0, len(in))
	for _, l := range in {
		out = append(out, link{Title: l.Title, LocationID: l.Feature})
	}
	return out
}

// optionalID turns the document's "zero is nothing" into the wire's "absent is
// nothing". The two spellings mean the same, and the wire's is what a reader
// has always seen.
func optionalID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	value := id
	return &value
}

func position(p *doc.Position) *coordinate {
	if p == nil {
		return nil
	}
	return &coordinate{Lat: p.Lat, Lng: p.Lng}
}
