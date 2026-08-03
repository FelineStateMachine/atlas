package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func tilesCommand() command {
	return command{
		name:    "tiles",
		summary: "derive raster pyramids from archived captures",
		run:     runTiles,
	}
}

func runTiles(args []string) error {
	fs := flags("tiles", "-archive DIR -output DIR [-force] [volume...]")
	archiveDir := fs.String("archive", "", "capture archive root (the directory holding archive.json)")
	output := fs.String("output", "", "tile set to write (the directory holding index.json)")
	force := fs.Bool("force", false, "derive every pyramid again, even one nothing has changed under")
	var options logging.Options
	options.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archiveDir == "" || *output == "" {
		fs.Usage()
		return errors.New("both -archive and -output are required")
	}
	log, err := logging.Setup(options)
	if err != nil {
		return err
	}

	store, err := archive.Open(*archiveDir)
	if err != nil {
		return err
	}
	tables, err := curation.Load()
	if err != nil {
		return err
	}
	// The tile set as the last run left it. A missing one is not a fault: it
	// means every pyramid is derived, which is what a first run does anyway.
	previous, _ := tiles.Open(filepath.Join(*output, tiles.IndexName))

	parent := filepath.Dir(*output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(parent, "."+filepath.Base(*output)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	register := tiles.Register{TileSize: tiles.TileSize, Size: tiles.WorldSize}
	derived := make(map[string]bool)
	carried := 0
	wanted := volumeFilter(fs.Args())

	// Deciding what every pyramid is comes before deriving any of them. Two
	// sources capturing one volume name the same ground the same thing, so the
	// names have to be settled against the whole set; and a warped variant is
	// named after the picture it aligns onto, so it can only be planned once
	// that name has stopped moving.
	var planned []reading
	var plans []tiles.Plan
	for _, ref := range store.Volumes() {
		source, err := sources.For(ref.Source)
		if err != nil {
			log.Warn("volume skipped", logging.Source(ref.Source),
				logging.Path(archive.TrimRoot(store.Root(), ref.Dir())), "reason", err.Error())
			continue
		}
		document, err := source.Translate(store, ref, log)
		if err != nil {
			if errors.Is(err, sources.ErrNotReady) {
				log.Info("volume not ready", logging.Source(ref.Source),
					logging.Path(archive.TrimRoot(store.Root(), ref.Dir())), "reason", err.Error())
				continue
			}
			return fmt.Errorf("translate %s: %w", ref.Title, err)
		}
		if len(wanted) > 0 && !wanted[document.Volume.Slug] {
			continue
		}
		worlds, err := store.Worlds(ref)
		if err != nil {
			return err
		}
		byWorld := make(map[string]archive.WorldRef, len(worlds))
		for _, world := range worlds {
			byWorld[world.Slug] = world
		}
		for _, world := range document.Worlds {
			held, known := byWorld[world.Slug]
			if !known {
				continue
			}
			captured, err := store.Tiles(held)
			if err != nil {
				if errors.Is(err, archive.ErrNotReady) {
					continue
				}
				return err
			}
			for _, lens := range world.Lenses {
				name := pyramidName(document, world, lens)
				plan, err := tiles.PlanLens(store, held, name, lens,
					captured[lens.TileSet], !tables.PixelArt(document.Volume.Slug))
				if err != nil {
					if errors.Is(err, tiles.ErrNoFrame) {
						log.Warn("picture skipped", logging.Volume(document.Volume.Slug),
							logging.World(world.Slug), logging.Lens(lens.Name), "reason", err.Error())
						continue
					}
					return err
				}
				plans = append(plans, plan)
				planned = append(planned, reading{
					volume: document.Volume.Slug,
					source: ref.Source,
					world:  world,
					frame:  plan.Frame,
				})
			}
		}
	}
	tiles.Settle(plans)
	for index := range planned {
		planned[index].plan = &plans[index]
	}
	plans = append(plans, planWarps(planned, log)...)

	for _, plan := range plans {
		stamp := tiles.PlanStamp(plan)
		if kept, ok := previous.Carry(plan, stamp); ok && !*force {
			register.Pyramids = append(register.Pyramids, kept)
			carried++
			continue
		}
		pyramid, err := tiles.Derive(temp, plan)
		if err != nil {
			return fmt.Errorf("%s: %w", plan.Name, err)
		}
		pyramid.Stamp = stamp
		register.Pyramids = append(register.Pyramids, pyramid)
		derived[plan.Name] = true
		log.Info("pyramid derived", logging.Lens(plan.Name),
			logging.Stamp(stamp[:12]), "zooms", pyramid.MaxZoom+1)
	}

	if err := tiles.Install(temp, *output, register, derived); err != nil {
		return err
	}
	log.Info("tile set written", logging.Op("tiles"), logging.Path(*output),
		"derived", len(derived), "carried", carried)
	fmt.Printf("%s\t%d derived\t%d carried\n", *output, len(derived), carried)
	return nil
}

// pyramidName is the directory a picture's pyramid lands in: the volume, the
// world, and -- where a world offers more than one picture -- which one. It is
// spelled in Atlas's own vocabulary, never the publisher's, and composition
// takes the volume prefix back off when the pyramid travels into a bundle.
func pyramidName(document doc.Document, world doc.World, lens doc.Lens) string {
	name := document.Volume.Slug + "__" + world.Slug
	if len(world.Lenses) > 1 {
		name += "__" + doc.Slugify(lens.Name)
	}
	return name
}

func volumeFilter(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[strings.TrimSpace(name)] = true
	}
	return out
}
