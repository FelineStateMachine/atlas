package bundle_test

import (
	"reflect"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// The stamp is the determinism invariant made concrete, so its arithmetic is
// pinned to literal digests rather than to a description of itself. A change
// to the algorithm renames every bundle in every library, which is a decision
// somebody has to make on purpose.
func TestStampVectors(t *testing.T) {
	cases := []struct {
		name  string
		parts [][2]string
		want  string
	}{
		{
			name: "nothing added",
			want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "one part",
			parts: [][2]string{{"atlas.json", "aa"}},
			want:  "195c654d74045824d198e3e92876b6cf83a9cac67f12d860ddcc41e73c469d2b",
		},
		{
			name: "a manifest, a payload, and a pyramid",
			parts: [][2]string{
				{"atlas.json", bundle.HashBytes([]byte("manifest"))},
				{"worlds/overworld.json", bundle.HashBytes([]byte("payload"))},
				{"tiles/overworld", "deadbeef"},
			},
			want: "bfda76a0377d98a607c0e4c70f365457cb1f00a921b7026c246b23c58f11151e",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var stamp bundle.Stamp
			for _, part := range test.parts {
				stamp.Add(part[0], part[1])
			}
			if got := stamp.Sum(); got != test.want {
				t.Errorf("Sum = %s, want %s", got, test.want)
			}
		})
	}
}

func TestHashBytesIsSHA256Hex(t *testing.T) {
	cases := map[string]string{
		"":    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"abc": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	}
	for input, want := range cases {
		if got := bundle.HashBytes([]byte(input)); got != want {
			t.Errorf("HashBytes(%q) = %s, want %s", input, got, want)
		}
	}
}

// Order independence is what lets a producer add parts as it happens to build
// them -- worlds in whatever order, pyramids from a map -- and still agree
// with a producer that built them in another order.
func TestStampIsOrderIndependent(t *testing.T) {
	parts := [][2]string{
		{"atlas.json", "1"},
		{"worlds/a.json", "2"},
		{"worlds/b.json", "3"},
		{"tiles/a", "4"},
		{"icons/marker.svg", "5"},
	}
	var forward bundle.Stamp
	for _, part := range parts {
		forward.Add(part[0], part[1])
	}
	var backward bundle.Stamp
	for index := len(parts) - 1; index >= 0; index-- {
		backward.Add(parts[index][0], parts[index][1])
	}
	if forward.Sum() != backward.Sum() {
		t.Errorf("adding the same parts in reverse gave %s, not %s", backward.Sum(), forward.Sum())
	}
	if !reflect.DeepEqual(forward.Parts(), backward.Parts()) {
		t.Errorf("Parts disagree: %v and %v", forward.Parts(), backward.Parts())
	}
	// Summing does not consume, so a producer may sum, add more, and sum again.
	before := forward.Sum()
	if forward.Sum() != before {
		t.Error("summing twice gave two answers")
	}
}

// The name of a part is as load-bearing as its hash: two parts holding the
// same bytes under different names are a different bundle, and a renamed part
// is a rebuild.
func TestStampSeparatesNameFromHash(t *testing.T) {
	var named, renamed bundle.Stamp
	named.Add("worlds/a.json", "ff")
	renamed.Add("worlds/b.json", "ff")
	if named.Sum() == renamed.Sum() {
		t.Error("renaming a part did not change the stamp")
	}

}

// The separator is a single space, which is only unambiguous because a part
// name holds none. The collision is pinned here rather than fixed: fixing it
// would restamp every bundle in every library, and the constraint costs
// nothing to keep. A producer naming a part with a space is the bug.
func TestStampPartNamesMustNotHoldSpaces(t *testing.T) {
	var left, right bundle.Stamp
	left.Add("a b", "c")
	right.Add("a", "b c")
	if left.Sum() != right.Sum() {
		t.Error("the space separator has stopped being ambiguous; docs/format.md says it is")
	}

	// Under the constraint, names and hashes stay distinguishable.
	var named, hashed bundle.Stamp
	named.Add("worlds/a.json", "ff00")
	hashed.Add("worlds/a.jsonff00", "")
	if named.Sum() == hashed.Sum() {
		t.Error("a space-free name collided with its own hash")
	}
}

func TestShortStamp(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"abc":                  "abc",
		"abcdefghijkl":         "abcdefghijkl",
		"abcdefghijklmnopqrst": "abcdefghijkl",
	}
	for input, want := range cases {
		if got := bundle.ShortStamp(input); got != want {
			t.Errorf("ShortStamp(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCaptureDay(t *testing.T) {
	cases := map[string]string{
		"2026-08-01T09:30:00Z":        "20260801",
		"2026-08-01T20:13:08.883648Z": "20260801",
		"2026-08-01":                  "20260801",
		"20260801":                    "20260801",
		"2026-08":                     "202608",
		"":                            "",
		"never":                       "",
	}
	for input, want := range cases {
		if got := bundle.CaptureDay(input); got != want {
			t.Errorf("CaptureDay(%q) = %q, want %q", input, got, want)
		}
	}
}

// The file name is derived from nothing but the manifest, so the same build
// carries the same name wherever and whenever it is written.
func TestVersionedFileName(t *testing.T) {
	stamp := "abababababababababababababababababababababababababababababababab"
	cases := []struct {
		name      string
		slug      string
		createdAt string
		stamp     string
		want      string
	}{
		{"an ordinary build", "fixture", "2026-08-01T09:30:00Z", stamp, "fixture-20260801-abababababab.atlas"},
		{"a fractional capture time", "mars", "2026-08-01T14:48:10.486177Z", stamp, "mars-20260801-abababababab.atlas"},
		{"a short stamp", "fixture", "2026-08-01T09:30:00Z", "abc", "fixture-20260801-abc.atlas"},
		{"no capture time at all", "fixture", "", stamp, "fixture-abababababab.atlas"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := bundle.Manifest{
				Volume:  bundle.Volume{Slug: test.slug},
				Version: bundle.Version{Stamp: test.stamp, CreatedAt: test.createdAt},
			}
			if got := bundle.VersionedFileName(manifest); got != test.want {
				t.Errorf("name = %q, want %q", got, test.want)
			}
		})
	}
}
