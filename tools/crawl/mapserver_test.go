package main

import (
	"net/url"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/arcgismap"
)

var testWindow = arcgismap.Window{West: -105.2, South: 39.8, East: -104.9, North: 40.0}

func testDataset() arcgismap.Dataset {
	for _, dataset := range arcgismap.National {
		if dataset.Slug == "streams" {
			return dataset
		}
	}
	panic("streams is not curated")
}

func TestMapServerQueryURLSpellsThePage(t *testing.T) {
	address := mapServerQueryURL(testDataset(), testWindow, 4000)
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(parsed.Path, "/6/query") {
		t.Fatalf("path is %q", parsed.Path)
	}
	values := parsed.Query()
	wants := map[string]string{
		"f":                 "geojson",
		"where":             "gnis_name IS NOT NULL",
		"geometry":          "-105.2,39.8,-104.9,40",
		"geometryType":      "esriGeometryEnvelope",
		"inSR":              "4326",
		"outSR":             "4326",
		"spatialRel":        "esriSpatialRelIntersects",
		"outFields":         "OBJECTID,gnis_name",
		"geometryPrecision": "7",
		"orderByFields":     "OBJECTID",
		"resultOffset":      "4000",
		"resultRecordCount": "2000",
	}
	for key, want := range wants {
		if got := values.Get(key); got != want {
			t.Fatalf("%s is %q, want %q", key, got, want)
		}
	}
	// The where-clause must actually be escaped on the wire.
	if strings.Contains(address, "gnis_name IS") {
		t.Fatalf("where rides unescaped: %s", address)
	}
}

func TestDecodeMapServerPageReadsFeaturesAndClips(t *testing.T) {
	body := []byte(`{"type":"FeatureCollection","features":[
		{"properties":{"OBJECTID":7,"gnis_name":"Big Dry Creek"},
		 "geometry":{"type":"LineString","coordinates":[[-105.1,39.9],[-105.0,39.9]]}},
		{"properties":{"OBJECTID":8,"gnis_name":"Elsewhere Creek"},
		 "geometry":{"type":"LineString","coordinates":[[-99.0,39.9],[-98.9,39.9]]}}
	]}`)
	features, more, err := decodeMapServerPage(testDataset(), testWindow, body, "test")
	if err != nil {
		t.Fatal(err)
	}
	if more {
		t.Fatal("a short page claims another waits")
	}
	if len(features) != 1 || features[0].ID != 7 {
		t.Fatalf("features are %+v; the out-of-window line should be cut away", features)
	}
	if features[0].Fields["gnis_name"] != "Big Dry Creek" {
		t.Fatalf("fields are %v", features[0].Fields)
	}
}

func TestDecodeMapServerPageReadsTheTruncationFlag(t *testing.T) {
	spellings := []string{
		`{"type":"FeatureCollection","exceededTransferLimit":true,"features":[]}`,
		`{"type":"FeatureCollection","properties":{"exceededTransferLimit":true},"features":[]}`,
	}
	for _, body := range spellings {
		_, more, err := decodeMapServerPage(testDataset(), testWindow, []byte(body), "test")
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			t.Fatalf("truncation went unread in %s", body)
		}
	}
}

func TestDecodeMapServerPageRefusesAnErrorAnswer(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"Invalid parameters"}}`)
	if _, _, err := decodeMapServerPage(testDataset(), testWindow, body, "test"); err == nil {
		t.Fatal("an error answer decoded as a page")
	}
}

func TestMapServerFeatureIDPrefersTheCuratedDerivation(t *testing.T) {
	var subwatersheds arcgismap.Dataset
	for _, dataset := range arcgismap.National {
		if dataset.Slug == arcgismap.SlugSubwatersheds {
			subwatersheds = dataset
		}
	}
	id, err := mapServerFeatureID(subwatersheds,
		arcgismap.Fields{"huc12": "101900030304"}, map[string]any{"objectid": float64(9)})
	if err != nil {
		t.Fatal(err)
	}
	if id != 101900030304 {
		t.Fatalf("id is %d, want the hydrologic unit code", id)
	}
	if _, err := mapServerFeatureID(subwatersheds, arcgismap.Fields{}, nil); err == nil {
		t.Fatal("a row without its code passed")
	}
}
