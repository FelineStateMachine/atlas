// Package national joins a volume's own ground to the national hydrography it
// was captured beside.
//
// A city's zones and trails are curated by the city. The subwatersheds under
// them are surveyed by the USGS, and a capture that carries both can say, of
// each piece of the city's ground, which subwatershed it lies in. That sentence
// is worth one line on a card and one machine-readable key beside it, and it is
// computed here rather than at capture time so that re-curating the sentence
// never needs a re-fetch: the evidence base travels in the capture, and the join
// re-runs against it.
//
// # The under-claiming ethic
//
// The claim is made only when every piece of ground a feature answers for
// agrees on one subwatershed. A trail that crosses a divide says nothing. A
// zone whose boundary leaves the surveyed extent says nothing. This is the
// lesson an excised zoning-rules join left behind: one earned sentence beats a
// wall of uncorrelated prose, and a claim that is merely plausible is worse than
// silence because a reader cannot tell the two apart.
package national

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// Name is what curation queues this enricher as.
const Name = "national"

// EvidenceName is what the evidence base calls the surveyed subwatersheds.
const EvidenceName = "hydro/huc12.json"

// The evidence document's identity. Its schema is the enrich lane's, not a
// source's: whoever captured the survey writes this, and this package reads it,
// so the join re-runs from the archive without either side knowing the other's
// vocabulary.
const (
	EvidenceDoc     = "atlas-enrich-evidence"
	EvidenceVersion = 1
	// EvidenceKind names what this particular evidence is of.
	EvidenceKind = "hydro.huc12"
	// SpaceWorld is the one coordinate space the evidence may be published in:
	// the volume's own projection, which is the space a claim is decided in.
	// Whoever writes the evidence has already projected the survey into the
	// world it was clipped to, so nothing here reprojects anything.
	SpaceWorld = "world"
)

// Evidence is the surveyed hydrologic units, as the capture carries them.
type Evidence struct {
	Evidence string `json:"evidence"`
	Version  int    `json:"version"`
	Kind     string `json:"kind"`
	Space    string `json:"space"`
	Units    []Unit `json:"units"`
}

// Unit is one hydrologic unit: its twelve-digit code, its name, and its ground
// as polygon rings in the volume's own projection.
type Unit struct {
	Code  string           `json:"code"`
	Name  string           `json:"name"`
	Rings [][][][2]float64 `json:"rings"`
}

// ClaimSamples is how many boundary positions a feature volunteers beside its
// anchor, per piece of geometry. The anchor alone would let a zone made of one
// citywide polygon claim whichever subwatershed its centroid happens to sit in;
// sampling the boundary catches the spread, and the cost stays flat no matter
// how detailed the ring.
const ClaimSamples = 16

// Enricher writes the membership join.
type Enricher struct{}

// New builds the enricher.
func New() *Enricher { return &Enricher{} }

func (*Enricher) Name() string { return Name }

func (*Enricher) Declares() []string { return []string{semconv.KeyHydroHUC12} }

// Enrich reads the evidence base and claims what it can be sure of.
//
// An evidence base that carries nothing about this volume is not an error: it
// is a volume nobody surveyed, and the enricher says nothing about it.
func (e *Enricher) Enrich(v *enrich.Volume, ctx enrich.Context) (enrich.Contribution, error) {
	out := enrich.Contribution{Enricher: Name, Volume: v.Slug}
	if ctx.Evidence == nil {
		return out, nil
	}
	log := ctx.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With(logging.Op(Name), logging.Volume(v.Slug))

	data, held, err := ctx.Evidence.Open(EvidenceName)
	if err != nil {
		return enrich.Contribution{}, fmt.Errorf("read %s: %w", EvidenceName, err)
	}
	if !held {
		log.Debug("no hydrography was captured beside this volume")
		return out, nil
	}
	evidence, err := ReadEvidence(data)
	if err != nil {
		return enrich.Contribution{}, err
	}
	if len(evidence.Units) == 0 {
		return out, nil
	}

	// The evidence does not make claims about itself: the hydrography is the
	// survey, and a subwatershed needs no sentence saying which subwatershed it
	// lies in.
	surveyed := make(map[string]bool)
	for _, title := range ctx.Curation.EvidenceCollections(v.Slug) {
		surveyed[title] = true
	}

	claimed, straddled := 0, 0
	for _, world := range v.Worlds {
		for _, collection := range world.Collections {
			if collection.Kind == enrich.KindPoint || surveyed[collection.Title] {
				continue
			}
			for _, feature := range collection.Features {
				if feature.Attrs[semconv.KeyHydroHUC12] != "" {
					continue
				}
				unit, sure := claim(evidence.Units, feature)
				if !sure {
					straddled++
					continue
				}
				claimed++
				out.Ops = append(out.Ops, enrich.Op{
					Kind:    enrich.OpSetAttr,
					World:   world.Slug,
					Feature: feature.ID,
					Entity:  semconv.EntityFeature,
					Key:     semconv.KeyHydroHUC12,
					Value:   unit.Code,
				})
				if feature.Description == "" {
					out.Ops = append(out.Ops, enrich.Op{
						Kind:    enrich.OpSetProse,
						World:   world.Slug,
						Feature: feature.ID,
						Value:   Sentence(unit),
					})
				}
			}
		}
	}
	if claimed > 0 || straddled > 0 {
		log.Info("hydrologic membership joined", "claimed", claimed, "unclaimed", straddled,
			"units", len(evidence.Units))
	}
	return out, nil
}

