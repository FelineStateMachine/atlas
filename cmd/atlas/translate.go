package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func translateCommand() command {
	return command{
		name:    "translate",
		summary: "read archived captures and print the interchange document",
		run:     runTranslate,
	}
}

// runTranslate is the debugging window into the first half of the generate
// lane. It answers the question composition cannot: what did the source
// actually make of these bytes? The document goes to stdout, so it pipes into a
// diff or a JSON tool while the event stream stays on stderr.
func runTranslate(args []string) error {
	fs := flags("translate", "-archive DIR [-volume SLUG] [-artwork]")
	archiveDir := fs.String("archive", "", "capture archive root (the directory holding archive.json)")
	volume := fs.String("volume", "", "the volume to translate; default is every volume in the archive")
	artwork := fs.Bool("artwork", false, "include icon bytes; off by default because they drown the document")
	list := fs.Bool("list", false, "list the archive's volumes and their sources instead of translating")
	var log logging.Options
	log.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archiveDir == "" {
		fs.Usage()
		return errors.New("-archive is required")
	}
	logger, err := logging.Setup(log)
	if err != nil {
		return err
	}
	store, err := archive.Open(*archiveDir)
	if err != nil {
		return err
	}
	if *list {
		for _, v := range store.Volumes() {
			fmt.Printf("%s\t%s\t%s\n", v.Source, v.Title,
				archive.TrimRoot(store.Root(), v.Dir()))
		}
		return nil
	}

	printed := 0
	for _, v := range store.Volumes() {
		source, err := sources.For(v.Source)
		if err != nil {
			logger.Warn("volume skipped", logging.Source(v.Source),
				logging.Path(archive.TrimRoot(store.Root(), v.Dir())), "reason", err.Error())
			continue
		}
		document, err := source.Translate(store, v, logger)
		if err != nil {
			if errors.Is(err, sources.ErrNotReady) {
				logger.Info("volume not ready", logging.Source(v.Source), "reason", err.Error())
				continue
			}
			return fmt.Errorf("translate %s: %w", v.Title, err)
		}
		if *volume != "" && document.Volume.Slug != *volume {
			continue
		}
		if !*artwork {
			document.Icons = strip(document.Icons)
		}
		data, err := document.Marshal()
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
		printed++
	}
	if printed == 0 {
		return fmt.Errorf("no volume translated (looked for %s)", quote(*volume))
	}
	return nil
}

// strip empties the artwork but keeps the roll of it, so a document printed for
// reading still says which icons a volume carries and what they are called.
func strip(icons []doc.Icon) []doc.Icon {
	out := make([]doc.Icon, 0, len(icons))
	for _, icon := range icons {
		icon.Data = nil
		out = append(out, icon)
	}
	return out
}
