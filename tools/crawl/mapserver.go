// The national fetch path. Hub datasets arrive through a download API that
// stages a whole file server-side; a national layer on a plain ArcGIS
// MapServer offers no such staging, so it is paged through the query
// endpoint instead -- the city's padded window as the envelope, true degrees
// asked for by name, pages ordered by the layer's own row identity until
// one comes back short. Fetched geometry is cut to the window at capture
// time: a watershed intersecting the window may extend a hundred windows
// past it, and the trimmed shape is the honest city-sized answer.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/arcgismap"
)

// mapServerPage is the most rows one query asks for, matching the services'
// own maxRecordCount.
const mapServerPage = 2000

// fetchMapServerDataset takes one national layer through the query endpoint,
// page by page, and answers features already cut to the window.
func fetchMapServerDataset(
	ctx context.Context,
	fetcher *fetcher,
	window arcgismap.Window,
	dataset arcgismap.Dataset,
) ([]arcgismap.Feature, error) {
	var features []arcgismap.Feature
	claimed := map[int64]bool{}
	for offset := 0; ; offset += mapServerPage {
		address := mapServerQueryURL(dataset, window, offset)
		body, _, err := fetcher.get(ctx, address)
		if err != nil {
			return nil, err
		}
		page, more, err := decodeMapServerPage(dataset, window, body, address)
		if err != nil {
			return nil, err
		}
		for _, feature := range page {
			// A row captured twice means the pages shifted under the
			// crawl; a capture must not hold it two ways, and Normalize's
			// sort would bury the evidence.
			if claimed[feature.ID] {
				return nil, fmt.Errorf("feature %d arrived twice; pages shifted mid-crawl: %s",
					feature.ID, address)
			}
			claimed[feature.ID] = true
		}
		features = append(features, page...)
		if !more {
			return features, nil
		}
	}
}

// mapServerQueryURL spells one page's request: the window as the envelope,
// degrees in and degrees out, fields limited to the curation, rows ordered
// by the layer's row identity so the pages tile the layer exactly once.
func mapServerQueryURL(dataset arcgismap.Dataset, window arcgismap.Window, offset int) string {
	where := dataset.Where
	if where == "" {
		where = "1=1"
	}
	envelope := strings.Join([]string{
		strconv.FormatFloat(window.West, 'f', -1, 64),
		strconv.FormatFloat(window.South, 'f', -1, 64),
		strconv.FormatFloat(window.East, 'f', -1, 64),
		strconv.FormatFloat(window.North, 'f', -1, 64),
	}, ",")
	values := url.Values{
		"f":                 {"geojson"},
		"where":             {where},
		"geometry":          {envelope},
		"geometryType":      {"esriGeometryEnvelope"},
		"inSR":              {"4326"},
		"spatialRel":        {"esriSpatialRelIntersects"},
		"outSR":             {"4326"},
		"outFields":         {strings.Join(dataset.Keep, ",")},
		"geometryPrecision": {"7"},
		"orderByFields":     {dataset.Keep[0]},
		"resultOffset":      {strconv.Itoa(offset)},
		"resultRecordCount": {strconv.Itoa(mapServerPage)},
	}
	return fmt.Sprintf("%s/%d/query?%s", dataset.Server, dataset.Layer, values.Encode())
}

// decodeMapServerPage reads one page into capture features, cut to the
// window, and says whether another page waits. ArcGIS answers errors as
// HTTP 200 with an error member, and spells the truncation flag at the
// collection's top level or under its properties depending on the vintage.
func decodeMapServerPage(
	dataset arcgismap.Dataset,
	window arcgismap.Window,
	body []byte,
	address string,
) ([]arcgismap.Feature, bool, error) {
	var page struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Type                  string `json:"type"`
		ExceededTransferLimit bool   `json:"exceededTransferLimit"`
		Properties            struct {
			ExceededTransferLimit bool `json:"exceededTransferLimit"`
		} `json:"properties"`
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, false, fmt.Errorf("decode %s: %w", address, err)
	}
	if page.Error != nil {
		return nil, false, fmt.Errorf("%s answered error %d: %s",
			address, page.Error.Code, page.Error.Message)
	}
	if page.Type != "FeatureCollection" {
		return nil, false, fmt.Errorf("%s is a %q, not a FeatureCollection", address, page.Type)
	}

	features := make([]arcgismap.Feature, 0, len(page.Features))
	for at, raw := range page.Features {
		fields := arcgismap.Fields{}
		for _, keep := range dataset.Keep {
			if value := arcgismap.FieldString(raw.Properties[keep]); value != "" {
				fields[keep] = value
			}
		}
		id, err := mapServerFeatureID(dataset, fields, raw.Properties)
		if err != nil {
			return nil, false, fmt.Errorf("feature %d of %s: %w", at, address, err)
		}
		geometry, err := arcgisGeometry(dataset.Geometry, raw.Geometry.Type, raw.Geometry.Coordinates)
		if err != nil {
			return nil, false, fmt.Errorf("feature %d: %w", id, err)
		}
		if geometry = clipToWindow(window, geometry); geometry == nil {
			continue
		}
		features = append(features, arcgismap.Feature{ID: id, Fields: fields, Geometry: *geometry})
	}
	more := page.ExceededTransferLimit || page.Properties.ExceededTransferLimit ||
		len(page.Features) == mapServerPage
	return features, more, nil
}

// mapServerFeatureID is the capture's identity for one national row: the
// curated derivation where the table declares one, the layer's own object
// id otherwise.
func mapServerFeatureID(dataset arcgismap.Dataset, fields arcgismap.Fields, properties map[string]any) (int64, error) {
	if dataset.IDOf == nil {
		return arcgisObjectID(properties)
	}
	id, ok := dataset.IDOf(fields)
	if !ok {
		return 0, fmt.Errorf("no identity in %v", fields)
	}
	return id, nil
}

// clipToWindow cuts a normalized geometry to the window, answering nil when
// nothing of it remains. Points pass untouched: no national layer captures
// them yet, and a stray point is the stray tolerance's business, not this.
func clipToWindow(window arcgismap.Window, geometry *arcgismap.Geometry) *arcgismap.Geometry {
	if geometry == nil {
		return nil
	}
	switch geometry.Type {
	case arcgismap.GeometryRings:
		geometry.Rings = arcgismap.ClipRings(window, geometry.Rings)
		if len(geometry.Rings) == 0 {
			return nil
		}
	case arcgismap.GeometryLines:
		geometry.Lines = arcgismap.ClipLines(window, geometry.Lines)
		if len(geometry.Lines) == 0 {
			return nil
		}
	}
	return geometry
}
