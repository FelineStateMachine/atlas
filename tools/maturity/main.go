// Command maturity reads the library and reports how mature each build of
// each volume is, along the axes internal/measure defines -- the same
// yardstick the cartograph workbench judges by, aligned with the semantic
// conventions the payloads speak: how thoroughly locations are explained in
// writing, how much visual map information the rasters hold, how much
// machine-readable structure rides beyond the pixels, and how much of the
// shared vocabulary the build actually declares.
//
// Every number is an absolute measurement of one build, never a rank within
// the collection: ranks move when the collection does, and a merge gate needs
// figures that stand still. A build that adds icons, or descriptions, or a
// deeper pyramid, reads higher than the build before it, and a build that
// lost something reads lower -- which is the judgement the library exists to
// make visible.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/measure"
)

func main() {
	bundles := flag.String("bundles", "",
		"registry directory holding .atlas bundles (default: the application's own library)")
	flag.Parse()
	if *bundles == "" {
		library, err := bundle.DefaultDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "maturity: resolving the bundle library:", err)
			os.Exit(1)
		}
		*bundles = library
	}
	if err := run(*bundles); err != nil {
		fmt.Fprintln(os.Stderr, "maturity:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "*.atlas"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no bundles in %s", dir)
	}

	byGame := make(map[string][]*measure.Build)
	for _, path := range paths {
		build, err := measure.MeasureBundle(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		byGame[build.VolumeSlug] = append(byGame[build.VolumeSlug], build)
	}

	slugs := make([]string, 0, len(byGame))
	for slug := range byGame {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		builds := byGame[slug]
		// The registry's own order, spelled once in internal/measure, so the
		// first line of every volume is the build the reader sees.
		sort.Slice(builds, func(a, b int) bool { return measure.Newer(builds[a], builds[b]) })
		fmt.Printf("%s (%s)\n", builds[0].VolumeTitle, slug)
		for at, b := range builds {
			marker := "        "
			if at == 0 {
				marker = "serving "
			}
			fmt.Printf("  %s%s\n", marker, b.File)
			fmt.Printf("    annotation   %5d pins · %d described (%s) · median %d chars\n",
				b.Pins, b.Described, b.DescribedPct(), b.MedianLength)
			fmt.Printf("    cartography  %5d tiles · %s MB unique raster · depth z%d holds %d tiles\n",
				b.TileCount, b.RasterMB(), b.Depth, b.DepthTiles)
			fmt.Printf("    structure    %5d categories in %d groups · %d text label sets · %d zones (%d vertices) · %d layers\n",
				b.Categories, b.Groups, b.TextSets, b.Zones, b.Vertices, b.Lenses)
			fmt.Printf("    icons        %5d of %d marker categories carry one (%s)\n",
				b.IconsCarried, b.IconsWanted, b.IconPct())
			fmt.Printf("    conventions  %s\n", conventionsLine(b))
			for _, account := range b.Merges {
				fmt.Printf("    merge        %s: %d donor pins · %d donor shapes · %d matched (median %dpx) · %d added (%d adopted) · %d held · %d rejected\n",
					account.Source, account.DonorPins, account.DonorShapesN(), account.MatchedN(), account.MedianMatchPx(),
					account.Added, account.AdoptedN(), account.HeldN(), account.RejectedN())
			}
		}
	}
	return nil
}

// conventionsLine says how much of the shared vocabulary a build speaks. A
// build from before the conventions says so plainly rather than scoring
// zero on a test it never sat.
func conventionsLine(b *measure.Build) string {
	if b.Conventions == 0 {
		return "none declared (pre-conventions build)"
	}
	line := fmt.Sprintf("v%d · %s of categories declare their rendering · geometry %s",
		b.Conventions, b.RenderPct(), b.Geometry)
	if b.StdIcons > 0 {
		line += fmt.Sprintf(" · %d standard icons", b.StdIcons)
	}
	if b.GeoPins > 0 {
		line += fmt.Sprintf(" · %d pins carry true coordinates", b.GeoPins)
	}
	if b.UnknownAttrs > 0 {
		line += fmt.Sprintf(" · %d attributes this build does not know", b.UnknownAttrs)
	}
	return line
}
