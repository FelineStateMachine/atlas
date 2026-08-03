package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// semconvLiterals keeps the conventions registry the single place an `atlas.*`
// key is spelled. Producers are strict — an unregistered key fails the build —
// and that check only means something if every writer goes through the
// registry API (issue #5 §2, §9).
var semconvLiterals = &analysis.Analyzer{
	Name: "semconvlit",
	Doc: `forbid loose atlas.* convention keys outside format/semconv (issue #5 §9)

Convention keys are written through the format/semconv registry, never as
string literals. The registry owns the stability tier, the value checker and
the prose twin; a literal elsewhere silently opts out of all three.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runSemconvLiterals,
}

// semconvRegistry is the package that may spell keys, because spelling them is
// what it is for.
const semconvRegistry = "format/semconv"

// atlasKey matches a dotted convention key: atlas.render.as, atlas.hydro.huc12.
// A bare "atlas" or a sentence containing one is not a key, and neither is the
// manifest filename "atlas.json" — every registered key has at least two
// segments after the prefix.
var atlasKey = regexp.MustCompile(`^atlas(\.[a-z0-9][a-z0-9_]*){2,}$`)

func runSemconvLiterals(pass *analysis.Pass) (any, error) {
	if testBinary(pass) {
		return nil, nil
	}
	fromRel, ok := rel(packagePath(pass))
	if !ok {
		return nil, nil
	}
	from := laneOf(fromRel)
	switch {
	case from == LaneOutside, from == LaneTests:
		// The test tree reads bundles other code produced; data says keys
		// out loud, and a reader is lenient by convention.
		return nil, nil
	case under(fromRel, semconvRegistry):
		return nil, nil
	}

	insp, _ := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if insp == nil {
		return nil, nil
	}
	r := newReporter(pass)
	insp.Preorder([]ast.Node{(*ast.BasicLit)(nil)}, func(n ast.Node) {
		lit := n.(*ast.BasicLit)
		if lit.Kind != token.STRING {
			return
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil || !atlasKey.MatchString(strings.TrimSpace(value)) {
			return
		}
		r.reportf(lit.Pos(), contractf(
			"the convention key "+quote(value)+" is written as a string literal outside "+semconvRegistry,
			"9",
			"conventions are asymmetric — producers strict, readers lenient — and the strictness lives in the registry: a literal key skips the value checker, the stability tier and the prose twin that documents it",
		))
	})
	return nil, nil
}
