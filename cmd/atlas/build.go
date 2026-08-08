package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/FelineStateMachine/atlas/internal/authoring"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func buildCommand() command {
	return command{
		name:    "build",
		summary: "plan and build one .atlas-project into a native Atlas volume",
		run:     runBuild,
	}
}

func runBuild(args []string) error {
	fs := flags("build", "[-cache DIR] [-bundles DIR] [-offline] [-plan-json] FILE.atlas-project")
	cacheDir := fs.String("cache", "", "shared content-addressed source cache")
	bundleDir := fs.String("bundles", "", "Atlas library to install the immutable build into")
	offline := fs.Bool("offline", false, "use captured evidence only; perform no source requests")
	planJSON := fs.Bool("plan-json", false, "print the resolved plan without fetching or writing")
	var logOptions logging.Options
	logOptions.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("name exactly one .atlas-project manifest")
	}
	project := fs.Arg(0)
	if *cacheDir == "" {
		var err error
		if *cacheDir, err = defaultAuthoringCacheDir(); err != nil {
			return err
		}
	}
	if *bundleDir == "" {
		var err error
		if *bundleDir, err = defaultRegistryDir(); err != nil {
			return err
		}
	}
	logger, err := logging.Setup(logOptions)
	if err != nil {
		return err
	}
	result, err := authoring.Build(context.Background(), authoring.BuildOptions{
		ProjectPath: project, CacheDir: *cacheDir, LibraryDir: *bundleDir,
		Offline: *offline, PlanOnly: *planJSON,
		Event: func(event authoring.Event) {
			attrs := []any{"stage", event.Stage}
			if event.Current > 0 {
				attrs = append(attrs, "current", event.Current, "total", event.Total)
			}
			if event.Bytes > 0 {
				attrs = append(attrs, "bytes", event.Bytes)
			}
			if event.Cached {
				attrs = append(attrs, "cached", true)
			}
			if event.Artifact != "" {
				attrs = append(attrs, "artifact", event.Artifact)
			}
			logger.Info(event.Message, attrs...)
		},
	})
	if err != nil {
		return err
	}
	if *planJSON {
		data, err := json.MarshalIndent(result.Plan, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	state := "installed"
	if result.Present {
		state = "already installed"
	}
	fmt.Printf("%s\t%s\t%s\n", result.Descriptor.Slug, filepath.Base(result.Path), state)
	logger.Info("build finished", slog.String("volume", result.Descriptor.Slug), logging.Path(result.Path))
	return nil
}

func defaultAuthoringCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find the authoring cache: %w", err)
	}
	return filepath.Join(base, applicationID, "authoring"), nil
}
