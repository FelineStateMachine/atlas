package main

import "strings"

// modulePath is the import path of this module. Every rule below reasons about
// package paths relative to it: an import that does not carry this prefix is
// either the standard library or a third-party dependency.
const modulePath = "github.com/FelineStateMachine/atlas"

// Lane names the architectural lane a package belongs to. Lanes are the unit
// the import matrix of issue #5 §3.2 is written in; they are a property of the
// package path, not of a bespoke directory taxonomy.
type Lane string

const (
	LaneFormat    Lane = "format"
	LaneGenerate  Lane = "generate"
	LaneEnrich    Lane = "enrich"
	LaneApp       Lane = "app"
	LaneWorkbench Lane = "workbench"
	LaneCLI       Lane = "cmd/atlas"

	// LaneTests is the out-of-package test tree: suites that must read the
	// filesystem (the corpus, the shared cell vectors, the e2e registry)
	// against packages that may not. It has the run of the lanes it judges
	// and none of the producer duties, and the rules below say so by name.
	LaneTests Lane = "tests"

	// LaneShell is the desktop shell at the module root: the Wails host of
	// issue #5 §3.4. It is a lane so that the rules which do apply to a host
	// entry — the clean-room rule, network confinement, semconv discipline —
	// apply to it, and so that the one that does not (hostenv purity, which a
	// host entry exists to violate) says so by name rather than by the shell
	// happening to sit outside the analysis.
	LaneShell Lane = "shell"

	// LaneLogging is the one shared clean-room package: the leveled event
	// stream every lane narrates itself through (issue #5 §9,
	// docs/logging.md). It is a lane of its own so that who may import it is
	// answered by the matrix rather than by where it happens to sit —
	// everybody may, except format/, which depends on the standard library
	// alone and says so in its own rule.
	LaneLogging Lane = "logging"

	// LaneOutside covers anything the module's rules do not own — tools/,
	// third-party paths, anything unheard of. Rules never fire on it: a rule
	// that is a total function of a package path needs an answer for a path
	// it has never heard of.
	LaneOutside Lane = ""
)

// cleanRoomRoots are the module-relative directories the analyzers police, in
// the order a reader of §3.1 meets them. A root that does not exist yet simply
// contributes no packages, which is how rules for unwritten lanes pass
// trivially rather than erroring.
//
// shellRoot is the module root itself and means the one package there, never
// the tree beneath it — every other root is already named on its own line, and
// a recursive pattern here would analyze each of them twice.
var cleanRoomRoots = []string{
	shellRoot,
	"format",
	"internal/logging",
	"internal/generate",
	"internal/enrich",
	"internal/app",
	"internal/workbench",
	"cmd/atlas",
	"tests",
}

// shellRoot names the module root in the vocabulary cleanRoomRoots is written
// in. The module-relative path of the root package is the empty string; "." is
// what a reader of a root list expects to see, and defaultPatterns and laneOf
// both translate.
const shellRoot = "."

// lanePrefixes maps each lane to the module-relative path that defines it.
// Order matters only for readability; laneOf matches on exact path segments.
var lanePrefixes = []struct {
	lane   Lane
	prefix string
}{
	{LaneFormat, "format"},
	{LaneLogging, "internal/logging"},
	{LaneGenerate, "internal/generate"},
	{LaneEnrich, "internal/enrich"},
	{LaneApp, "internal/app"},
	{LaneWorkbench, "internal/workbench"},
	{LaneCLI, "cmd/atlas"},
	{LaneTests, "tests"},
}

// rel returns the module-relative path of an import path, and whether the
// import is module-local at all.
func rel(importPath string) (string, bool) {
	if importPath == modulePath {
		return "", true
	}
	if r, ok := strings.CutPrefix(importPath, modulePath+"/"); ok {
		return r, true
	}
	return "", false
}

// laneOf classifies a module-relative package path.
func laneOf(relPath string) Lane {
	if relPath == "" || relPath == shellRoot {
		return LaneShell
	}
	for _, lp := range lanePrefixes {
		if relPath == lp.prefix || strings.HasPrefix(relPath, lp.prefix+"/") {
			return lp.lane
		}
	}
	return LaneOutside
}

// laneOfImport classifies an import path, reporting LaneOutside for anything
// that is not a module-local clean-room package.
func laneOfImport(importPath string) (Lane, bool) {
	r, ok := rel(importPath)
	if !ok {
		return LaneOutside, false
	}
	return laneOf(r), true
}

// isStdlib reports whether an import path names a standard-library package.
// The standard heuristic: stdlib paths have no dot in their first segment.
func isStdlib(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

// under reports whether a module-relative path sits at or below prefix.
func under(relPath, prefix string) bool {
	return relPath == prefix || strings.HasPrefix(relPath, prefix+"/")
}
