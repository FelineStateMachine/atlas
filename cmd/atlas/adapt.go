package main

import (
	"encoding/json"
	"fmt"

	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The seam between the two pipeline lanes.
//
// The generate lane and the enrich lane never import each other (issue #5
// §3.2): what a source read is an interchange document, what an enricher reads
// is a volume under enrichment, and the two models are owned by the two lanes.
// This binary is the one place both are in scope, so this file is the whole of
// the ⊕ in "the composed multi-source result is generate ⊕ enrich".
//
// Both directions are mechanical copies. That is the point: if the adaptation
// had judgement in it, the judgement would be lane logic hiding in a command.
// TestAdaptationRoundTrips holds the pair to identity -- a document adapted and
// adapted back is the same document, byte for byte -- which is also the
// mechanical form of the lane's no-change-no-build law: an empty queue cannot
// produce a different build.

// volumeOf adapts one source's reading into a volume the enrich lane can work
// on. The grid is what the composition of this document would cut its worlds
// from, which the enrich lane needs because distances are measured in world
// pixels and a document speaks degrees.
func volumeOf(d doc.Document, grid enrich.Grid) *enrich.Volume {
	out := &enrich.Volume{
		Slug:  d.Volume.Slug,
		Title: d.Volume.Title,
		Source: enrich.Source{
			Name:  d.Source.Name,
			Label: d.Source.Label,
		},
	}
	for _, icon := range d.Icons {
		out.Icons = append(out.Icons, enrich.Icon{Key: icon.Key, File: icon.File, Data: icon.Data})
	}
	for _, world := range d.Worlds {
		adapted := enrich.World{
			Slug:  world.Slug,
			Title: world.Title,
			ID:    world.ID,
			Center: enrich.Position{
				Lat: world.Center.Lat,
				Lng: world.Center.Lng,
			},
			Capture: enrich.Capture{
				Kind:        world.Capture.Kind,
				ID:          world.Capture.ID,
				Locator:     world.Capture.Locator,
				ContentHash: world.Capture.ContentHash,
			},
			CapturedAt: world.Capture.CapturedAt,
			Grid:       grid,
			Attrs:      world.Attrs,
		}
		for _, lens := range world.Lenses {
			adapted.Lenses = append(adapted.Lenses, enrich.Lens{Name: lens.Name, TileSet: lens.TileSet})
		}
		for _, collection := range world.Collections {
			adaptedCollection := enrich.Collection{
				ID:        collection.ID,
				Key:       collection.Key,
				Title:     collection.Title,
				Group:     collection.Group,
				Kind:      collection.Kind,
				Icon:      collection.Icon,
				Color:     collection.Color,
				IconColor: collection.IconColor,
				Visible:   collection.Visible,
				Attrs:     collection.Attrs,
			}
			for _, feature := range collection.Features {
				adaptedCollection.Features = append(adaptedCollection.Features, enrich.Feature{
					ID:          feature.ID,
					Title:       feature.Title,
					Subtitle:    feature.Subtitle,
					Description: feature.Description,
					At:          position(feature.At),
					Center:      position(feature.Center),
					Geometry:    geometry(feature.Geometry),
					Member:      feature.Member,
					Parent:      feature.Parent,
					Links:       links(feature.Links),
					Attrs:       feature.Attrs,
				})
			}
			adapted.Collections = append(adapted.Collections, adaptedCollection)
		}
		out.Worlds = append(out.Worlds, adapted)
	}
	return out
}

// documentOf adapts an enriched volume back into an interchange document, so
// composition -- which knows only documents -- can build the volume the enrich
// lane made.
//
// The ledger does not travel this way. It is provenance about the composition
// rather than something a source said, and it reaches the payload through
// composition's own ledger option, which is what keeps a document the thing one
// source has to say.
func documentOf(v *enrich.Volume, source doc.Provenance) doc.Document {
	out := doc.Document{
		Doc:     doc.Doc,
		Version: doc.Version,
		Volume:  doc.Volume{Slug: v.Slug, Title: v.Title},
		Source:  source,
	}
	for _, icon := range v.Icons {
		out.Icons = append(out.Icons, doc.Icon{Key: icon.Key, File: icon.File, Data: icon.Data})
	}
	for _, world := range v.Worlds {
		adapted := doc.World{
			ID:     world.ID,
			Slug:   world.Slug,
			Title:  world.Title,
			Center: doc.Position{Lat: world.Center.Lat, Lng: world.Center.Lng},
			Capture: doc.Capture{
				Kind:        world.Capture.Kind,
				ID:          world.Capture.ID,
				Locator:     world.Capture.Locator,
				ContentHash: world.Capture.ContentHash,
				CapturedAt:  world.CapturedAt,
			},
			Attrs: world.Attrs,
		}
		for _, lens := range world.Lenses {
			adapted.Lenses = append(adapted.Lenses, doc.Lens{Name: lens.Name, TileSet: lens.TileSet})
		}
		for _, collection := range world.Collections {
			adaptedCollection := doc.Collection{
				ID:        collection.ID,
				Key:       collection.Key,
				Title:     collection.Title,
				Group:     collection.Group,
				Kind:      collection.Kind,
				Icon:      collection.Icon,
				Color:     collection.Color,
				IconColor: collection.IconColor,
				Visible:   collection.Visible,
				Attrs:     collection.Attrs,
			}
			for _, feature := range collection.Features {
				adaptedCollection.Features = append(adaptedCollection.Features, doc.Feature{
					ID:          feature.ID,
					Title:       feature.Title,
					Subtitle:    feature.Subtitle,
					Description: feature.Description,
					At:          docPosition(feature.At),
					Center:      docPosition(feature.Center),
					Geometry:    docGeometry(feature.Geometry),
					Member:      feature.Member,
					Parent:      feature.Parent,
					Links:       docLinks(feature.Links),
					Attrs:       feature.Attrs,
				})
			}
			adapted.Collections = append(adapted.Collections, adaptedCollection)
		}
		out.Worlds = append(out.Worlds, adapted)
	}
	return out
}

// ledgerOf serializes an enriched volume's accounts, by world, in the form
// composition splices into a payload.
func ledgerOf(v *enrich.Volume) (map[string][]json.RawMessage, error) {
	out := make(map[string][]json.RawMessage, len(v.Worlds))
	for _, world := range v.Worlds {
		if len(world.Ledger) == 0 {
			continue
		}
		accounts := make([]json.RawMessage, 0, len(world.Ledger))
		for _, account := range world.Ledger {
			data, err := json.Marshal(account)
			if err != nil {
				return nil, fmt.Errorf("world %s: marshal ledger: %w", world.Slug, err)
			}
			accounts = append(accounts, data)
		}
		out[world.Slug] = accounts
	}
	return out, nil
}

func position(p *doc.Position) *enrich.Position {
	if p == nil {
		return nil
	}
	return &enrich.Position{Lat: p.Lat, Lng: p.Lng}
}

func docPosition(p *enrich.Position) *doc.Position {
	if p == nil {
		return nil
	}
	return &doc.Position{Lat: p.Lat, Lng: p.Lng}
}

func geometry(in []doc.Geometry) []enrich.Geometry {
	if len(in) == 0 {
		return nil
	}
	out := make([]enrich.Geometry, 0, len(in))
	for _, part := range in {
		out = append(out, enrich.Geometry{Type: part.Type, Coordinates: part.Coordinates})
	}
	return out
}

func docGeometry(in []enrich.Geometry) []doc.Geometry {
	if len(in) == 0 {
		return nil
	}
	out := make([]doc.Geometry, 0, len(in))
	for _, part := range in {
		out = append(out, doc.Geometry{Type: part.Type, Coordinates: part.Coordinates})
	}
	return out
}

func links(in []doc.Link) []enrich.Link {
	if len(in) == 0 {
		return nil
	}
	out := make([]enrich.Link, 0, len(in))
	for _, link := range in {
		out = append(out, enrich.Link{Title: link.Title, Feature: link.Feature})
	}
	return out
}

func docLinks(in []enrich.Link) []doc.Link {
	if len(in) == 0 {
		return nil
	}
	out := make([]doc.Link, 0, len(in))
	for _, link := range in {
		out = append(out, doc.Link{Title: link.Title, Feature: link.Feature})
	}
	return out
}
