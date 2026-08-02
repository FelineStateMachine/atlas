package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/FelineStateMachine/atlas/internal/enrich"
	enrichcuration "github.com/FelineStateMachine/atlas/internal/enrich/curation"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/compose"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func enrichCommand() command {
	return command{
		name:    "enrich",
		summary: "fold every reading of a volume together and build the richer volume",
		run:     runEnrich,
	}
}

// runEnrich is the ⊕ of generate ⊕ enrich.
//
// It translates the archive with the generate lane, adapts each reading into
// the enrich lane's model, runs the curated queue over the reading that serves,
// adapts the result back into a document, and composes it. Neither lane imports
// the other; this command holds both.
//
// A volume the queue had nothing to say about is not rebuilt. That is the
// lane's no-change-no-build law, and it is what makes running this over a whole
// archive cheap.
func runEnrich(args []string) error {
	fs := flags("enrich", "-archive DIR -tiles INDEX [-bundles DIR] [-evidence DIR] [-lenses FILE] [volume...]")
	archiveDir := fs.String("archive", "", "capture archive root (the directory holding archive.json)")
	tileIndex := fs.String("tiles", "", "derived tile set register (the tile set's index.json)")
	bundleDir := fs.String("bundles", "", "registry to install into; default is the application's own library")
	evidenceDir := fs.String("evidence", "",
		"evidence base an enricher re-runs its judgement against, as <dir>/<volume>/<name>")
	lensFile := fs.String("lenses", "", "pictures offered for a volume's grounds (see docs/enrich.md)")
	dry := fs.Bool("n", false, "enrich and stamp but write nothing")
	var log logging.Options
	log.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archiveDir == "" || *tileIndex == "" {
		fs.Usage()
		return errors.New("both -archive and -tiles are required")
	}
	logger, err := logging.Setup(log)
	if err != nil {
		return err
	}

	dir := *bundleDir
	if dir == "" {
		if dir, err = defaultRegistryDir(); err != nil {
			return err
		}
	}
	if *dry {
		dir = ""
	}

	tables, err := curation.Load()
	if err != nil {
		return err
	}
	enrichTables, err := enrichcuration.Load()
	if err != nil {
		return err
	}
	queue, err := enrich.Queue(enrichTables.Queue(), enrichers.All())
	if err != nil {
		return err
	}
	set, err := tiles.Open(*tileIndex)
	if err != nil {
		return err
	}
	store, err := archive.Open(*archiveDir)
	if err != nil {
		return err
	}
	offers, err := readLensOffers(*lensFile)
	if err != nil {
		return err
	}

	wanted := map[string]bool{}
	for _, name := range fs.Args() {
		wanted[name] = true
	}

	// Every reading of every volume, grouped by the volume they are readings of.
	readings := map[string][]doc.Document{}
	var order []string
	for _, volume := range store.Volumes() {
		source, err := sources.For(volume.Source)
		if err != nil {
			logger.Warn("volume skipped", logging.Source(volume.Source),
				logging.Path(archive.TrimRoot(store.Root(), volume.Dir())), "reason", err.Error())
			continue
		}
		document, err := source.Translate(store, volume, logger)
		if err != nil {
			if errors.Is(err, sources.ErrNotReady) {
				logger.Info("volume not ready", logging.Source(volume.Source),
					logging.Path(archive.TrimRoot(store.Root(), volume.Dir())), "reason", err.Error())
				continue
			}
			return fmt.Errorf("translate %s: %w", volume.Title, err)
		}
		slug := document.Volume.Slug
		if len(wanted) > 0 && !wanted[slug] {
			continue
		}
		if _, seen := readings[slug]; !seen {
			order = append(order, slug)
		}
		readings[slug] = append(readings[slug], document)
	}
	sort.Strings(order)

	built, unchanged := 0, 0
	for _, slug := range order {
		result, changed, err := enrichVolume(readings[slug], enrichVolumeOptions{
			Tables:    tables,
			Enrich:    enrichTables,
			Queue:     queue,
			Tiles:     set,
			BundleDir: dir,
			Evidence:  *evidenceDir,
			Offers:    offers,
			Log:       logger,
		})
		if err != nil {
			return fmt.Errorf("enrich %s: %w", slug, err)
		}
		if !changed {
			unchanged++
			logger.Info("nothing to add", logging.Op("enrich"), logging.Volume(slug))
			continue
		}
		built++
		fmt.Printf("%s\t%s\t%d worlds\t%d tiles\t%d icons%s\n",
			result.Volume, result.File, result.Worlds, result.Tiles, result.Icons,
			presence(result.Present))
	}
	if dir != "" && built > 0 {
		if err := compose.WriteRegistryIndex(dir); err != nil {
			return err
		}
	}
	logger.Info("enrich finished", logging.Op("enrich"),
		"volumes", built, "unchanged", unchanged)
	return nil
}

