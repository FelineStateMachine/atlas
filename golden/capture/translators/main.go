// Command translators captures what each source's translator makes of an
// archived capture. It reads the crawl archive the way tools/generate reads
// it -- the newest snapshot of a map, handed through the same chain of
// MaybeTranslate calls in the same order -- and commits the resulting
// interchange document, canonicalized.
//
//	go run ./golden/capture/translators -out golden/fixtures -private arcgis-hub
//
// The fixture pins behavior, not shape: the rewrite's interchange document
// may be spelled differently, and equivalence is judged at the composed
// bundle. What these files catch is a source's semantics quietly moving --
// an id space that stops being stable, a projection that shifts, a category
// that stops being declared.
//
// The archived capture itself is not committed. It is named by its content
// hash, which is enough to prove two runs read the same input and small
// enough to live in a fixture.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/golden/capture/canon"
	"github.com/FelineStateMachine/atlas/internal/arcgismap"
	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/ignmap"
	"github.com/FelineStateMachine/atlas/internal/pbmap"
	"github.com/FelineStateMachine/atlas/internal/trekmap"
)

// mapgenieSource is what this capture calls a game the archive records no
// source for: the archive predates the field, and everything without one came
// from MapGenie or a MapGenie-shaped site.
const mapgenieSource = "mapgenie"

type archiveFile struct {
	Games []struct {
		Directory string `json:"directory"`
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Source    string `json:"source"`
	} `json:"games"`
}

