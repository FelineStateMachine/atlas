package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/enrich/align"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// The tile deriver, held against the pyramids the reference implementation left
// in the tile cache.
//
// Two things are proven separately here, because only one of them can be.
//
// The **tiles** can be identical, and are: rebuilding a pyramid from the frames
// the archive holds writes the same bytes, tile for tile, as the reference tool
// wrote. That is the whole observable claim -- a bundle carries tiles, and these
// are those tiles.
//
// The **stamp** cannot be, by construction. A derivation stamp covers the
// deriving code's own source, deliberately: changing how a level is reduced has
// to invalidate every pyramid, and a stamp that only watched the archive would
// quietly keep serving the old derivation. A clean-room deriver is by definition
// different source, so it stamps differently however identical its output. What
// is proven instead is the rest of the stamp -- the plan: the same frame, the
// same complete level, the same content bounds, the same encoding, the same
// captured tiles with the same hashes in the same order. Feeding the reference
// implementation's own tool hash into the clean room's stamp function reproduces
// the recorded stamp exactly, which says the two plans are the same plan.
//
// golden/format/STAMPS.md carries what that costs per volume.

// referenceTool is the tool hash the reference implementation's derivations were
// stamped with: SHA-256 over its embedded sources, name and length framed by
// NUL, in sorted name order. The files are read from the golden-reference tree
// still in this checkout -- the oracle, not the subject.
func referenceTool(t *testing.T) string {
	t.Helper()
	const dir = "../../tools/tiles"
	names := []string{"main.go", "stamp.go", "warp.go"}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Skipf("the golden-reference tile tool is not in this checkout: %v", err)
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", name, len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// planned is one lens's plan, the ground it pictures, and the pyramid the
// reference left for it.
type planned struct {
	plan      tiles.Plan
	world     doc.World
	reference tiles.Pyramid
}

// planFixture plans every pyramid of one volume against the reference tile set.
func planFixture(t *testing.T, volume string) []planned {
	t.Helper()
	store, err := archive.Open(archiveDir(t))
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	set, err := tiles.Open(tileIndex(t))
	if err != nil {
		t.Fatalf("tile set: %v", err)
	}
	tables, err := curation.Load()
	if err != nil {
		t.Fatalf("curation: %v", err)
	}

	var out []planned
	for _, ref := range store.Volumes() {
		source, err := sources.For(ref.Source)
		if err != nil {
			continue
		}
		document, err := source.Translate(store, ref, slog.New(slog.DiscardHandler))
		if err != nil {
			if errors.Is(err, sources.ErrNotReady) {
				continue
			}
			t.Fatalf("translate %s: %v", ref.Title, err)
		}
		if document.Volume.Slug != volume {
			continue
		}
		worlds, err := store.Worlds(ref)
		if err != nil {
			t.Fatal(err)
		}
		byWorld := make(map[string]archive.WorldRef, len(worlds))
		for _, world := range worlds {
			byWorld[world.Slug] = world
		}
		for _, world := range document.Worlds {
			held, known := byWorld[world.Slug]
			if !known {
				t.Fatalf("the archive holds no world %s", world.Slug)
			}
			captured, err := store.Tiles(held)
			if err != nil {
				t.Fatalf("captured tiles of %s: %v", world.Slug, err)
			}
			for _, lens := range world.Lenses {
				reference, known := set.Native(lens.TileSet)
				if !known {
					t.Fatalf("the reference tile set holds no pyramid for %s", lens.TileSet)
				}
				plan, err := tiles.PlanLens(store, held, reference.Name, lens,
					captured[lens.TileSet], !tables.PixelArt(document.Volume.Slug))
				if err != nil {
					t.Fatalf("plan %s: %v", lens.TileSet, err)
				}
				out = append(out, planned{plan: plan, world: world, reference: reference})
			}
		}
	}
	// Every reading, not the first: two sources may both answer for one volume,
	// and the pair of them is exactly what a warped variant is planned from.
	if len(out) == 0 {
		t.Fatalf("the archive at %s holds no readable %s", archiveDir(t), volume)
	}
	return out
}

// TestDeriverPlansTheReferencePyramid holds the plan half of a derivation stamp
// against the stamp the reference recorded, for every single-source fixture.
//
// This is the closest a clean-room deriver can come to stamp identity, and it is
// close: everything the stamp covers except the deriving code's own hash is
// reproduced bit for bit, over every captured tile of every level.
func TestDeriverPlansTheReferencePyramid(t *testing.T) {
	tool := referenceTool(t)
	for _, subject := range singleSource {
		t.Run(subject.volume, func(t *testing.T) {
			for _, entry := range planFixture(t, subject.volume) {
				got := tiles.StampWith(entry.plan, tool)
				if got != entry.reference.Stamp {
					t.Errorf("pyramid %s plans to %s, the reference recorded %s",
						entry.plan.Name, got, entry.reference.Stamp)
					continue
				}
				t.Logf("%s: plan-identical (%s)", entry.plan.Name, got[:12])
				// The clean room's own stamp differs, and has to: the tool is
				// part of what a derivation was made from.
				if tiles.PlanStamp(entry.plan) == entry.reference.Stamp {
					t.Errorf("pyramid %s stamps identically to a different tool, "+
						"so the tool is not in the stamp", entry.plan.Name)
				}
			}
		})
	}
}

// TestDeriverWritesTheReferenceTiles rebuilds one pyramid from the frames the
// archive holds and compares every tile it wrote against the reference cache,
// byte for byte.
//
// Tunic is the subject because it is the plainest shape the deriver handles --
// one picture, one complete level, a photographic reduction, a background tile
// omitted from every level -- and because 741 tiles is small enough to rebuild
// in a test and large enough that a wrong reduction cannot hide.
func TestDeriverWritesTheReferenceTiles(t *testing.T) {
	entries := planFixture(t, "tunic")
	if len(entries) != 1 {
		t.Fatalf("tunic planned %d pyramids, want one", len(entries))
	}
	plan, reference := entries[0].plan, entries[0].reference

	root := t.TempDir()
	built, err := tiles.Derive(root, plan)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	// The register entry says the same thing about the pyramid.
	if built.MaxZoom != reference.MaxZoom || built.FullZoom != reference.FullZoom ||
		built.SourceZoom != reference.SourceZoom || built.Window != reference.Window ||
		built.Interpolate != reference.Interpolate || built.Background != reference.Background {
		t.Errorf("derived %+v, the reference recorded %+v", built, reference)
	}
	if strings.Join(built.Formats, ",") != strings.Join(reference.Formats, ",") {
		t.Errorf("formats %v, reference %v", built.Formats, reference.Formats)
	}
	if (built.Bounds == nil) != (reference.Bounds == nil) ||
		(built.Bounds != nil && *built.Bounds != *reference.Bounds) {
		t.Errorf("bounds %+v, reference %+v", built.Bounds, reference.Bounds)
	}
	if len(built.Coverage) != len(reference.Coverage) {
		t.Errorf("%d covered levels, reference %d", len(built.Coverage), len(reference.Coverage))
	}
	for level, mask := range reference.Coverage {
		got, held := built.Coverage[level]
		if !held || got == nil || *got != *mask {
			t.Errorf("level %s covers %+v, reference %+v", level, got, mask)
		}
	}

	// And every tile it wrote is the tile the reference wrote.
	want := hashTree(t, filepath.Join(filepath.Dir(tileIndex(t)), reference.Name))
	got := hashTree(t, filepath.Join(root, plan.Name))
	if len(got) != len(want) {
		t.Fatalf("derived %d tiles, the reference cache holds %d", len(got), len(want))
	}
	differed := 0
	for name, hash := range want {
		switch {
		case got[name] == "":
			t.Errorf("%s was not derived", name)
		case got[name] != hash:
			differed++
			if differed <= 3 {
				t.Errorf("%s is %s, reference %s", name, got[name], hash)
			}
		}
	}
	if differed > 3 {
		t.Errorf("and %d more tiles differ", differed-3)
	}
	if differed == 0 {
		t.Logf("%d tiles rebuilt byte for byte from the archive", len(got))
	}
}

// The warped variant, held to the same two halves.
//
// Cyberpunk's night city is captured twice: a publisher's own raster at zoom 7
// and a wiki's rasterized rendering at zoom 5. The second is resampled into the
// first's world through the affine their shared place names determine, and
// offered as a second picture of one ground. That resample is the one pyramid in
// the corpus whose derivation takes an input the archive does not hold -- the
// alignment -- and it is therefore the one whose stamp proves something extra:
// six numbers to nine decimal places, reproduced from the captures alone.

// alignedPair is the two readings of one ground and the warp between them.
type alignedPair struct {
	base  tiles.Plan
	donor tiles.Plan
	warp  tiles.Plan
}

// planAlignedPyramid plans cyberpunk's aligned variant from the archive.
//
// The pairing is stated rather than searched. Which readings pair, and which of
// them is the frame the other is brought into, is the pipeline's policy and
// lives in cmd/atlas; what this gate is measuring is the derivation, so it names
// the pair the fixture is a fixture of and asserts the one property of the
// policy that the derivation depends on -- that the deeper picture is the base.
func planAlignedPyramid(t *testing.T) alignedPair {
	t.Helper()
	byTileSet := map[string]planned{}
	for _, entry := range planFixture(t, "cyberpunk-2077") {
		byTileSet[entry.plan.TileSet] = entry
	}
	base, held := byTileSet["cbp"]
	if !held {
		t.Fatal("the archive holds no Piggyback reading of night city")
	}
	donor, held := byTileSet["cyberpunk-2077/night-city"]
	if !held {
		t.Fatal("the archive holds no IGN reading of night city")
	}
	if tiles.WorldDepth(base.plan) <= tiles.WorldDepth(donor.plan) {
		t.Fatalf("the base draws its world at %d pixels and the donor at %d; "+
			"a warp into a coarser world would throw away what was captured",
			tiles.WorldDepth(base.plan), tiles.WorldDepth(donor.plan))
	}

	affine, report, err := align.Fit(anchorsOfPlan(donor), anchorsOfPlan(base))
	if err != nil {
		t.Fatalf("the two readings do not align: %v", err)
	}
	t.Logf("alignment: %s", report)
	warp := tiles.PlanWarp(base.plan, donor.plan, tiles.Affine{
		AX: affine.AX, BX: affine.BX, CX: affine.CX,
		AY: affine.AY, BY: affine.BY, CY: affine.CY,
	}, "IGN Wiki")
	return alignedPair{base: base.plan, donor: donor.plan, warp: warp}
}

// anchorsOfPlan lands a reading's named places in its own world pixels, which is
// the space the fit is made in.
func anchorsOfPlan(entry planned) []align.Anchor {
	zoom, first := entry.plan.Frame.Window()
	grid := enrich.Grid{SourceZoom: zoom, FirstTile: first,
		TileSize: tiles.TileSize, Size: tiles.WorldSize}
	var out []align.Anchor
	for _, collection := range entry.world.Collections {
		if collection.Kind != doc.KindPoint {
			continue
		}
		for _, feature := range collection.Features {
			if feature.At == nil {
				continue
			}
			out = append(out, align.Anchor{Title: feature.Title,
				X: grid.ProjectX(feature.At.Lng), Y: grid.ProjectY(feature.At.Lat)})
		}
	}
	return out
}

// TestDeriverPlansTheAlignedPyramid holds the warped plan to the stamp the
// reference recorded for it.
//
// This is a stronger statement than the same test makes about a native pyramid.
// A warp's stamp covers the alignment itself, printed to nine decimal places, so
// reproducing it means the anchors, the name matching, the trimming, the
// least-squares fit and the target zoom all came out identical from the captures
// alone -- and that the two lanes agree, because the affine in this stamp is the
// one the enrich lane's merge folds the features by.
func TestDeriverPlansTheAlignedPyramid(t *testing.T) {
	tool := referenceTool(t)
	pair := planAlignedPyramid(t)

	set, err := tiles.Open(tileIndex(t))
	if err != nil {
		t.Fatalf("tile set: %v", err)
	}
	var reference tiles.Pyramid
	for _, aligned := range set.Aligned(pair.base.TileSet) {
		if aligned.Name == pair.warp.Name {
			reference = aligned
		}
	}
	if reference.Name == "" {
		t.Fatalf("the reference tile set holds no aligned pyramid named %s", pair.warp.Name)
	}
	if reference.TileSet != pair.warp.TileSet {
		t.Errorf("the warp is planned from %s, the reference recorded %s",
			pair.warp.TileSet, reference.TileSet)
	}
	if got := tiles.StampWith(pair.warp, tool); got != reference.Stamp {
		t.Fatalf("the warp plans to %s, the reference recorded %s", got, reference.Stamp)
	}
	t.Logf("%s: plan-identical, alignment and all (%s)", pair.warp.Name, reference.Stamp[:12])
	if tiles.PlanStamp(pair.warp) == reference.Stamp {
		t.Error("the warp stamps identically to a different tool, so the tool is not in the stamp")
	}
}

// TestDeriverWritesTheAlignedTiles resamples the donor through the alignment and
// compares every tile against the reference cache, byte for byte.
//
// The whole pyramid is rebuilt rather than a level of it: the deepest level is
// where the resampling happens, and every shallower one is folded down from it,
// so a reduction that changed would show only below the level the warp wrote.
func TestDeriverWritesTheAlignedTiles(t *testing.T) {
	pair := planAlignedPyramid(t)
	set, err := tiles.Open(tileIndex(t))
	if err != nil {
		t.Fatalf("tile set: %v", err)
	}
	reference, held := set.Pyramid(pair.warp.Name)
	if !held {
		t.Fatalf("the reference tile set holds no pyramid named %s", pair.warp.Name)
	}

	root := t.TempDir()
	built, err := tiles.Derive(root, pair.warp)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	if built.MaxZoom != reference.MaxZoom || built.FullZoom != reference.FullZoom ||
		built.SourceZoom != reference.SourceZoom || built.Window != reference.Window ||
		built.Interpolate != reference.Interpolate || built.Background != reference.Background ||
		built.LensName != reference.LensName || built.AlignedWith != reference.AlignedWith {
		t.Errorf("derived %+v, the reference recorded %+v", built, reference)
	}
	if strings.Join(built.Formats, ",") != strings.Join(reference.Formats, ",") {
		t.Errorf("formats %v, reference %v", built.Formats, reference.Formats)
	}
	if (built.Bounds == nil) != (reference.Bounds == nil) ||
		(built.Bounds != nil && *built.Bounds != *reference.Bounds) {
		t.Errorf("bounds %+v, reference %+v", built.Bounds, reference.Bounds)
	}
	if len(built.Coverage) != len(reference.Coverage) {
		t.Errorf("%d covered levels, reference %d", len(built.Coverage), len(reference.Coverage))
	}
	for level, mask := range reference.Coverage {
		got, covered := built.Coverage[level]
		if !covered || got == nil || *got != *mask {
			t.Errorf("level %s covers %+v, reference %+v", level, got, mask)
		}
	}

	want := hashTree(t, filepath.Join(filepath.Dir(tileIndex(t)), reference.Name))
	got := hashTree(t, filepath.Join(root, pair.warp.Name))
	if len(got) != len(want) {
		t.Fatalf("derived %d tiles, the reference cache holds %d", len(got), len(want))
	}
	differed := 0
	for name, hash := range want {
		switch {
		case got[name] == "":
			t.Errorf("%s was not derived", name)
		case got[name] != hash:
			differed++
			if differed <= 3 {
				t.Errorf("%s is %s, reference %s", name, got[name], hash)
			}
		}
	}
	if differed > 3 {
		t.Errorf("and %d more tiles differ", differed-3)
	}
	if differed == 0 {
		t.Logf("%d warped tiles rebuilt byte for byte from the archive and the fit", len(got))
	}
}

// hashTree digests every file under a directory, keyed by its path within it.
func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}
