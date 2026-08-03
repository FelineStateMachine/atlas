// Package curation holds the enrich lane's editorial decisions as data.
//
// Two kinds of thing live here. The first is the order the enrichers run in,
// which the lane's contract requires to be declared rather than emergent: a
// queue in a reviewed file is a decision a reader can see, where a queue that
// falls out of a registration order is folklore. The second is the handful of
// numbers and per-key policies that decide what a contribution is allowed to
// conclude -- how near is near enough to be the same place, which attributes a
// serving reading takes from another when it has none of its own, and which
// collections a national evidence base is not allowed to make claims about.
//
// The file is curation.json, embedded, so a build carries its own curation and
// an enriched volume cannot depend on what happened to be on the operator's
// disk. Its schema is documented in docs/enrich.md; this package is the reader
// and the checker.
package curation

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

//go:embed curation.json
var embedded []byte

// Schema is the curation file layout this package reads. It moves when an
// existing field's meaning breaks.
const Schema = 1

// Tables is the whole curated corpus, read. It satisfies the enrich lane's
// Curation interface.
type Tables struct {
	queue            []string
	matchRadius      float64
	separateRadius   float64
	nearbyFloor      float64
	donorFillsEmpty  map[string]bool
	evidenceByVolume map[string][]string
}

type wire struct {
	Schema int `json:"schema"`
	Queue  struct {
		Order []string `json:"order"`
	} `json:"queue"`
	Merge struct {
		MatchRadiusPx    float64 `json:"matchRadiusPx"`
		SeparateRadiusPx float64 `json:"separateRadiusPx"`
		NearbyFloorPx    float64 `json:"nearbyFloorPx"`
		Attributes       struct {
			DonorFillsEmpty []string `json:"donorFillsEmpty"`
		} `json:"attributes"`
	} `json:"merge"`
	National struct {
		EvidenceCollections map[string]json.RawMessage `json:"evidenceCollections"`
	} `json:"national"`
}

// Load reads the embedded curation. It is the only way to get the real tables,
// so no caller can enrich against curation nobody reviewed.
func Load() (Tables, error) { return parse(embedded) }

// LoadFrom reads curation from bytes, for a test that wants to state its own
// corpus rather than the real one.
func LoadFrom(data []byte) (Tables, error) { return parse(data) }

func parse(data []byte) (Tables, error) {
	var raw wire
	if err := json.Unmarshal(data, &raw); err != nil {
		return Tables{}, fmt.Errorf("decode curation: %w", err)
	}
	if raw.Schema != Schema {
		return Tables{}, fmt.Errorf("curation schema %d, want %d", raw.Schema, Schema)
	}
	if len(raw.Queue.Order) == 0 {
		return Tables{}, fmt.Errorf("curation declares no enricher queue")
	}
	if raw.Merge.MatchRadiusPx <= 0 || raw.Merge.SeparateRadiusPx <= raw.Merge.MatchRadiusPx {
		return Tables{}, fmt.Errorf(
			"curation declares a match radius of %g and a separate radius of %g; "+
				"the second has to be the larger, or nothing is ever held back",
			raw.Merge.MatchRadiusPx, raw.Merge.SeparateRadiusPx)
	}
	if raw.Merge.NearbyFloorPx < 0 {
		return Tables{}, fmt.Errorf("curation declares a negative nearby floor")
	}
	out := Tables{
		queue:            raw.Queue.Order,
		matchRadius:      raw.Merge.MatchRadiusPx,
		separateRadius:   raw.Merge.SeparateRadiusPx,
		nearbyFloor:      raw.Merge.NearbyFloorPx,
		donorFillsEmpty:  make(map[string]bool, len(raw.Merge.Attributes.DonorFillsEmpty)),
		evidenceByVolume: make(map[string][]string, len(raw.National.EvidenceCollections)),
	}
	for _, key := range raw.Merge.Attributes.DonorFillsEmpty {
		// The policy table speaks the conventions' vocabulary, so a key nobody
		// registered is a typo rather than a policy. The one deliberate
		// exception is the name a description travels under, which is
		// registered nowhere on purpose: no payload carries it.
		if _, known := semconv.EntityOf(key); !known && key != semconv.KeyNoteText {
			return Tables{}, fmt.Errorf(
				"curation gives %q an attribute policy, but the conventions registry does not know it", key)
		}
		out.donorFillsEmpty[key] = true
	}
	// The evidence table carries prose keys alongside its volumes, the way
	// every other section does, so a value that is not a list is commentary and
	// not a volume.
	for volume, value := range raw.National.EvidenceCollections {
		var titles []string
		if err := json.Unmarshal(value, &titles); err != nil {
			continue
		}
		out.evidenceByVolume[volume] = titles
	}
	return out, nil
}

// Queue is the order the enrichers run in.
func (t Tables) Queue() []string { return t.queue }

// MatchRadiusPx, SeparateRadiusPx and NearbyFloorPx are the resolution
// distances, in world pixels of the volume's own square.
func (t Tables) MatchRadiusPx() float64    { return t.matchRadius }
func (t Tables) SeparateRadiusPx() float64 { return t.separateRadius }
func (t Tables) NearbyFloorPx() float64    { return t.nearbyFloor }

// DonorFillsEmpty reports whether a matched feature takes this attribute from
// its counterpart when it has none of its own.
func (t Tables) DonorFillsEmpty(key string) bool { return t.donorFillsEmpty[key] }

// EvidenceCollections lists the collections a national evidence base
// contributed to a volume.
func (t Tables) EvidenceCollections(volume string) []string { return t.evidenceByVolume[volume] }