type gameFile struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Maps  []struct {
		Directory string `json:"directory"`
		ID        int64  `json:"id"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
	} `json:"maps"`
}

type snapshotRecord struct {
	CapturedAt  string `json:"capturedAt"`
	ContentHash string `json:"contentHash"`
	Kind        string `json:"kind"`
	SourceID    int64  `json:"sourceId"`
	SourceURL   string `json:"sourceUrl"`
}

// translatorFixture is the provenance beside a translated document: which
// capture went in, which translator ran, and what came out.
type translatorFixture struct {
	Source      string `json:"source"`
	Kind        string `json:"kind"`
	Passthrough bool   `json:"passthrough"`
	MapSlug     string `json:"mapSlug"`
	Capture     struct {
		CapturedAt  string `json:"capturedAt"`
		ContentHash string `json:"contentHash"`
		SourceID    int64  `json:"sourceId"`
		SourceURL   string `json:"sourceUrl"`
		Bytes       int    `json:"bytes"`
	} `json:"capture"`
	Output struct {
		Bytes  int    `json:"bytes"`
		SHA256 string `json:"sha256"`
		File   string `json:"file"`
	} `json:"output"`
	Document documentShape `json:"document"`
}

// documentShape is the translated document counted rather than quoted: what
// a reviewer wants from the fixture's header before opening the document
// itself.
type documentShape struct {
	MapID       int64  `json:"mapId"`
	MapSlug     string `json:"mapSlug"`
	MapTitle    string `json:"mapTitle"`
	GameID      int64  `json:"gameId"`
	GameSlug    string `json:"gameSlug"`
	GameTitle   string `json:"gameTitle"`
	TileSets    int    `json:"tileSets"`
	Groups      int    `json:"groups"`
	Categories  int    `json:"categories"`
	Locations   int    `json:"locations"`
	Regions     int    `json:"regions"`
	Collections int    `json:"collections"`
	Attrs       int    `json:"attrs"`
}

func main() {
	archive := flag.String("archive", "../gamemap/fmg-archive",
		"crawl archive root, the one tools/generate reads")
	out := flag.String("out", "golden/fixtures", "fixtures directory to write into")
	games := flag.String("games", "115",
		"comma-separated game ids to prefer when a source archived several (115 is TUNIC)")
	private := flag.String("private", "",
		"comma-separated source names whose fixtures go under <out>/private")
	flag.Parse()

	preferred := map[int64]bool{}
	for _, field := range strings.Split(*games, ",") {
		if field = strings.TrimSpace(field); field != "" {
			id, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				fail(err)
			}
			preferred[id] = true
		}
	}
	held := map[string]bool{}
	for _, name := range strings.Split(*private, ",") {
		if name = strings.TrimSpace(name); name != "" {
			held[name] = true
		}
	}

	var listed archiveFile
	if err := readJSON(filepath.Join(*archive, "archive.json"), &listed); err != nil {
		fail(fmt.Errorf("read archive: %w", err))
	}

	// One game per source: the preferred id when the archive holds several,
	// and otherwise the only one there is.
	chosen := map[string]string{}
	for _, game := range listed.Games {
		source := game.Source
		if source == "" {
			source = mapgenieSource
		}
		if _, taken := chosen[source]; taken && !preferred[game.ID] {
			continue
		}
		chosen[source] = game.Directory
	}

	sources := make([]string, 0, len(chosen))
	for source := range chosen {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	for _, source := range sources {
		base := filepath.Join(*out, "translators")
		if held[source] {
			base = filepath.Join(*out, "private", "translators")
		}
		written, err := capture(*archive, chosen[source], source, base)
		if err != nil {
			fmt.Fprintf(os.Stderr, "translators: %s: %v\n", source, err)
			continue
		}
		fmt.Printf("translators: %s -> %s\n", source, written)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "translators:", err)
	os.Exit(1)
}

func capture(archiveRoot, gameDirectory, source, out string) (string, error) {
	var game gameFile
	if err := readJSON(filepath.Join(archiveRoot, gameDirectory, "game.json"), &game); err != nil {
		return "", err
	}
	if len(game.Maps) == 0 {
		return "", fmt.Errorf("%s lists no maps", gameDirectory)
	}
	// The first map is the game's own first: for every source in this
	// archive there is exactly one, and where there is more than one the
	// first is the sheet the others were split from.
	chosen := game.Maps[0]

	mapDir := filepath.Join(archiveRoot, chosen.Directory)
	var snapshots []snapshotRecord
	if err := readJSON(filepath.Join(mapDir, "snapshots", "index.json"), &snapshots); err != nil {
		return "", err
	}
	if len(snapshots) == 0 {
		return "", fmt.Errorf("%s has no snapshots", chosen.Directory)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt < snapshots[j].CapturedAt })
	latest := snapshots[len(snapshots)-1]

	raw, err := os.ReadFile(filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json"))
	if err != nil {
		return "", err
	}

	// The same chain tools/generate and tools/tiles hand a snapshot through,
	// in the same order. A snapshot of another source's kind passes each one
	// untouched, so the order only matters to a reader.
	doc := raw
	if doc, err = ignmap.MaybeTranslate(latest.Kind, doc); err != nil {
		return "", err
	}
	if doc, err = pbmap.MaybeTranslate(latest.Kind, doc); err != nil {
		return "", err
	}
	if doc, err = trekmap.MaybeTranslate(latest.Kind, doc); err != nil {
		return "", err
	}
	if doc, err = arcgismap.MaybeTranslate(latest.Kind, doc); err != nil {
		return "", err
	}

	fixture := translatorFixture{
		Source:      source,
		Kind:        latest.Kind,
		Passthrough: len(doc) == len(raw) && string(doc) == string(raw),
		MapSlug:     chosen.Slug,
	}
	fixture.Capture.CapturedAt = latest.CapturedAt
	fixture.Capture.ContentHash = latest.ContentHash
	fixture.Capture.SourceID = latest.SourceID
	fixture.Capture.SourceURL = latest.SourceURL
	fixture.Capture.Bytes = len(raw)

	canonical, err := canon.Bytes(doc)
	if err != nil {
		return "", err
	}
	name := source + ".doc.json"
	fixture.Output.Bytes = len(doc)
	fixture.Output.SHA256 = bundle.HashBytes(doc)
	fixture.Output.File = name
	fixture.Document = shapeOf(doc)

	if err := canon.WriteFile(filepath.Join(out, name), canonical); err != nil {
		return "", err
	}
	if err := canon.WriteValue(filepath.Join(out, source+".fixture.json"), fixture); err != nil {
		return "", err
	}
	return filepath.Join(out, name), nil
}

// shapeOf counts the translated document without holding the pipeline's own
// decoder to this capture's needs: the fields are read loosely, because a
// fixture header that fails to write is worse than one that reads a zero.
func shapeOf(doc []byte) documentShape {
	var read struct {
		ID     int64  `json:"id"`
		Title  string `json:"title"`
		Slug   string `json:"slug"`
		Config struct {
			TileSets []json.RawMessage `json:"tile_sets"`
		} `json:"config"`
		Game struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Slug  string `json:"slug"`
		} `json:"game"`
		Groups []struct {
			Categories []struct {
				Locations []json.RawMessage `json:"locations"`
			} `json:"categories"`
		} `json:"groups"`
		Regions     []json.RawMessage `json:"regions"`
		Collections []json.RawMessage `json:"atlas_collections"`
		Attrs       map[string]string `json:"atlas_attrs"`
	}
	if err := json.Unmarshal(doc, &read); err != nil {
		return documentShape{}
	}
	shape := documentShape{
		MapID:       read.ID,
		MapSlug:     read.Slug,
		MapTitle:    read.Title,
		GameID:      read.Game.ID,
		GameSlug:    read.Game.Slug,
		GameTitle:   read.Game.Title,
		TileSets:    len(read.Config.TileSets),
		Groups:      len(read.Groups),
		Regions:     len(read.Regions),
		Collections: len(read.Collections),
		Attrs:       len(read.Attrs),
	}
	for _, group := range read.Groups {
		shape.Categories += len(group.Categories)
		for _, category := range group.Categories {
			shape.Locations += len(category.Locations)
		}
	}
	return shape
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}
