// Command measure captures the measurement fixtures: what internal/measure
// makes of each fixture bundle, structured, beside the verbatim report
// tools/maturity prints from the same numbers.
//
//	go run ./golden/capture/measure -bundles <dir> -out golden/fixtures
//
// The derived figures are captured as the tooling spells them, defects
// included. DescribedPct divides a count of text entries by a count of point
// features, and a world whose shape features carry prose therefore reads
// above 100%; that is the current behavior and the fixture carries it. What
// is wrong with a number belongs in NOTES.md, never in the number.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/golden/capture/canon"
	"github.com/FelineStateMachine/atlas/internal/measure"
)

// derived is every figure the reports compute rather than count, captured
// with the counts that produced it.
type derived struct {
	DescribedPct string `json:"describedPct"`
	IconPct      string `json:"iconPct"`
	RenderPct    string `json:"renderPct"`
	RasterMB     string `json:"rasterMB"`
	ShortStamp   string `json:"shortStamp"`
}

type buildFixture struct {
	Build   *measure.Build `json:"build"`
	Derived derived        `json:"derived"`
}

func main() {
	bundles := flag.String("bundles", "", "registry directory holding the fixture .atlas bundles")
	out := flag.String("out", "golden/fixtures", "fixtures directory to write into")
	flag.Parse()
	if *bundles == "" {
		fmt.Fprintln(os.Stderr, "measure: -bundles is required")
		os.Exit(2)
	}

	paths, err := filepath.Glob(filepath.Join(*bundles, "*.atlas"))
	if err != nil {
		fail(err)
	}
	if len(paths) == 0 {
		fail(fmt.Errorf("no bundles in %s", *bundles))
	}
	sort.Strings(paths)

	for _, path := range paths {
		build, err := measure.MeasureBundle(path)
		if err != nil {
			fail(fmt.Errorf("%s: %w", filepath.Base(path), err))
		}
		// The path is where this machine keeps the bundle, which is no part
		// of the measurement; the file name is.
		build.Path = ""
		fixture := buildFixture{
			Build: build,
			Derived: derived{
				DescribedPct: build.DescribedPct(),
				IconPct:      build.IconPct(),
				RenderPct:    build.RenderPct(),
				RasterMB:     build.RasterMB(),
				ShortStamp:   build.ShortStamp(),
			},
		}
		target := filepath.Join(*out, "measure", build.VolumeSlug+".build.json")
		if err := canon.WriteValue(target, fixture); err != nil {
			fail(err)
		}
		fmt.Printf("measure: %s -> %s\n", build.VolumeSlug, target)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "measure:", err)
	os.Exit(1)
}
