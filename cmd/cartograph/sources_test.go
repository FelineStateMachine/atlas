package main

import (
	"reflect"
	"testing"
)

func TestRegistry(t *testing.T) {
	if len(sources) != 3 {
		t.Fatalf("registry holds %d sources, want 3", len(sources))
	}
	for _, slug := range []string{"mapgenie", "ign-wiki", "piggyback"} {
		src, ok := sourceBySlug(slug)
		if !ok {
			t.Fatalf("registry misses %s", slug)
		}
		// All three publish complete maps; they differ in quality per
		// component, never in kind.
		for _, component := range []Component{
			ComponentRaster, ComponentIcons, ComponentLocations, ComponentMetadata,
		} {
			if !src.Components().Has(component) {
				t.Errorf("%s misses component %s", slug, component)
			}
		}
		if src.Name() == "" || src.Description() == "" || src.TargetHint() == "" {
			t.Errorf("%s is missing a face: %q %q %q", slug, src.Name(), src.Description(), src.TargetHint())
		}
	}
	if _, ok := sourceBySlug("nowhere"); ok {
		t.Error("registry answered for a source it does not hold")
	}
}

func TestFetchArgs(t *testing.T) {
	good := []struct {
		source string
		target string
		want   []string
	}{
		{"mapgenie", "cyberpunk-2077", []string{"-game", "cyberpunk-2077"}},
		{"ign-wiki", "cyberpunk-2077/night-city", []string{"-ign", "cyberpunk-2077/night-city"}},
		{"piggyback", "cyberpunk-2077/night-city", []string{"-piggyback", "cyberpunk-2077/night-city"}},
	}
	for _, at := range good {
		src, _ := sourceBySlug(at.source)
		got, err := src.FetchArgs(at.target)
		if err != nil {
			t.Errorf("%s %q refused: %v", at.source, at.target, err)
			continue
		}
		if !reflect.DeepEqual(got, at.want) {
			t.Errorf("%s %q built %v, want %v", at.source, at.target, got, at.want)
		}
	}

	bad := []struct {
		source string
		target string
	}{
		{"mapgenie", ""},
		{"mapgenie", "cyberpunk-2077/night-city"}, // single-slug source takes no slash
		{"mapgenie", "-list"},                     // a target must never read as a flag
		{"mapgenie", "night city"},
		{"ign-wiki", "night-city"},   // pair source needs its slash
		{"ign-wiki", "a/b/c"},        // exactly one
		{"piggyback", "/night-city"}, // no empty half
		{"piggyback", "night-city/"},
		{"piggyback", "Night-City/x"}, // slugs are lowercase
	}
	for _, at := range bad {
		src, _ := sourceBySlug(at.source)
		if _, err := src.FetchArgs(at.target); err == nil {
			t.Errorf("%s accepted %q", at.source, at.target)
		}
	}
}
