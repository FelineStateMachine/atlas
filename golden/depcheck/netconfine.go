package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// netConfine is the pipeline-side face of the offline invariant: a bundle
// serves with zero network, forever, because only one package ever reaches
// outward (issue #5 §2, §9). Fetching is crawling — even the national
// enricher's evidence is captured in generate/crawl and travels in the
// archive.
var netConfine = &analysis.Analyzer{
	Name: "netconfine",
	Doc: `confine outbound HTTP to generate/crawl (issue #5 §9)

Two checks. At import level, the format and pipeline lanes may not import
net/http at all outside generate/crawl. At use level, the outbound half of
net/http — the client constructors and package-level convenience calls — is
reported anywhere outside generate/crawl, so the app and the workbench may
serve HTTP without being able to fetch it.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runNetConfine,
}

// crawlPackage is the one lane member allowed to touch the network.
const crawlPackage = "internal/generate/crawl"

// outbound names the net/http and net identifiers that reach out. The server
// half of net/http (Handler, ServeMux, Server, Error, StatusOK…) is absent by
// design: the application is an http.Handler and must stay able to say so.
var outbound = map[string]map[string]bool{
	"net/http": {
		"Get": true, "Head": true, "Post": true, "PostForm": true,
		"Client": true, "Transport": true,
		"DefaultClient": true, "DefaultTransport": true,
		"NewRequest": true, "NewRequestWithContext": true,
		"ProxyFromEnvironment": true, "ReadResponse": true,
	},
	"net": {
		"Dial": true, "DialTimeout": true, "Dialer": true,
	},
}

func runNetConfine(pass *analysis.Pass) (any, error) {
	fromRel, ok := rel(packagePath(pass))
	if !ok {
		return nil, nil
	}
	from := laneOf(fromRel)
	if from == LaneOutside || from == LaneGolden || under(fromRel, crawlPackage) {
		return nil, nil
	}

	r := newReporter(pass)

	// Import level: in the lanes that produce and read bundles, the networking
	// stack has no business being linked at all.
	if from == LaneFormat || from == LaneGenerate || from == LaneEnrich {
		for _, f := range pass.Files {
			for _, spec := range f.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil || !isHTTPStack(path) {
					continue
				}
				r.reportf(importPos(spec), contractf(
					string(from)+" must not import "+quote(path)+"; outbound HTTP lives in "+crawlPackage,
					"9",
					"offline purity is a format invariant: capture is the only network-touching step, so a policy change replays over archived captures instead of recrawling, and no payload can ever carry a runtime URL",
				))
			}
		}
	}

	// Use level: the hosts may serve HTTP; they may not fetch it.
	insp, _ := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if insp == nil {
		return nil, nil
	}
	insp.Preorder([]ast.Node{(*ast.SelectorExpr)(nil)}, func(n ast.Node) {
		node := n.(*ast.SelectorExpr)
		if isTestFile(pass, node.Pos()) {
			return // httptest wiring is not an outbound call
		}
		ident, ok := node.X.(*ast.Ident)
		if !ok {
			return
		}
		pkgName, ok := pass.TypesInfo.Uses[ident].(*types.PkgName)
		if !ok {
			return
		}
		path := pkgName.Imported().Path()
		if !outbound[path][node.Sel.Name] {
			return
		}
		r.reportf(node.Pos(), contractf(
			"outbound HTTP ("+path+"."+node.Sel.Name+") is confined to "+crawlPackage,
			"9",
			"the application serves the /data plane from bytes it already has; anything that fetches belongs to capture, where politeness, content addressing and the archive live",
		))
	})
	return nil, nil
}

func isHTTPStack(path string) bool {
	return path == "net/http" || strings.HasPrefix(path, "net/http/")
}

func isTestFile(pass *analysis.Pass, pos token.Pos) bool {
	return strings.HasSuffix(pass.Fset.Position(pos).Filename, "_test.go")
}
