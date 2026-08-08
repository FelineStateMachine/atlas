// Command corpussmoke fully validates every native .atlas file in a real
// library. Legacy files are failures after the hard cutover.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

const (
	bundlesDirEnv = "ATLAS_BUNDLES_DIR"
	appIdentifier = "dev.felinestatemachine.atlas"
)

func main() {
	bundles := flag.String("bundles", "", "the library of .atlas files (default $"+bundlesDirEnv+", else the application's own data directory)")
	flag.Parse()
	dir, err := libraryDir(*bundles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpussmoke:", err)
		os.Exit(1)
	}
	failed, err := smoke(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpussmoke:", err)
		os.Exit(1)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func smoke(dir string) (failed int, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.atlas"))
	if err != nil {
		return 0, err
	}
	if len(paths) == 0 {
		return 0, fmt.Errorf("%s holds no .atlas files", dir)
	}
	for _, path := range paths {
		if problem := check(path); problem != nil {
			failed++
			fmt.Printf("FAIL  %s\n      %v\n", filepath.Base(path), problem)
		} else {
			fmt.Printf("ok    %s\n", filepath.Base(path))
		}
	}
	fmt.Printf("%d native bundles: %d ok, %d failed\n", len(paths), len(paths)-failed, failed)
	return failed, nil
}

func check(path string) error {
	reader, err := vnext.OpenFile(path, vnext.StandardSchema())
	if err != nil {
		return err
	}
	defer reader.Close()
	return reader.Validate()
}

func libraryDir(flagged string) (string, error) {
	if flagged != "" {
		return flagged, nil
	}
	if fromEnv := os.Getenv(bundlesDirEnv); fromEnv != "" {
		return fromEnv, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no -bundles given and no config directory to fall back on: %w", err)
	}
	return filepath.Join(base, appIdentifier, "bundles"), nil
}
