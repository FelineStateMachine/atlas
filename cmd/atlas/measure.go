package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/enrich/maturity"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func measureCommand() command {
	return command{
		name:    "measure",
		summary: "score every build in a registry and report what each one is made of",
		run:     runMeasure,
	}
}

// runMeasure reports the feature-maturity score of every build, with the five
// absolute axes as diagnostics beneath it and every merge ledger the payloads
// carry.
//
// Every number is an absolute measurement of one build, never a rank within the
// library: ranks move when the library does, and a gate needs figures that
// stand still.
func runMeasure(args []string) error {
	fs := flags("measure", "[-bundles DIR] [-json] [volume...]")
	bundleDir := fs.String("bundles", "", "registry to read; default is the application's own library")
	asJSON := fs.Bool("json", false, "write the scores as JSON instead of a report")
	var log logging.Options
	log.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := logging.Setup(log); err != nil {
		return err
	}

	dir := *bundleDir
	var err error
	if dir == "" {
		if dir, err = defaultRegistryDir(); err != nil {
			return err
		}
	}
	table, err := maturity.Points()
	if err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*"+bundle.Extension))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no bundles in %s", quote(dir))
	}

	wanted := map[string]bool{}
	for _, name := range fs.Args() {
		wanted[name] = true
	}

	byVolume := map[string][]*maturity.Score{}
	var slugs []string
	for _, path := range paths {
		score, err := maturity.Measure(path, table)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if len(wanted) > 0 && !wanted[score.Volume] {
			continue
		}
		if _, seen := byVolume[score.Volume]; !seen {
			slugs = append(slugs, score.Volume)
		}
		byVolume[score.Volume] = append(byVolume[score.Volume], score)
	}
	sort.Strings(slugs)

	if *asJSON {
		out := make([]*maturity.Score, 0, len(paths))
		for _, slug := range slugs {
			out = append(out, byVolume[slug]...)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(out)
	}

	for _, slug := range slugs {
		builds := byVolume[slug]
		serving := maturity.Serving(builds)
		sort.SliceStable(builds, func(a, b int) bool { return builds[a].Total > builds[b].Total })
		fmt.Printf("%s (%s) — point table v%d\n", builds[0].Title, slug, table.Version)
		for _, build := range builds {
			report(build, build == serving)
		}
	}
	return nil
}

func report(b *maturity.Score, serving bool) {
	marker := "        "
	if serving {
		marker = "serving "
	}
	enriched := ""
	if b.Enriched {
		enriched = fmt.Sprintf(" · enriched (policy %d)", b.EnrichPolicy)
	}
	fmt.Printf("  %s%s\n", marker, b.File)
	fmt.Printf("    maturity     %5d points · revision %d%s\n", b.Total, b.Revision, enriched)
	for _, world := range b.Worlds {
		fmt.Printf("      %-24s %5d = %d features + %d collections + %d world\n",
			world.Slug, world.Total, world.Features, world.Collections, world.World)
	}
	a := b.Axes
	fmt.Printf("    annotation   %5d points · %d described (%s) · median %d chars\n",
		a.Points, a.Described, a.DescribedShare(), a.MedianLength)
	fmt.Printf("    cartography  %5d tiles · %s MB unique raster · depth z%d holds %d tiles\n",
		a.TileCount, a.RasterMB(), a.Depth, a.DepthTiles)
	fmt.Printf("    structure    %5d collections in %d groups · %d text label sets · %d shapes (%d vertices) · %d lenses\n",
		a.Collections, a.Groups, a.TextSets, a.Shapes, a.Vertices, a.Lenses)
	fmt.Printf("    icons        %5d of %d marker collections carry one (%s)\n",
		a.IconsCarried, a.IconsWanted, a.IconShare())
	fmt.Printf("    conventions  %s\n", conventions(a))
	for _, line := range b.Ledger {
		if line.Account.Origin {
			fmt.Printf("    origin       %s: %s holds %d points, %d paths, %d areas\n",
				line.World, line.Account.Source, line.Account.DonorFeatures.Point,
				line.Account.DonorFeatures.Path, line.Account.DonorFeatures.Area)
			continue
		}
		fmt.Printf("    merge        %s: %s: %d offered · %d matched (median %dpx, %d enriched) · %d added (%d adopted) · %d held · %d rejected\n",
			line.World, line.Account.Source, line.Account.DonorFeatures.Total(),
			line.Account.MatchedN(), line.Account.MedianMatchPx(), line.Account.EnrichedN(),
			line.Account.Added, line.Account.AdoptedN(), line.Account.HeldN(), line.Account.RejectedN())
	}
}

// conventions says how much of the shared vocabulary a build speaks. A build
// from before the conventions says so plainly rather than scoring zero on a
// test it never sat.
func conventions(a maturity.Axes) string {
	if a.Conventions == 0 {
		return "none declared (pre-conventions build)"
	}
	line := fmt.Sprintf("v%d · %s of collections declare their rendering · geometry %s",
		a.Conventions, a.RenderShare(), a.Geometry)
	if a.StdIcons > 0 {
		line += fmt.Sprintf(" · %d standard icons", a.StdIcons)
	}
	if a.GeoFeatures > 0 {
		line += fmt.Sprintf(" · %d features carry true coordinates", a.GeoFeatures)
	}
	if a.Memberships > 0 {
		line += fmt.Sprintf(" · %d memberships joined", a.Memberships)
	}
	if a.UnknownAttrs > 0 {
		line += fmt.Sprintf(" · %d attributes this build does not know", a.UnknownAttrs)
	}
	return line
}
