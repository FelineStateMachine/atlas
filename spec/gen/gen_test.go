package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// root is the repository, found from the package directory the test runs in.
func root(t *testing.T) string {
	t.Helper()
	dir, err := repoRoot("")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestTheCommittedArtifactsAreWhatTheSpecSays is the gate that replaced the
// prose twin's agreement test. The registry, the TypeScript key constants and
// docs/semconv/REGISTRY.md used to be three hand-written statements of one
// vocabulary, held together by a test that read the document with a regular
// expression. They are now one statement and two derivations, and the thing
// worth checking is that the derivations are current: a spec edit that was not
// regenerated is a red gate rather than a quiet drift.
func TestTheCommittedArtifactsAreWhatTheSpecSays(t *testing.T) {
	dir := root(t)
	artifacts, err := build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("the generator produced %d artifacts, want 3", len(artifacts))
	}
	for _, path := range sorted(artifacts) {
		committed, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if !bytes.Equal(committed, artifacts[path]) {
			t.Errorf("%s is out of date with %s — run `go generate ./format/semconv`\n%s",
				path, specFile, firstDifference(committed, artifacts[path]))
		}
	}
}

// TestGenerationIsByteStable is the property the artifacts' committedness
// stands on. Rendering is a pure function of the spec -- every listing in the
// spec's own order or sorted, no map ranged over -- so two runs must produce
// identical bytes. A generator that emitted a map's iteration order would pass
// the test above on the machine that last ran it and fail on every other.
func TestGenerationIsByteStable(t *testing.T) {
	dir := root(t)
	first, err := build(dir)
	if err != nil {
		t.Fatal(err)
	}
	for run := range 4 {
		again, err := build(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range sorted(first) {
			if !bytes.Equal(first[path], again[path]) {
				t.Fatalf("run %d produced different bytes for %s", run+2, path)
			}
		}
	}
}

// Every artifact says it is generated, in its own comment syntax, on its first
// line. The Go form is the one `go` tooling recognises; the others are for a
// person who opened the file to edit it.
func TestEveryArtifactAnnouncesItself(t *testing.T) {
	artifacts, err := build(root(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range sorted(artifacts) {
		first, _, _ := strings.Cut(string(artifacts[path]), "\n")
		if !strings.Contains(first, banner) {
			t.Errorf("%s opens with %q, which does not carry the generated banner", path, first)
		}
	}
}

// A spec the generator would happily render into something nobody can trust is
// refused before anything is written. These are the faults worth naming: they
// are the ones an ordinary edit makes.
func TestTheSpecIsHeldToItsOwnShape(t *testing.T) {
	valid := func() spec {
		return spec{
			Version:     2,
			Namespace:   "atlas.",
			Entities:    []entity{{Name: "world", Const: "EntityWorld"}},
			Stabilities: []named{{Name: "stable", Const: "Stable"}},
			Keys: []key{{
				Key: "atlas.geometry.body", Const: "KeyGeometryBody",
				Entity: "world", Stability: "stable", Check: "slug",
				Doc: "a body", ValuesMD: "slug", Meaning: "the body pictured",
			}},
		}
	}
	reference := valid()
	if err := reference.validate(); err != nil {
		t.Fatalf("the reference spec is refused: %v", err)
	}

	cases := []struct {
		name    string
		break_  func(*spec)
		mention string
	}{
		{"a key outside the namespace", func(s *spec) {
			s.Keys[0].Key = "vendor.thing"
		}, "namespace"},
		{"an entity nobody declared", func(s *spec) {
			s.Keys[0].Entity = "zone"
		}, "not a declared entity"},
		{"a stability tier nobody declared", func(s *spec) {
			s.Keys[0].Stability = "settled"
		}, "not a declared tier"},
		{"a checker nobody wrote", func(s *spec) {
			s.Keys[0].Check = "looksRight"
		}, "not a checker"},
		{"an enum with no vocabulary", func(s *spec) {
			s.Keys[0].Check = "enum"
		}, "vocabulary"},
		{"a vocabulary on something that is not an enum", func(s *spec) {
			s.Keys[0].Values = []vocab{{Const: "Whatever", Value: "whatever"}}
		}, "vocabulary"},
		{"numbers with no arity", func(s *spec) {
			s.Keys[0].Check = "numbers"
			s.Keys[0].Values = nil
		}, "arity"},
		{"an arity on something that counts nothing", func(s *spec) {
			s.Keys[0].Arity = 4
		}, "arity"},
		{"a key documented nowhere", func(s *spec) {
			s.Keys[0].Meaning = ""
		}, "no documented values or meaning"},
		{"one key twice", func(s *spec) {
			s.Keys = append(s.Keys, s.Keys[0])
		}, "declared twice"},
		{"a policy key colliding with a registered one", func(s *spec) {
			s.PolicyKeys = []key{{Key: "atlas.note.text", Const: "KeyGeometryBody"}}
		}, "constant"},
		{"a first key with no comment for the rest to join", func(s *spec) {
			s.Keys[0].Doc = ""
		}, "doc comment"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := valid()
			test.break_(&s)
			err := s.validate()
			if err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Errorf("validate = %v, wanted mention of %q", err, test.mention)
			}
		})
	}
}

// firstDifference points at where two renderings part company, so a failure
// reads as a diff rather than as two walls of generated text.
func firstDifference(have, want []byte) string {
	haveLines := strings.Split(string(have), "\n")
	wantLines := strings.Split(string(want), "\n")
	for index := range max(len(haveLines), len(wantLines)) {
		h, w := at(haveLines, index), at(wantLines, index)
		if h != w {
			return fmt.Sprintf("  line %d\n  committed: %s\n  generated: %s", index+1, h, w)
		}
	}
	return "  the files differ only in their trailing bytes"
}

func at(lines []string, index int) string {
	if index < len(lines) {
		return lines[index]
	}
	return "(end of file)"
}
