package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/compose"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func composeCommand() command {
	return command{
		name:    "compose",
		summary: "build volumes from archived captures and derived tile pyramids",
		run:     runCompose,
	}
}

func runCompose(args []string) error {
	fs := flags("compose", "-archive DIR -tiles INDEX [-bundles DIR] [volume...]")
	archiveDir := fs.String("archive", "", "capture archive root (the directory holding archive.json)")
	tileIndex := fs.String("tiles", "", "derived tile set register (the tile set's index.json)")
	bundleDir := fs.String("bundles", "", "registry to install into; default is the application's own library")
	dry := fs.Bool("n", false, "compose and stamp but write nothing")
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

	tableS, err := curation.Load()
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

	wanted := map[string]bool{}
	for _, name := range fs.Args() {
		wanted[name] = true
	}

	built, skipped := 0, 0
	for _, volume := range store.Volumes() {
		source, err := sources.For(volume.Source)
		if err != nil {
			logger.Warn("volume skipped", logging.Source(volume.Source),
				logging.Path(archive.TrimRoot(store.Root(), volume.Dir())), "reason", err.Error())
			skipped++
			continue
		}
		document, err := source.Translate(store, volume, logger)
		if err != nil {
			if errors.Is(err, sources.ErrNotReady) {
				logger.Info("volume not ready", logging.Source(volume.Source),
					logging.Path(archive.TrimRoot(store.Root(), volume.Dir())), "reason", err.Error())
				skipped++
				continue
			}
			return fmt.Errorf("translate %s: %w", volume.Title, err)
		}
		if len(wanted) > 0 && !wanted[document.Volume.Slug] {
			continue
		}
		result, err := compose.Compose(compose.Options{
			Document:  document,
			Tiles:     set,
			Curation:  tableS,
			BundleDir: dir,
			Log:       logger,
		})
		if err != nil {
			return fmt.Errorf("compose %s: %w", document.Volume.Slug, err)
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
	logger.Info("compose finished", logging.Op("compose"), "volumes", built, "skipped", skipped)
	return nil
}

func presence(present bool) string {
	if present {
		return "\talready installed"
	}
	return ""
}

// defaultRegistryDir is where the application keeps its library. The path is the
// operating system's own convention for per-user configuration, under the
// application's identity, which is what lets a bundle built here be opened there
// without either side being told about the other.
func defaultRegistryDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the application library: %w", err)
	}
	return filepath.Join(base, applicationID, "bundles"), nil
}

// applicationID is the directory the application's data lives under, on every
// platform.
const applicationID = "dev.felinestatemachine.atlas"

// quote is used by the messages that name a path back to the operator.
func quote(value string) string { return `"` + strings.TrimSpace(value) + `"` }
