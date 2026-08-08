package app

import (
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestVNextEarthBuildsTheRendererDirectly(t *testing.T) {
	t.Parallel()

	native, err := vnext.OpenFile("../../included/earth-20260803-28305c3d1811.atlas", vnext.StandardSchema())
	if err != nil {
		t.Fatalf("open native Earth: %v", err)
	}
	defer native.Close()
	semantic, err := native.Volume()
	if err != nil {
		t.Fatalf("restore native Earth: %v", err)
	}

	model, err := buildVNextWorld(semantic.Worlds[0], semantic.Assets)
	if err != nil {
		t.Fatalf("build vNext world: %v", err)
	}
	if len(model.Points) != 202 || len(model.Shapes) != 177 || len(model.Members) != 7 || len(model.Lenses) != 1 {
		t.Fatalf("runtime model has %d points, %d shapes, %d collections and %d lenses", len(model.Points), len(model.Shapes), len(model.Members), len(model.Lenses))
	}
	countries := nativeCollection(model, "Countries")
	if countries == nil || len(countries.Shapes) != 177 {
		t.Fatalf("Countries collection = %#v", countries)
	}
	fiji := nativeShape(countries, "Fiji")
	if fiji == nil || len(fiji.Polygons) < 2 || fiji.Feature == nil {
		t.Fatalf("Fiji native shape = %#v", fiji)
	}
	if !strings.Contains(fiji.ID, "/feature/") || countries.ID == "" {
		t.Fatalf("native identities were not preserved: feature=%q layer=%q", fiji.ID, countries.ID)
	}
}

func nativeCollection(model *worldModel, title string) *collectionModel {
	for _, collection := range model.Members {
		if collection.Title == title {
			return collection
		}
	}
	return nil
}

func nativeShape(collection *collectionModel, title string) *shapeModel {
	for _, shape := range collection.Shapes {
		if shape.Title == title {
			return shape
		}
	}
	return nil
}
