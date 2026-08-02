// Command survey reads the installed .atlas registry the way the application
// does -- newest-per-slug wins -- and prints what each winning volume is made
// of: its worlds, its lenses and their shards, its merge ledgers, its
// conventions. It writes nothing. It exists so the fixture set chosen in
// golden/fixtures/FIXTURES.json can be justified from evidence rather than
// from memory, and so a later re-capture can check that the classifications
// still hold.
//
//	go run ./golden/capture/survey            # every installed volume
//	go run ./golden/capture/survey -json      # machine-readable
//	go run ./golden/capture/survey tunic mars # only these slugs
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

// worldPeek is the sliver of a world payload the survey reads. It is
// deliberately loose: the survey classifies, it does not validate.
type worldPeek struct {
	Grid *struct {
		SourceZoom int `json:"sourceZoom"`
		FirstTile  int `json:"firstTile"`
	} `json:"grid,omitempty"`
	Lenses []struct {
		Name     string `json:"name"`
		Tiles    string `json:"tiles"`
		MinZoom  int    `json:"minZoom"`
		MaxZoom  int    `json:"maxZoom"`
		FullZoom int    `json:"fullZoom"`
		Shard    int64  `json:"shard,omitempty"`
	} `json:"lenses"`
	Collections []struct {
		Title    string            `json:"title"`
		Kind     string            `json:"kind"`
		Attrs    map[string]string `json:"attrs,omitempty"`
		Features []json.RawMessage `json:"features,omitempty"`
	} `json:"collections"`
	Attrs  map[string]string `json:"attrs,omitempty"`
	Merged []struct {
		Source string `json:"source"`
		Slug   string `json:"slug,omitempty"`
		Origin bool   `json:"origin,omitempty"`
		Added  int    `json:"added"`
	} `json:"merged,omitempty"`
}

