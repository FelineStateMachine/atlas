// Command maturity reads the library and reports how mature each build of
// each game is, along the three axes that judge a game's evolution as its
// sources mix: how thoroughly its locations are explained in writing, how
// much visual map information its rasters hold, and how much machine-readable
// structure it carries beyond the pixels -- with the icons its legend draws
// counted alongside.
//
// Every number is an absolute measurement of one build, never a rank within
// the collection: ranks move when the collection does, and a merge gate needs
// figures that stand still. A build that adds icons, or descriptions, or a
// deeper pyramid, reads higher than the build before it, and a build that
// lost something reads lower -- which is the judgement the library exists to
// make visible.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

type mapDetail struct {
	Groups []struct {
		Categories []struct {
			IconAsset   string `json:"iconAsset"`
			DisplayType string `json:"displayType"`
			Locations   []struct {
				ID int64 `json:"id"`
			} `json:"locations"`
		} `json:"categories"`
	} `json:"groups"`
	Zones []struct {
		Features []struct {
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"features"`
	} `json:"zones"`
	Variants []struct {
		Name    string `json:"name"`
		MaxZoom int    `json:"maxZoom"`
	} `json:"variants"`
}

// measure is one build's absolute account of itself.
type measure struct {
	File      string
	Stamp     string
	CreatedAt string

	Pins         int
	Described    int
	MedianLength int
	TileCount    int
	RasterBytes  int64
	Depth        int
	DepthTiles   int
	Categories   int
	TextSets     int
	Groups       int
	Zones        int
	Vertices     int
	Variants     int
	IconsCarried int
	IconsWanted  int
}

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

	byGame := make(map[string][]measure)
	titles := make(map[string]string)
	for _, path := range paths {
		slug, title, m, err := measureBundle(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		byGame[slug] = append(byGame[slug], m)
		titles[slug] = title
	}

	slugs := make([]string, 0, len(byGame))
	for slug := range byGame {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		builds := byGame[slug]
		// The registry's own order: newest capture first, then stamp, so the
		// first line of every game is the build the reader sees.
		sort.Slice(builds, func(a, b int) bool {
			if builds[a].CreatedAt != builds[b].CreatedAt {
				return builds[a].CreatedAt > builds[b].CreatedAt
			}
			return builds[a].Stamp > builds[b].Stamp
		})
		fmt.Printf("%s (%s)\n", titles[slug], slug)
		for at, m := range builds {
			marker := "        "
			if at == 0 {
				marker = "serving "
			}
			fmt.Printf("  %s%s\n", marker, m.File)
			fmt.Printf("    annotation   %5d pins · %d described (%s) · median %d chars\n",
				m.Pins, m.Described, percent(m.Described, m.Pins), m.MedianLength)
			fmt.Printf("    cartography  %5d tiles · %.1f MB unique raster · depth z%d holds %d tiles\n",
				m.TileCount, float64(m.RasterBytes)/1e6, m.Depth, m.DepthTiles)
			fmt.Printf("    structure    %5d categories in %d groups · %d text label sets · %d zones (%d vertices) · %d layers\n",
				m.Categories, m.Groups, m.TextSets, m.Zones, m.Vertices, m.Variants)
			fmt.Printf("    icons        %5d of %d marker categories carry one (%s)\n",
				m.IconsCarried, m.IconsWanted, percent(m.IconsCarried, m.IconsWanted))
		}
	}
	return nil
}

func percent(part, whole int) string {
	if whole == 0 {
		return "—"
	}
	return strconv.Itoa(part*100/whole) + "%"
}

func measureBundle(path string) (string, string, measure, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", "", measure{}, err
	}
	defer reader.Close()

	entries := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		entries[file.Name] = file
	}
	var manifest bundle.Manifest
	if err := readEntry(entries, "atlas.json", &manifest); err != nil {
		return "", "", measure{}, err
	}

	m := measure{
		File:      filepath.Base(path),
		Stamp:     manifest.Version.Stamp,
		CreatedAt: manifest.Version.CreatedAt,
	}

	// Annotation: how thoroughly the locations are explained in writing.
	var lengths []int
	for _, entry := range manifest.Maps {
		m.Pins += entry.PinCount
		var text map[string]struct {
			Description string `json:"d"`
		}
		if err := readEntry(entries, "maps/"+entry.Slug+".text", &text); err != nil {
			return "", "", measure{}, err
		}
		for _, held := range text {
			cleaned := strings.Join(strings.Fields(held.Description), " ")
			if cleaned == "" {
				continue
			}
			m.Described++
			lengths = append(lengths, len(cleaned))
		}

		var detail mapDetail
		if err := readEntry(entries, "maps/"+entry.Slug+".json", &detail); err != nil {
			return "", "", measure{}, err
		}
		m.Variants += len(detail.Variants)
		for _, variant := range detail.Variants {
			m.Depth = max(m.Depth, variant.MaxZoom)
		}
		m.Zones += len(detail.Zones)
		for _, zone := range detail.Zones {
			for _, feature := range zone.Features {
				m.Vertices += countVertices(feature.Coordinates)
			}
		}
		for _, group := range detail.Groups {
			m.Groups++
			for _, category := range group.Categories {
				m.Categories++
				if category.DisplayType == "text" {
					m.TextSets++
					continue
				}
				m.IconsWanted++
				if category.IconAsset != "" {
					m.IconsCarried++
				}
			}
		}
	}
	sort.Ints(lengths)
	if len(lengths) > 0 {
		m.MedianLength = lengths[len(lengths)/2]
	}

	// Cartography: the stored representation, de-duplicated -- a border of
	// repeated filler tiles is one tile's worth of information however many
	// times it appears.
	seen := make(map[[2]uint64]bool)
	for name, file := range entries {
		if !strings.HasPrefix(name, "tiles/") {
			continue
		}
		m.TileCount++
		key := [2]uint64{uint64(file.CRC32), file.UncompressedSize64}
		if !seen[key] {
			seen[key] = true
			m.RasterBytes += int64(file.UncompressedSize64)
		}
		if zoom := tileZoom(name); zoom == m.Depth {
			m.DepthTiles++
		}
	}

	return manifest.Game.Slug, manifest.Game.Title, m, nil
}

func readEntry(entries map[string]*zip.File, name string, into any) error {
	file, ok := entries[name]
	if !ok {
		return fmt.Errorf("%s is missing", name)
	}
	opened, err := file.Open()
	if err != nil {
		return err
	}
	defer opened.Close()
	return json.NewDecoder(opened).Decode(into)
}

// tileZoom reads the level out of tiles/<pyramid>/<zoom>/<x>/<y>.<ext>.
func tileZoom(name string) int {
	parts := strings.Split(name, "/")
	if len(parts) < 5 {
		return -1
	}
	zoom, err := strconv.Atoi(parts[len(parts)-3])
	if err != nil {
		return -1
	}
	return zoom
}

// countVertices counts coordinate pairs through however many rings and
// nestings a geometry holds.
func countVertices(raw json.RawMessage) int {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0
	}
	numbers := 0
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case float64:
			numbers++
		}
	}
	walk(value)
	return numbers / 2
}
