package main

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// edgeRule decides one import edge: the importing lane, the importing
// package's module-relative path, and the imported path in. The empty string
// means the edge is permitted.
type edgeRule func(from Lane, fromRel, importPath string) string

// runImportRule adapts an edgeRule to the analysis framework. Every import
// rule shares this walk so a new boundary is a function, not a driver.
func runImportRule(rule edgeRule) func(*analysis.Pass) (any, error) {
	return func(pass *analysis.Pass) (any, error) {
		fromRel, ok := rel(packagePath(pass))
		if !ok {
			return nil, nil
		}
		from := laneOf(fromRel)
		if from == LaneOutside {
			return nil, nil
		}

		r := newReporter(pass)
		r.checkPragmas()
		for _, f := range pass.Files {
			for _, spec := range f.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				if msg := rule(from, fromRel, path); msg != "" {
					r.reportf(importPos(spec), msg)
				}
			}
		}
		return nil, nil
	}
}

// packagePath normalizes the path of a package under analysis. Test variants
// arrive as `example.com/p [example.com/p.test]`; the rules care about the
// package, not the variant.
func packagePath(pass *analysis.Pass) string {
	path := pass.Pkg.Path()
	if i := strings.Index(path, " ["); i >= 0 {
		path = path[:i]
	}
	return strings.TrimSuffix(path, "_test")
}