type worldReport struct {
	Slug        string            `json:"slug"`
	Title       string            `json:"title"`
	Parent      string            `json:"parent,omitempty"`
	Points      int               `json:"points"`
	Paths       int               `json:"paths"`
	Areas       int               `json:"areas"`
	Lenses      []string          `json:"lenses"`
	Shards      []int64           `json:"shards,omitempty"`
	Collections int               `json:"collections"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	MergedFrom  []string          `json:"mergedFrom,omitempty"`
}

type volumeReport struct {
	Slug        string        `json:"slug"`
	Title       string        `json:"title"`
	Path        string        `json:"path"`
	Stamp       string        `json:"stamp"`
	Stamp12     string        `json:"stamp12"`
	CreatedAt   string        `json:"createdAt"`
	Revision    int           `json:"revision"`
	Conventions int           `json:"conventions"`
	SizeBytes   int64         `json:"sizeBytes"`
	Entries     int           `json:"entries"`
	Icons       int           `json:"icons"`
	TileCount   int           `json:"tileCount"`
	Pyramids    []string      `json:"pyramids"`
	Traits      []string      `json:"traits"`
	Worlds      []worldReport `json:"worlds"`
}

func main() {
	dir := flag.String("dir", "", "registry directory (default: the application's own library)")
	asJSON := flag.Bool("json", false, "print the survey as JSON")
	asPaths := flag.Bool("paths", false,
		"print only the file serving each named slug, one path per line")
	flag.Parse()

	root := *dir
	if root == "" {
		resolved, err := bundle.DefaultDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "survey:", err)
			os.Exit(1)
		}
		root = resolved
	}
	registry := bundle.NewRegistry(root)
	if err := registry.Rescan(); err != nil {
		fmt.Fprintln(os.Stderr, "survey:", err)
		os.Exit(1)
	}
	winners := registry.Snapshot().Volumes

	wanted := map[string]bool{}
	for _, slug := range flag.Args() {
		wanted[slug] = true
	}

	slugs := make([]string, 0, len(winners))
	for slug := range winners {
		if len(wanted) == 0 || wanted[slug] {
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)

	// The one answer a script needs: which file the registry's fold picked,
	// so a curated fixture registry can be assembled without a human copying
	// stamps from one window into another.
	if *asPaths {
		for _, slug := range slugs {
			fmt.Println(winners[slug].Path)
		}
		if len(slugs) != len(wanted) && len(wanted) > 0 {
			os.Exit(1)
		}
		return
	}

	reports := make([]volumeReport, 0, len(slugs))
	for _, slug := range slugs {
		report, err := survey(winners[slug])
		if err != nil {
			fmt.Fprintf(os.Stderr, "survey %s: %v\n", slug, err)
			continue
		}
		reports = append(reports, report)
	}

	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(reports); err != nil {
			fmt.Fprintln(os.Stderr, "survey:", err)
			os.Exit(1)
		}
		return
	}
	for _, report := range reports {
		print(report)
	}
}

func survey(held *bundle.Bundle) (volumeReport, error) {
	manifest := held.Manifest
	report := volumeReport{
		Slug:        manifest.Volume.Slug,
		Title:       manifest.Volume.Title,
		Path:        held.Path,
		Stamp:       manifest.Version.Stamp,
		Stamp12:     bundle.ShortStamp(manifest.Version.Stamp),
		CreatedAt:   manifest.Version.CreatedAt,
		Revision:    manifest.Version.Revision,
		Conventions: manifest.Conventions,
	}
	if info, err := os.Stat(held.Path); err == nil {
		report.SizeBytes = info.Size()
	}
	pyramids := map[string]bool{}
	for _, name := range entryNames(held) {
		report.Entries++
		switch {
		case strings.HasPrefix(name, "icons/"):
			report.Icons++
		case strings.HasPrefix(name, "tiles/"):
			report.TileCount++
			if rest := strings.TrimPrefix(name, "tiles/"); strings.Contains(rest, "/") {
				pyramids[rest[:strings.IndexByte(rest, '/')]] = true
			}
		}
	}
	for pyramid := range pyramids {
		report.Pyramids = append(report.Pyramids, pyramid)
	}
	sort.Strings(report.Pyramids)

	var sharded, merged, split, sphere bool
	for _, entry := range manifest.Worlds {
		raw, err := held.ReadEntry("worlds/" + entry.Slug + ".json")
		if err != nil {
			return report, err
		}
		var peek worldPeek
		if err := json.Unmarshal(raw, &peek); err != nil {
			return report, fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		world := worldReport{
			Slug:        entry.Slug,
			Title:       entry.Title,
			Parent:      entry.Parent,
			Points:      entry.Points,
			Paths:       entry.Paths,
			Areas:       entry.Areas,
			Collections: len(peek.Collections),
			Attrs:       peek.Attrs,
		}
		for _, lens := range peek.Lenses {
			world.Lenses = append(world.Lenses, lens.Name+"="+lens.Tiles)
			if lens.Shard != 0 {
				world.Shards = append(world.Shards, lens.Shard)
				sharded = true
			}
		}
		for _, source := range peek.Merged {
			mark := source.Source
			if source.Origin {
				mark += " (origin)"
			} else {
				merged = true
			}
			world.MergedFrom = append(world.MergedFrom, mark)
		}
		if entry.Parent != "" {
			split = true
		}
		if peek.Attrs["atlas.geometry.surface"] == "sphere" {
			sphere = true
		}
		report.Worlds = append(report.Worlds, world)
	}
	if sharded {
		report.Traits = append(report.Traits, "lens-sharded")
	}
	if merged {
		report.Traits = append(report.Traits, "multi-source-merge")
	}
	if split {
		report.Traits = append(report.Traits, "split-sheet")
	}
	if sphere {
		report.Traits = append(report.Traits, "sphere")
	}
	if len(report.Traits) == 0 {
		report.Traits = append(report.Traits, "plain")
	}
	return report, nil
}

// entryNames lists the archive's entries. The bundle reader keeps its entry
// map private, and the golden capture may not widen the reference tree's
// surface to suit itself, so the survey opens the zip a second time for this
// one diagnostic.
func entryNames(held *bundle.Bundle) []string {
	archive, err := zip.OpenReader(held.Path)
	if err != nil {
		return nil
	}
	defer archive.Close()
	names := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	return names
}

func print(report volumeReport) {
	fmt.Printf("%s  %q\n", report.Slug, report.Title)
	fmt.Printf("  traits      %s\n", strings.Join(report.Traits, ", "))
	fmt.Printf("  file        %s (%.1f MiB)\n", report.Path, float64(report.SizeBytes)/(1<<20))
	fmt.Printf("  version     stamp=%s created=%s revision=%d conventions=%d\n",
		report.Stamp12, report.CreatedAt, report.Revision, report.Conventions)
	fmt.Printf("  archive     %d entries, %d tiles, %d icons, pyramids %s\n",
		report.Entries, report.TileCount, report.Icons, strings.Join(report.Pyramids, " "))
	for _, world := range report.Worlds {
		fmt.Printf("  world %-28s pts=%-6d paths=%-4d areas=%-4d colls=%-4d lenses=%d%s\n",
			world.Slug, world.Points, world.Paths, world.Areas, world.Collections,
			len(world.Lenses), parentNote(world.Parent))
		if len(world.Shards) > 0 {
			fmt.Printf("      shards  %v\n", world.Shards)
		}
		if len(world.MergedFrom) > 0 {
			fmt.Printf("      merged  %s\n", strings.Join(world.MergedFrom, ", "))
		}
	}
	fmt.Println()
}

func parentNote(parent string) string {
	if parent == "" {
		return ""
	}
	return " parent=" + parent
}
