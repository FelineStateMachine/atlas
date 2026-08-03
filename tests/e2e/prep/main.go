// Command prep packs the e2e registry: every corpus volume, written out as a
// real .atlas file under dist/e2e/bundles.
//
// `make test-e2e` runs this before Playwright starts the application, so the
// browser walks a library that was built from committed bytes moments
// earlier. There is nothing to fetch and nothing that can be missing: if the
// corpus does not pack, this command fails and the e2e run goes red rather
// than quietly walking an empty library.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FelineStateMachine/atlas/tests/corpus"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "prep:", err)
		os.Exit(1)
	}
}

func run() error {
	root := corpus.Root(".")
	slugs, err := corpus.Slugs(root)
	if err != nil {
		return fmt.Errorf("reading the corpus (run from the repository root): %w", err)
	}
	if len(slugs) == 0 {
		return fmt.Errorf("the corpus at %s holds no volumes", root)
	}

	dir := filepath.Join("dist", "e2e", "bundles")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, slug := range slugs {
		path, err := corpus.Pack(filepath.Join(root, slug), dir)
		if err != nil {
			return fmt.Errorf("packing %s: %w", slug, err)
		}
		fmt.Println(path)
	}
	return nil
}
