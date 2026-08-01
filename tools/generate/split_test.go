package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// A zone's prose defers into the text payload the way a pin's does: the
// detail carries only the marker, and the words wait to be asked for.
func TestBuildPayloadDefersZoneProse(t *testing.T) {
	world := catalogWorld{
		Zones: []zone{
			{ID: 71, Title: "PF", Description: "Public facilities.\n\nData: Zoneomics"},
			{ID: 72, Title: "RS"},
		},
	}
	detail, _, text := buildPayload(world)

	entry, told := text["71"]
	if !told || entry.Description != "Public facilities.\n\nData: Zoneomics" {
		t.Fatalf("zone text = %+v, %v", entry, told)
	}
	if _, told := text["72"]; told {
		t.Fatal("a quiet zone wrote text")
	}
	if !detail.Zones[0].HasText || detail.Zones[1].HasText {
		t.Fatalf("hasText markers are %v/%v", detail.Zones[0].HasText, detail.Zones[1].HasText)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Public facilities") {
		t.Fatal("zone prose rode the detail payload")
	}
}