type enrichVolumeOptions struct {
	Tables    curation.Tables
	Enrich    enrich.Curation
	Queue     []enrich.Enricher
	Tiles     *tiles.Set
	BundleDir string
	Evidence  string
	Offers    lensOffers
	Log       *slog.Logger
}

// enrichVolume folds every reading of one volume together and composes the
// result.
func enrichVolume(readings []doc.Document, o enrichVolumeOptions) (compose.Result, bool, error) {
	volumes := make([]*enrich.Volume, 0, len(readings))
	for _, reading := range readings {
		volumes = append(volumes, volumeOf(reading, gridOf(reading, o.Tables, o.Tiles)))
	}
	serving := enrich.Serving(volumes)
	if serving < 0 {
		return compose.Result{}, false, nil
	}
	var donors []*enrich.Volume
	for index, volume := range volumes {
		if index != serving {
			donors = append(donors, volume)
		}
	}
	// Donors are folded in oldest first, so that the newest thing said about a
	// place is the last thing said about it.
	sort.SliceStable(donors, func(a, b int) bool {
		return donors[a].NewestCapture() < donors[b].NewestCapture()
	})

	volume := volumes[serving]
	result, err := enrich.Run(volume, o.Queue, enrich.Context{
		Donors:   donors,
		Evidence: evidenceFor(o.Evidence, volume.Slug),
		Lenses:   o.Offers,
		Curation: o.Enrich,
		Log:      o.Log,
	})
	if err != nil {
		return compose.Result{}, false, err
	}
	if !result.Changed {
		return compose.Result{}, false, nil
	}

	revision, err := enrich.BuildRevision(compose.PolicyRevision)
	if err != nil {
		return compose.Result{}, false, err
	}
	ledger, err := ledgerOf(volume)
	if err != nil {
		return compose.Result{}, false, err
	}
	composed, err := compose.Compose(compose.Options{
		Document:  documentOf(volume, readings[serving].Source),
		Tiles:     o.Tiles,
		Curation:  o.Tables,
		Ledger:    ledger,
		Revision:  revision,
		BundleDir: o.BundleDir,
		Log:       o.Log,
	})
	if err != nil {
		return compose.Result{}, false, err
	}
	return composed, true, nil
}

// gridOf is the window a document's worlds are cut from: the pyramid's own
// where the tile set knows one, and the corpus window otherwise. It is what
// turns the document's degrees into the world pixels a merge measures in.
func gridOf(d doc.Document, tables curation.Tables, set *tiles.Set) enrich.Grid {
	grid := enrich.Grid{
		SourceZoom: tables.Window.SourceZoom,
		FirstTile:  tables.Window.FirstTile,
		TileSize:   set.TileSize,
		Size:       set.Size,
	}
	for _, world := range d.Worlds {
		for _, lens := range world.Lenses {
			pyramid, held := set.Native(lens.TileSet)
			if !held || pyramid.Window.SourceZoom == 0 {
				continue
			}
			grid.SourceZoom = pyramid.Window.SourceZoom
			grid.FirstTile = pyramid.Window.FirstTile
			return grid
		}
	}
	return grid
}

// dirEvidence is an evidence base kept as files: <dir>/<volume>/<name>.
type dirEvidence struct{ dir string }

func evidenceFor(dir, volume string) enrich.Evidence {
	if dir == "" {
		return nil
	}
	return dirEvidence{dir: filepath.Join(dir, volume)}
}

func (e dirEvidence) Open(name string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(e.dir, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// lensOffers is the pictures offered for a volume's grounds, read from a file.
// Deriving rasters is the generate lane's work; what this file carries is the
// offer -- which pyramid pictures which ground, and the derivation stamp that
// says what it was made from.
type lensOffers struct {
	Offered map[string][]enrich.Lens `json:"offers"`
}

func (l lensOffers) Offers(world string) []enrich.Lens { return l.Offered[world] }

func readLensOffers(path string) (lensOffers, error) {
	if path == "" {
		return lensOffers{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return lensOffers{}, fmt.Errorf("read lens offers: %w", err)
	}
	var out lensOffers
	if err := json.Unmarshal(data, &out); err != nil {
		return lensOffers{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return out, nil
}
