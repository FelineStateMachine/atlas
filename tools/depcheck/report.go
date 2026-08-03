package main

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Every rule's failure message names the contract it enforces and cites the
// issue section, so a violation teaches the boundary instead of merely
// blocking it (issue #5 §9). contractf is the one place that shape is built:
// "<what you did wrong> (issue #5 §<section>): <why the boundary exists>".
func contractf(violation, section, why string) string {
	return violation + " (issue #5 §" + section + "): " + why
}

// The escape hatch. A boundary crossing that is genuinely correct is annotated
// in place:
//
//	someCall() //depcheck:allow netconfine the workbench proxies its own origin check
//
// The pragma may sit on the offending line or on the line above it. It must
// name a rule (or "all") and carry a written reason — an unexplained pragma is
// itself a finding. Waivers against captured goldens live in
// golden/waivers.json; this is the source-level twin of that discipline.
const pragma = "//depcheck:allow"

// knownRules is used to reject typo'd pragmas, which would otherwise silently
// suppress nothing and read as if they did.
var knownRules = map[string]bool{
	"all":         true,
	"laneimports": true,
	"cleanroom":   true,
	"hostenv":     true,
	"netconfine":  true,
	"semconvlit":  true,
}

type allowance struct {
	rule   string
	reason string
	pos    token.Pos
}

// reporter reports diagnostics for one analysis pass, honoring pragmas.
type reporter struct {
	pass  *analysis.Pass
	allow map[int][]allowance // by line within the file set
}

func newReporter(pass *analysis.Pass) *reporter {
	r := &reporter{pass: pass, allow: make(map[int][]allowance)}
	for _, f := range pass.Files {
		for _, group := range f.Comments {
			for _, c := range group.List {
				text, ok := strings.CutPrefix(c.Text, pragma)
				if !ok {
					continue
				}
				line := pass.Fset.Position(c.Pos()).Line
				rule, reason, _ := strings.Cut(strings.TrimSpace(text), " ")
				r.allow[line] = append(r.allow[line], allowance{
					rule:   rule,
					reason: strings.TrimSpace(reason),
					pos:    c.Pos(),
				})
			}
		}
	}
	return r
}

// checkPragmas reports malformed pragmas addressed to this pass's analyzer.
// Doing it here rather than in a rule of its own keeps the pragma honest
// without a fifth analyzer whose only subject is the other four.
func (r *reporter) checkPragmas() {
	name := r.pass.Analyzer.Name
	for _, as := range r.allow {
		for _, a := range as {
			switch {
			case !knownRules[a.rule]:
				// Unknown rules are reported once, by the first analyzer, so
				// the message does not appear four times.
				if name == "laneimports" {
					r.pass.Reportf(a.pos, "%s", contractf(
						"unknown depcheck rule "+quote(a.rule)+" in a "+pragma+" pragma",
						"9",
						"a pragma that names no rule suppresses nothing while reading as if it did; the rules are laneimports, cleanroom, hostenv, netconfine, semconvlit, or all",
					))
				}
			case a.reason == "" && (a.rule == name || a.rule == "all" && name == "laneimports"):
				r.pass.Reportf(a.pos, "%s", contractf(
					pragma+" needs a written reason",
					"6",
					"every accepted divergence carries a reason a reviewer can weigh; an unexplained allowance is an edited golden by another name",
				))
			}
		}
	}
}

func (r *reporter) suppressed(pos token.Pos) bool {
	line := r.pass.Fset.Position(pos).Line
	for _, candidate := range []int{line, line - 1} {
		for _, a := range r.allow[candidate] {
			if a.reason == "" {
				continue
			}
			if a.rule == "all" || a.rule == r.pass.Analyzer.Name {
				return true
			}
		}
	}
	return false
}

func (r *reporter) reportf(pos token.Pos, msg string) {
	if r.suppressed(pos) {
		return
	}
	r.pass.Reportf(pos, "%s", msg)
}

// importPos points a diagnostic at the import spec so the fix is where the
// finger points.
func importPos(spec *ast.ImportSpec) token.Pos { return spec.Path.Pos() }

func quote(s string) string { return `"` + s + `"` }
