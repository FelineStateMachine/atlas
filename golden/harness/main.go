// Command harness runs the golden gates of issue #5 §6 in one pass.
//
// The harness is the rewrite's definition of done: format round-trips,
// generate ⊕ enrich reproduction, analysis vectors, the parity tour, HTTP
// replay, and depcheck on every lane boundary. Most of those gates have no
// lane to judge yet, so they report SKIP with the milestone they are waiting
// on. A run where every suite skips is green — the harness is a scoreboard, and
// an empty scoreboard is honest about being empty.
//
// Usage:
//
//	go run ./golden/harness              # run what is ready, skip the rest
//	go run ./golden/harness -suites=all  # attempt every suite (fails if not ready)
//	go run ./golden/harness -suites=depcheck,parity
//
// See golden/HARNESS.md for what each gate checks and how a divergence becomes
// a waiver rather than an edited golden.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A suite is one gate. The order is the order of §6: the format is checked
// before the pipeline that writes it, the pipeline before the app that serves
// it, and the boundaries last, because they are the only gate that holds when
// everything else is still empty.
type suite struct {
	name string

	// milestone and awaiting say what has to exist before this gate can judge
	// anything. They are printed on every skip, so a green run still reads as a
	// list of what is not yet proven.
	milestone string
	awaiting  string

	// entrypoint is the file the suite runs, relative to the repository root.
	// The suite that owns it lands with its milestone; the harness only needs
	// to know where to look.
	entrypoint string

	// scope is what a ready suite does *not* judge, printed under its result on
	// every run. A gate that answers for four of five fixtures is a different
	// gate from one that answers for all five, and the difference belongs on
	// the scoreboard rather than in a paragraph somebody has to go and find.
	scope string

	// argv is the command, run from the repository root.
	argv []string

	// ready marks a suite whose subject exists today.
	ready bool
}

func suites() []suite {
	return []suite{
		{
			name:       "format-roundtrip",
			milestone:  "M1",
			awaiting:   "",
			entrypoint: "golden/format/roundtrip_test.go",
			argv:       []string{"go", "test", "./golden/format/..."},
			ready:      true,
		},
		{
			name:       "generate-enrich",
			milestone:  "M2+M3",
			awaiting:   "",
			entrypoint: "golden/pipeline/reproduce_test.go",
			argv:       []string{"go", "test", "./golden/pipeline/..."},
			scope: "all six bundle fixtures: five single-source volumes " +
				"(bend-or's deepest level drawn from its own vectors) and " +
				"cyberpunk-2077 merged through the shipped command. Reproduction " +
				"needs the capture archive and the derived tile set, which are not " +
				"in git; a checkout without them skips and says so.",
			ready: true,
		},
		{
			// Captured from the current tree, and re-run against whichever
			// implementation golden/analysis/run.mjs imports. M6 re-pointed that
			// one import at analysis/cellsystems; the gate did not move.
			name:       "analysis-vectors",
			milestone:  "M0",
			awaiting:   "",
			entrypoint: "golden/analysis/run.mjs",
			argv:       []string{"node", "golden/analysis/run.mjs"},
			ready:      true,
		},
		{
			// The analysis lane's own gate, beside the vectors that judge it:
			// the TypeScript boundary rules of §9, the type checker at its
			// strictest, and the conformance suite every cell system must pass
			// (§5.4). The vectors say the lane reproduces the oracle; this says
			// the lane is the shape the architecture asked for.
			name:       "analysis-lane",
			milestone:  "M6",
			awaiting:   "",
			entrypoint: "analysis/package.json",
			argv:       []string{"npm", "run", "--silent", "lane"},
			ready:      true,
		},
		{
			// The seam's own gate, beside the analysis lane's: the same
			// TypeScript boundary rules, the type checker at its strictest,
			// the seam's unit tests against the golden fixtures -- the
			// packed layout, the tile inventories, the scene node, the
			// standing set, the grid the tour recorded -- and the authored
			// line budget of §5.5 as a warning.
			name:       "render-lane",
			milestone:  "M6",
			awaiting:   "",
			entrypoint: "render/package.json",
			argv:       []string{"npm", "run", "--silent", "seam-lane"},
			ready:      true,
		},
		{
			name:       "parity-compare",
			milestone:  "M5+M6",
			awaiting:   "the app and the seam: the tour re-pointed at the new build, diagnostics emitted jointly as a server JSON island plus seam state",
			entrypoint: "golden/parity/compare.mjs",
			argv:       []string{"node", "golden/parity/compare.mjs"},
		},
		{
			// The server's half of the joint diagnostics §6 asks for: the
			// state island held to the `session` object every parity
			// baseline records, key for key, over sequences driven through
			// the application's own routes. The other half -- the seam's
			// state, and the whole tour -- is parity-compare, which lands
			// with M6.
			name:       "island",
			milestone:  "M5",
			awaiting:   "",
			entrypoint: "golden/island/island_test.go",
			argv:       []string{"go", "test", "./golden/island/..."},
			ready:      true,
		},
		{
			name:       "http-replay",
			milestone:  "M5",
			awaiting:   "",
			entrypoint: "golden/http/replay_test.go",
			argv:       []string{"go", "test", "./golden/http/..."},
			ready:      true,
		},
		{
			name:       "depcheck",
			milestone:  "M0",
			awaiting:   "",
			entrypoint: "golden/depcheck/main.go",
			argv:       []string{"go", "run", "./golden/depcheck"},
			ready:      true,
		},
	}
}

