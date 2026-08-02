package main

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// laneImports encodes the dependency rules of issue #5 §3.2 as import edges.
// This analyzer is what depcheck *is*: lane isolation is a property of who
// imports whom, not of a directory taxonomy.
var laneImports = &analysis.Analyzer{
	Name: "laneimports",
	Doc: `check the lane import matrix of issue #5 §3.2

format/ depends on the standard library only. generate/ and enrich/ depend on
format/ and never on each other, app/, workbench/, render/, or analysis/.
app/ depends on format/ and its own packages. workbench/ depends on format/
plus enrich/maturity. Nothing imports render/.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runImportRule(laneImportEdge),
}

// laneImportEdge is the whole rule as a pure function of two package paths, so
// the matrix can be table-tested without a compiler. It returns the empty
// string when the edge is permitted.
func laneImportEdge(from Lane, fromRel, importPath string) string {
	if from == LaneOutside {
		return "" // the golden-reference tree is the oracle, not the subject
	}

	toRel, local := rel(importPath)
	if !local {
		if from == LaneFormat && !isStdlib(importPath) {
			return contractf(
				"format must depend on the Go standard library only, but imports "+quote(importPath),
				"3.2",
				"format/ is exported at the module root on purpose: a third party who knows nothing of Atlas the app must be able to read a bundle with no dependency but Go itself",
			)
		}
		return ""
	}

	// The two TypeScript lanes are unimportable from Go by construction; say so
	// in the terms the contract uses rather than letting them fall through to a
	// generic message.
	switch {
	case under(toRel, "render"):
		return contractf(
			"nothing imports render, but this package imports "+quote(importPath),
			"3.2",
			"the seam is deletable: it stands on the /data plane, the scene description and the analysis API, and nothing stands on it",
		)
	case under(toRel, "analysis"):
		return contractf(
			"analysis/ is a TypeScript lane and is not importable from Go, but this package imports "+quote(importPath),
			"3.1",
			"the analysis lane ships as a TypeScript package that Go tooling ignores; its contract crosses into Go only through published artifacts",
		)
	}

	to := laneOf(toRel)
	if to == LaneOutside {
		return "" // the clean-room analyzer owns imports of the old tree
	}
	if from == LaneFormat && to != LaneFormat {
		return contractf(
			"format must depend on the Go standard library only, but imports "+quote(importPath),
			"3.2",
			"format/ is the center: everything may depend on it and it may depend on nothing of ours",
		)
	}
	if to == from {
		return ""
	}
	if to == LaneFormat {
		return "" // every lane may depend on the center
	}
	if to == LaneLogging {
		// One leveled event stream narrates the whole system (§9), so every
		// lane may speak it. format/ never reaches this line: its
		// stdlib-only rule above has already answered.
		return ""
	}

	switch from {
	case LaneGenerate, LaneEnrich:
		other := LaneEnrich
		if from == LaneEnrich {
			other = LaneGenerate
		}
		if to == other {
			return contractf(
				string(from)+" must not import "+string(other)+", but imports "+quote(importPath),
				"3.2",
				"the pipeline lanes are isolated — the composed multi-source result is generate ⊕ enrich, joined by the pipeline and checked at the bundle level by the goldens, never by an import edge",
			)
		}
		return contractf(
			string(from)+" must not import "+string(to)+", but imports "+quote(importPath),
			"3.2",
			"each pipeline lane is a library plus CLI subcommands of one atlas binary; the application and the harness depend on the lanes, never the reverse",
		)

	case LaneApp:
		return contractf(
			"app must not import "+string(to)+", but imports "+quote(importPath),
			"3.2",
			"the application depends on format/ and its own packages; pipeline work is reached through the lane CLIs, and the harness is the oracle around the code, not a dependency of it",
		)

	case LaneWorkbench:
		if to == LaneEnrich {
			if under(toRel, "internal/enrich/maturity") {
				return ""
			}
			return contractf(
				"workbench may import only enrich/maturity from the enrich lane, but imports "+quote(importPath),
				"3.2",
				"the workbench reads scores and shells out to the lane CLIs for everything else, which is what keeps pipeline operations subprocesses it can stream rather than code it links",
			)
		}
		return contractf(
			"workbench must not import "+string(to)+", but imports "+quote(importPath),
			"3.2",
			"the workbench depends on format/ plus enrich/maturity and shells out to the lane CLIs",
		)

	case LaneLogging:
		return contractf(
			"logging must not import "+string(to)+", but imports "+quote(importPath),
			"9",
			"the event stream is the one thing every lane may depend on, which it can only stay if it depends on nothing of ours in return",
		)

	case LaneCLI:
		return "" // the one binary is allowed to wire every lane's subcommands

	case LaneGolden:
		return "" // the harness may read anything it measures
	}
	return ""
}
