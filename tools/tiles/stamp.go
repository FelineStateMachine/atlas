package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Every pyramid was derived afresh on every run: adding one game re-decoded and
// re-reduced the other seventeen, half a minute of work to arrive at the bytes
// that were already there. A layer is derived from tiles that are stored under
// their own content hash, so what went into a pyramid can be written down and
// compared, and a layer whose capture has not moved can be carried over from
// the last run instead.
//
// The tool's own source counts as an input. Changing how a level is reduced or
// where the content bounds land has to invalidate every pyramid, and a stamp
// that only watched the archive would quietly keep serving the old derivation.
//
//go:embed main.go stamp.go
var toolSource embed.FS

var toolStamp = sync.OnceValue(func() string {
	sum := sha256.New()
	entries, err := fs.ReadDir(toolSource, ".")
	if err != nil {
		// The files are embedded at build time, so this cannot fail in a built
		// binary. Falling back to a random-looking constant would defeat the
		// point of the stamp, and rebuilding everything is the safe answer.
		return "unstamped"
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := toolSource.ReadFile(name)
		if err != nil {
			return "unstamped"
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", name, len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))
})

// planStamp names everything a pyramid is derived from: the tool that derives
// it, the shape the plan settled on, and the content hash of every source tile
// that goes into it. Two runs that would write the same bytes stamp the same.
func planStamp(plan tilePlan) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "tool\x00%s\x00", toolStamp())
	fmt.Fprintf(sum, "layer\x00%s\x00%s\x00", plan.SourcePath, plan.AssetPath)
	fmt.Fprintf(sum, "zooms\x00%d\x00%d\x00", plan.MaxFullZoom, plan.MaxSourceZoom)
	fmt.Fprintf(sum, "format\x00%s\x00%t\x00", plan.PreferredFormat, plan.Interpolate)
	if plan.Bounds != nil {
		fmt.Fprintf(sum, "bounds\x00%d\x00%d\x00%d\x00%d\x00",
			plan.Bounds.X, plan.Bounds.Y, plan.Bounds.Width, plan.Bounds.Height)
	}

	zooms := make([]int, 0, len(plan.Levels))
	for zoom := range plan.Levels {
		zooms = append(zooms, zoom)
	}
	sort.Ints(zooms)
	for _, zoom := range zooms {
		files := append([]tileFile(nil), plan.Levels[zoom]...)
		sort.Slice(files, func(i, j int) bool {
			if files[i].Record.X != files[j].Record.X {
				return files[i].Record.X < files[j].Record.X
			}
			return files[i].Record.Y < files[j].Record.Y
		})
		fmt.Fprintf(sum, "level\x00%d\x00%d\x00", zoom, len(files))
		for _, file := range files {
			fmt.Fprintf(sum, "%d\x00%d\x00%s\x00%s\x00",
				file.Record.X, file.Record.Y, file.Record.ContentHash, file.Format)
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// linkTree reproduces a directory as links to the files it already holds, and
// reports how many it brought across. Directories are visited before their
// contents, so each one is made once rather than checked for again per file.
func linkTree(from, to string) (int, error) {
	tiles := 0
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.Mkdir(destination, 0o755)
		}
		tiles++
		if err := os.Link(path, destination); err == nil {
			return nil
		}
		// A link can fail across filesystems, and on one that has none. The
		// bytes are what matter, so fall back to writing them out.
		return copyFile(path, destination)
	})
	return tiles, err
}

// readManifest is the index left by the last run, or an empty one where there
// was no last run. A manifest that cannot be read is not an error: it means
// everything is derived again, which is what a first run does anyway.
func readManifest(output string) manifest {
	var previous manifest
	if err := readJSON(filepath.Join(output, "index.json"), &previous); err != nil {
		return manifest{}
	}
	return previous
}

func manifestBySource(previous manifest) map[string]variantManifest {
	bySource := make(map[string]variantManifest, len(previous.Variants))
	for _, variant := range previous.Variants {
		bySource[variant.SourcePath] = variant
	}
	return bySource
}
