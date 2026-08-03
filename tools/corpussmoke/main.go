// Command corpussmoke walks a real library of .atlas files and reports which
// of them parse and validate.
//
// It is deliberately NOT a CI gate and never will be: it reads an installed
// library — the maintainer's, typically tens of gigabytes — that no fresh
// checkout has. CI's rule is that every required test's inputs are committed
// or built by the run; this command is the other half of the bargain, the
// deep check a maintainer runs by hand over data that cannot be committed.
//
// It asserts invariants only: the container opens, the manifest re-reads,
// validation passes, every world's packed locations unpack. It compares no
// stamps, no hashes and no content — the days of holding the tree to captured
// bytes from a private library are over.
//
// Usage:
//
//	atlas-corpussmoke [-bundles dir]
//	make corpus-smoke
//
// The library is the -bundles flag, else $ATLAS_BUNDLES_DIR, else the
// application's own data directory — the same resolution `atlas serve` uses.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FelineStateMachine/atlas/format/bundle"
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
	older := 0
	for _, path := range paths {
		problem := check(path)
		switch {
		case problem == nil:
			fmt.Printf("ok    %s\n", filepath.Base(path))
		case isOlderFormat(path):
			// A library accretes: builds from before this format version sit
			// beside current ones, and a v%d reader refusing them is the
			// reader doing its job, not a finding about this tree.
			older++
		default:
			failed++
			fmt.Printf("FAIL  %s\n      %v\n", filepath.Base(path), problem)
		}
	}
	fmt.Printf("%d bundles: %d ok, %d failed, %d older than format v%d\n",
		len(paths), len(paths)-failed-older, failed, older, bundle.FormatVersion)
	return failed, nil
}

func check(path string) error {
	reader, err := bundle.Open(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := reader.Validate(); err != nil {
		return err
	}
	for _, world := range reader.Manifest.Worlds {
		if _, err := reader.Locations(world.Slug); err != nil {
			return fmt.Errorf("world %s: %w", world.Slug, err)
		}
	}
	return nil
}

// isOlderFormat peeks at the raw container alone — straight through
// archive/zip, past the reader's version refusal — because a bundle that
// says it is an earlier format is old, not broken. Format 1 announces itself
// by layout: its manifest is atlas.json, a name this format retired. A
// manifest.json that carries an earlier formatVersion is the same answer
// said in the current layout.
func isOlderFormat(path string) bool {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer archive.Close()
	names := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		names[file.Name] = file
	}
	manifest, current := names["manifest.json"]
	if !current {
		_, v1 := names["atlas.json"]
		return v1
	}
	entry, err := manifest.Open()
	if err != nil {
		return false
	}
	defer entry.Close()
	var peek struct {
		FormatVersion int `json:"formatVersion"`
	}
	if err := json.NewDecoder(entry).Decode(&peek); err != nil {
		return false
	}
	return peek.FormatVersion > 0 && peek.FormatVersion < bundle.FormatVersion
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
