package tiles

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"sync"
)

// Every pyramid used to be derived afresh on every run: adding one volume
// re-decoded and re-reduced the other seventeen, half a minute of work to arrive
// at the bytes that were already there. A pyramid is derived from tiles stored
// under their own content hash, so what went into one can be written down and
// compared, and a pyramid whose capture has not moved is carried over from the
// last run instead.
//
// The deriving code counts as an input. Changing how a level is reduced, or
// where the content bounds land, has to invalidate every pyramid, and a stamp
// that only watched the archive would quietly keep serving the old derivation.
// That is also why a stamp is not portable between implementations: two derivers
// that write identical bytes still stamp differently, because they are different
// tools, and the stamp's promise is "nothing that made this has moved" rather
// than "these bytes are these bytes". golden/format/STAMPS.md carries what that
// costs and what is proven instead.
//
//go:embed plan.go stamp.go derive.go warp.go
var toolSource embed.FS

// ToolStamp is the hash of the deriving code itself, computed once. It is
// exported so a test can say what it is holding a plan against: the stamp
// identity a fixture records was taken with the reference implementation's
// sources, and proving the plan half of the stamp means substituting that hash
// for this one.
var ToolStamp = sync.OnceValue(func() string {
	sum := sha256.New()
	entries, err := fs.ReadDir(toolSource, ".")
	if err != nil {
		// The files are embedded at build time, so this cannot fail in a built
		// binary. Falling back to a random-looking constant would defeat the
		// point of the stamp, and rebuilding everything is the safe answer.
		return "unstamped"
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := toolSource.ReadFile(name)
		if err != nil {
			return "unstamped"
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", name, len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))
})

// PlanStamp names everything a pyramid is derived from: the tool that derives
// it, the shape the plan settled on, and the content hash of every captured tile
// that goes into it. Two runs that would write the same bytes stamp the same.
//
// The field order and the NUL framing are the contract, spelled out in
// docs/generate.md §4.2, and they are frozen: a stamp is folded into a bundle's
// stamp, so re-spelling one restamps every bundle in every library.
func PlanStamp(plan Plan) string { return StampWith(plan, ToolStamp()) }

// StampWith is PlanStamp with the tool's identity supplied rather than measured.
// It exists for one caller: the gate that proves the plan half of a stamp by
// handing it the reference implementation's tool hash and asking whether the
// recorded stamp comes back. Nothing in the lane calls it, and nothing should --
// a pyramid is stamped by the tool that derived it or not at all.
func StampWith(plan Plan, tool string) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "tool\x00%s\x00", tool)
	fmt.Fprintf(sum, "layer\x00%s\x00%s\x00", plan.TileSet, plan.Name)
	fmt.Fprintf(sum, "zooms\x00%d\x00%d\x00", plan.MaxFullZoom, plan.MaxSourceZoom)
	fmt.Fprintf(sum, "format\x00%s\x00%t\x00", plan.Format, plan.Interpolate)
	if plan.Bounds != nil {
		fmt.Fprintf(sum, "bounds\x00%d\x00%d\x00%d\x00%d\x00",
			plan.Bounds.X, plan.Bounds.Y, plan.Bounds.Width, plan.Bounds.Height)
	}
	// A warped pyramid is derived from its alignment as much as from its tiles:
	// the same donor resampled through a different transformation is a different
	// picture.
	if plan.Warp != nil {
		fmt.Fprintf(sum, "warp\x00%s\x00%d\x00%d\x00%d\x00",
			plan.Warp.Base.TileSet, plan.Warp.TargetZoom,
			plan.Warp.Base.Frame.BaseZoom, plan.Warp.Base.Frame.BaseTile)
		a := plan.Warp.Affine
		fmt.Fprintf(sum, "%.9f\x00%.9f\x00%.9f\x00%.9f\x00%.9f\x00%.9f\x00",
			a.AX, a.BX, a.CX, a.AY, a.BY, a.CY)
	}

	zooms := make([]int, 0, len(plan.Levels))
	for zoom := range plan.Levels {
		zooms = append(zooms, zoom)
	}
	sort.Ints(zooms)
	for _, zoom := range zooms {
		tiles := append([]Tile(nil), plan.Levels[zoom]...)
		// Sorted by column and then row, which is not the order a level is
		// walked in: the listing order on disk is deliberately not an input, so
		// two archives holding the same tiles stamp alike however they list them.
		sort.Slice(tiles, func(i, j int) bool {
			if tiles[i].Ref.X != tiles[j].Ref.X {
				return tiles[i].Ref.X < tiles[j].Ref.X
			}
			return tiles[i].Ref.Y < tiles[j].Ref.Y
		})
		fmt.Fprintf(sum, "level\x00%d\x00%d\x00", zoom, len(tiles))
		for _, tile := range tiles {
			fmt.Fprintf(sum, "%d\x00%d\x00%s\x00%s\x00",
				tile.Ref.X, tile.Ref.Y, tile.Ref.ContentHash, tile.Format)
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}