// Sentence is the one line a claim earns on a card.
func Sentence(unit Unit) string {
	return fmt.Sprintf("Lies in the %s subwatershed (HUC %s).", unit.Name, unit.Code)
}

// ReadEvidence decodes an evidence document and holds it to what this package
// can actually reason about.
func ReadEvidence(data []byte) (Evidence, error) {
	var evidence Evidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return Evidence{}, fmt.Errorf("decode %s: %w", EvidenceName, err)
	}
	if evidence.Evidence != EvidenceDoc {
		return Evidence{}, fmt.Errorf("evidence says %q, not %q", evidence.Evidence, EvidenceDoc)
	}
	if evidence.Version != EvidenceVersion {
		return Evidence{}, fmt.Errorf("evidence version %d, want %d", evidence.Version, EvidenceVersion)
	}
	if evidence.Kind != EvidenceKind {
		return Evidence{}, fmt.Errorf("evidence is of %q, not %q", evidence.Kind, EvidenceKind)
	}
	if evidence.Space != SpaceWorld {
		return Evidence{}, fmt.Errorf(
			"evidence is published in %q; this join reads %q, the volume's own projection",
			evidence.Space, SpaceWorld)
	}
	for _, unit := range evidence.Units {
		if err := semconv.Check(semconv.EntityFeature, semconv.KeyHydroHUC12, unit.Code); err != nil {
			return Evidence{}, fmt.Errorf("evidence unit %q: %w", unit.Name, err)
		}
	}
	return evidence, nil
}

// claim answers which unit a feature's ground lies in, and whether the answer
// is unanimous. Every position the feature answers for has to land in the same
// unit, and none may land outside the survey.
func claim(units []Unit, feature enrich.Feature) (Unit, bool) {
	found := -1
	for _, geometry := range feature.Geometry {
		for _, position := range Positions(geometry) {
			at := locate(units, position[0], position[1])
			if at < 0 {
				return Unit{}, false
			}
			if found >= 0 && at != found {
				return Unit{}, false
			}
			found = at
		}
	}
	if found < 0 {
		return Unit{}, false
	}
	return units[found], true
}

// locate answers which unit holds a position, by index, or -1. Units are
// visited in the order the evidence lists them, so a position exactly on a
// shared boundary answers the same way every run.
func locate(units []Unit, lng, lat float64) int {
	for index, unit := range units {
		if inRings(unit.Rings, lng, lat) {
			return index
		}
	}
	return -1
}

// inRings is even-odd ray casting over polygon nesting: crossing an outer ring
// enters ground, crossing a hole leaves it again.
func inRings(polygons [][][][2]float64, lng, lat float64) bool {
	inside := false
	for _, polygon := range polygons {
		for _, ring := range polygon {
			for at := 0; at+1 < len(ring); at++ {
				a, b := ring[at], ring[at+1]
				if (a[1] > lat) == (b[1] > lat) {
					continue
				}
				crossing := a[0] + (lat-a[1])/(b[1]-a[1])*(b[0]-a[0])
				if lng < crossing {
					inside = !inside
				}
			}
		}
	}
	return inside
}

// Positions is the ground one piece of a feature's geometry answers for: its
// anchor, and an even sample of its outer-ring or line vertices.
//
// It is exported because it is the whole of what "how much of a feature is
// looked at" means, and a test that wants to say what a claim stood on has to
// be able to ask.
func Positions(g enrich.Geometry) [][2]float64 {
	var out [][2]float64
	var boundary [][2]float64
	best := 0.0
	var anchor [2]float64
	held := false

	for _, polygon := range g.Rings() {
		if len(polygon) == 0 {
			continue
		}
		if lng, lat, area, ok := ringCentroid(polygon[0]); ok && area > best {
			best, anchor, held = area, [2]float64{lng, lat}, true
		}
		boundary = append(boundary, polygon[0]...)
	}
	for _, line := range g.Lines() {
		length := 0.0
		for at := 0; at+1 < len(line); at++ {
			length += math.Hypot(line[at+1][0]-line[at][0], line[at+1][1]-line[at][1])
		}
		if length > best && len(line) > 0 {
			best, anchor, held = length, line[len(line)/2], true
		}
		boundary = append(boundary, line...)
	}
	if !held {
		// A point feature is its own anchor, though no point collection is
		// joined today.
		positions := g.Positions()
		if len(positions) == 1 {
			return positions
		}
		return nil
	}
	out = append(out, anchor)
	step := max((len(boundary)+ClaimSamples-1)/ClaimSamples, 1)
	for at := 0; at < len(boundary); at += step {
		out = append(out, boundary[at])
	}
	return out
}

// ringCentroid is the shoelace centroid of one ring, with the enclosed area to
// rank rings by.
func ringCentroid(ring [][2]float64) (lng, lat, area float64, ok bool) {
	var doubled, sumX, sumY float64
	for at := 0; at+1 < len(ring); at++ {
		a, b := ring[at], ring[at+1]
		cross := a[0]*b[1] - b[0]*a[1]
		doubled += cross
		sumX += (a[0] + b[0]) * cross
		sumY += (a[1] + b[1]) * cross
	}
	if doubled == 0 {
		return 0, 0, 0, false
	}
	return sumX / (3 * doubled), sumY / (3 * doubled), math.Abs(doubled) / 2, true
}