const waiverFile = "golden/waivers.json"

// A waiver is an accepted difference from a golden. Goldens are never edited to
// match the candidate; a divergence is written down here with a reason, and
// printed on every run as a visible cost (issue #5 §6).
type waiver struct {
	ID      string `json:"id"`
	Suite   string `json:"suite"`
	Fixture string `json:"fixture"`
	Reason  string `json:"reason"`
	Added   string `json:"added"`
}

type result int

const (
	pass result = iota
	skip
	fail
)

func (r result) String() string {
	switch r {
	case pass:
		return "PASS"
	case skip:
		return "SKIP"
	default:
		return "FAIL"
	}
}

func main() {
	selected := flag.String("suites", "", `comma-separated suites to attempt beyond those ready today, or "all"`)
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}

	fmt.Printf("atlas golden harness — the gates of issue #5 §6\n\n")

	counts := map[result]int{}
	for _, s := range suites() {
		r, note := run(s, chosen(*selected, s.name))
		counts[r]++
		fmt.Printf("  %-4s  %-16s  %s\n", r, s.name, note)
		if r != skip && s.scope != "" {
			fmt.Printf("        %-16s  scope: %s\n", "", wrap(s.scope, 62, 26))
		}
	}

	waivers, err := readWaivers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nharness: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	unexplained := reportWaivers(waivers)

	fmt.Printf("\n%d suites: %d passed, %d skipped, %d failed\n",
		len(suites()), counts[pass], counts[skip], counts[fail])
	if counts[fail] > 0 || unexplained > 0 {
		os.Exit(1)
	}
}

func run(s suite, selected bool) (result, string) {
	if !s.ready && !selected {
		return skip, fmt.Sprintf("awaiting %s — %s", s.milestone, s.awaiting)
	}
	if _, err := os.Stat(s.entrypoint); errors.Is(err, fs.ErrNotExist) {
		if !s.ready {
			return fail, fmt.Sprintf("selected, but %s does not exist yet (%s)", s.entrypoint, s.milestone)
		}
		return fail, fmt.Sprintf("%s is missing", s.entrypoint)
	}

	cmd := exec.Command(s.argv[0], s.argv[1:]...)
	out, err := cmd.CombinedOutput()
	summary := lastLine(string(out))
	if err != nil {
		fmt.Printf("\n--- %s ---\n%s\n", s.name, strings.TrimRight(string(out), "\n"))
		return fail, fmt.Sprintf("%s: %v", strings.Join(s.argv, " "), err)
	}
	if summary == "" {
		summary = strings.Join(s.argv, " ")
	}
	return pass, summary
}

// chosen reads the -suites selection.
func chosen(selection, name string) bool {
	for _, want := range strings.Split(selection, ",") {
		want = strings.TrimSpace(want)
		if want == "all" || (want != "" && want == name) {
			return true
		}
	}
	return false
}

func readWaivers() ([]waiver, error) {
	data, err := os.ReadFile(waiverFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", waiverFile, err)
	}
	var waivers []waiver
	if err := json.Unmarshal(data, &waivers); err != nil {
		return nil, fmt.Errorf("parse %s: %w", waiverFile, err)
	}
	return waivers, nil
}

// reportWaivers prints the standing cost of the run and returns the number of
// entries that carry no reason. An empty file is the good case and says so in
// one line; a non-empty one is loud, because a waiver is a difference from the
// oracle that somebody decided to live with. A waiver with no written reason
// fails the run: it is an edited golden by another name.
func reportWaivers(waivers []waiver) int {
	if len(waivers) == 0 {
		fmt.Printf("waivers: none (%s is empty)\n", waiverFile)
		return 0
	}
	fmt.Printf("waivers: %d accepted divergences from the goldens (%s)\n", len(waivers), waiverFile)
	unexplained := 0
	for _, w := range waivers {
		subject := w.Suite
		if w.Fixture != "" {
			subject += "/" + w.Fixture
		}
		id := w.ID
		if id == "" {
			id = "(no id)"
		}
		if strings.TrimSpace(w.Reason) == "" {
			unexplained++
			fmt.Printf("  BAD     %-16s  %s: no reason given — a waiver without one is an edited golden by another name\n", id, subject)
			continue
		}
		fmt.Printf("  WAIVED  %-16s  %s: %s\n", id, subject, w.Reason)
	}
	return unexplained
}

// wrap folds a scope note onto lines of about width columns, indented under the
// first. A scope note is prose and gets read; a scope note printed as one
// four-hundred-column line does not.
func wrap(text string, width, indent int) string {
	var out strings.Builder
	column := 0
	for index, word := range strings.Fields(text) {
		switch {
		case index == 0:
		case column+1+len(word) > width:
			out.WriteString("\n" + strings.Repeat(" ", indent))
			column = 0
		default:
			out.WriteString(" ")
			column++
		}
		out.WriteString(word)
		column += len(word)
	}
	return out.String()
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
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
