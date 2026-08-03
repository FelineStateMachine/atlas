// Command depcheck enforces the architecture of issue #5 as static analysis.
//
// The pseudo-contracts in the issue are encoded as analyzers on the
// golang.org/x/tools/go/analysis framework — the machinery go vet is built on
// — so a crossed boundary is an immediate mechanical "we don't do that here"
// rather than a convention someone has to remember (issue #5 §9).
//
// Usage:
//
//	go run ./tools/depcheck                 # every clean-room package that exists
//	go run ./tools/depcheck ./format/...    # an explicit pattern
//	go run ./tools/depcheck help laneimports
//
// With no package pattern, depcheck scopes itself to the clean-room roots of
// §3.1 that exist on disk. That is deliberate: the pre-rewrite tree (archived
// on the golden-reference tag) is not the subject, and a rule about a lane
// nobody has written yet passes by having nothing to say.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"
)

// analyzers is the guardrail set. Milestones extend it: rules land with the
// contracts they enforce (issue #5 §9).
func analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		laneImports,
		cleanRoom,
		hostenvPurity,
		netConfine,
		semconvLiterals,
	}
}

func main() {
	if hasPatterns(os.Args[1:]) {
		multichecker.Main(analyzers()...)
		return
	}

	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "depcheck:", err)
		os.Exit(1)
	}
	patterns := defaultPatterns(root)
	if len(patterns) == 0 {
		fmt.Fprintf(os.Stderr, "depcheck: %d rules armed; no clean-room package exists yet (roots: %s)\n",
			len(analyzers()), strings.Join(cleanRoomRoots, " "))
		return
	}
	fmt.Fprintf(os.Stderr, "depcheck: %d rules over %s\n", len(analyzers()), strings.Join(patterns, " "))

	os.Args = append(os.Args[:1:1], append(flagsOf(os.Args[1:]), patterns...)...)
	multichecker.Main(analyzers()...)
}

// hasPatterns reports whether the caller named packages itself. Analysis flags
// are all of the -flag=value form, so a bare argument is a pattern (or the
// "help" subcommand, which multichecker handles).
func hasPatterns(args []string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return true
		}
	}
	return false
}

func flagsOf(args []string) []string {
	flags := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		}
	}
	return flags
}

// defaultPatterns turns the clean-room roots into package patterns, skipping
// roots that do not exist yet. Scoping this way — rather than running ./... —
// is what keeps the old tree out of the analysis entirely: it is neither
// loaded nor judged.
func defaultPatterns(root string) []string {
	var patterns []string
	for _, dir := range cleanRoomRoots {
		// The module root is the desktop shell's one package, not the tree
		// beneath it: every other root is named on its own line below.
		if dir == shellRoot {
			patterns = append(patterns, modulePath)
			continue
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir))); err == nil && info.IsDir() {
			patterns = append(patterns, modulePath+"/"+dir+"/...")
		}
	}
	return patterns
}

// moduleRoot walks up from the working directory to the go.mod, so depcheck
// behaves the same from the repository root and from a lane directory.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate module root: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
