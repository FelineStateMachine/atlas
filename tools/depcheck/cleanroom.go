package main

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// cleanRoom keeps the rewrite a rewrite. The pre-rewrite packages still live
// in this module — they are the oracle the goldens were captured from — and
// nothing in the new layout may quietly link against them.
var cleanRoom = &analysis.Analyzer{
	Name: "cleanroom",
	Doc: `forbid clean-room lanes from importing the golden-reference tree (issue #5 §1)

The current implementation is the golden reference, not the base. Old packages
are readable as an evolution record and importable only by golden/, which
measures them.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runImportRule(cleanRoomEdge),
}

func cleanRoomEdge(from Lane, fromRel, importPath string) string {
	switch from {
	case LaneOutside, LaneGolden:
		// golden/ captures fixtures *from* the old tree; that is its job.
		return ""
	case LaneFormat:
		// The stdlib-only rule already says something stricter and clearer.
		return ""
	}
	toRel, local := rel(importPath)
	if !local {
		return ""
	}
	if under(toRel, "render") || under(toRel, "analysis") {
		return "" // the lane matrix names these two in their own terms
	}
	if laneOf(toRel) != LaneOutside {
		return ""
	}
	return contractf(
		"clean-room lanes must not import the golden-reference tree, but this package imports "+quote(importPath),
		"1",
		"the current implementation is the golden reference, not the base: no file is carried forward except as specification or asset, and an import edge into the old tree carries the whole thing",
	)
}
