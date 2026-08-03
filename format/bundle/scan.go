package bundle

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// This file is the whole of the package's filesystem: a walk that turns a
// directory of .atlas files into descriptors, and an install that lets one
// more file in. Everything above it -- the fold, the ordering, the index -- is
// pure, so a host that keeps its library elsewhere reimplements only what is
// here.

// Extension is the suffix a bundle file carries in a registry directory.
const Extension = ".atlas"

// Skipped is one candidate a scan could not read. One bad download should not
// take a library down with it, so a scan reports what it passed over and
// leaves the decision -- warn, refuse, ignore -- to its caller. That is also
// what keeps this package free of a logger.
type Skipped struct {
	Locator string
	Err     error
}

func (s Skipped) Error() string { return s.Locator + ": " + s.Err.Error() }
func (s Skipped) Unwrap() error { return s.Err }

// Scan reads every bundle in dir and returns their descriptors, sorted by
// locator. A directory that is missing or empty is a registry with no
// volumes, not an error.
//
// Only the manifest of each bundle is read, so a scan of a library of
// hundred-megabyte files costs a seek and a small read per file. The returned
// descriptors are what [Fold] and [BuildIndex] consume.
func Scan(dir string) ([]Descriptor, []Skipped, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*"+Extension))
	if err != nil {
		return nil, nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	sort.Strings(paths)

	descriptors := make([]Descriptor, 0, len(paths))
	var skipped []Skipped
	for _, path := range paths {
		descriptor, err := Describe(path)
		if err != nil {
			skipped = append(skipped, Skipped{Locator: path, Err: err})
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, skipped, nil
}

// Describe opens one bundle far enough to read its manifest and closes it
// again. It is the per-file step of [Scan], exported because a host importing
// a single file wants exactly this and nothing more.
func Describe(path string) (Descriptor, error) {
	reader, err := Open(path)
	if err != nil {
		return Descriptor{}, err
	}
	defer reader.Close()
	return reader.Descriptor(), nil
}

// Install copies the bundle at source into dir under its versioned name, and
// reports the descriptor of the installed build.
//
// The sequence is deliberate. The source is opened and validated before
// anything is written, so a broken file never lands in a library. The copy
// goes to a temporary name in the same directory and is renamed into place,
// so a reader scanning mid-install sees either no file or a whole one -- the
// temporary name does not end in the bundle extension, so a concurrent scan
// passes over it. The installed file is then reopened and validated again,
// because the copy is what will actually serve and it is the copy's promises
// that matter.
//
// Builds sit side by side and the fold serves the right one, so installing an
// old file can never overwrite a newer build; it just arrives already
// shadowed. Installing a build that is already present is a no-op: the name
// carries the stamp, so a file of that name is that build.
//
// Install touches no application state. Where the host cannot rename over an
// open file, it is the host's business to release its handle and retry.
func Install(dir, source string) (Descriptor, error) {
	opened, err := Open(source)
	if err != nil {
		return Descriptor{}, err
	}
	manifest := opened.Manifest
	if err := opened.Validate(); err != nil {
		opened.Close()
		return Descriptor{}, fmt.Errorf("%s: %w", filepath.Base(source), err)
	}
	opened.Close()

	target := filepath.Join(dir, VersionedFileName(manifest))
	if sameFile(source, target) {
		return Describe(target)
	}
	if _, err := os.Stat(target); err == nil {
		// This exact build is already installed under its own name.
		return Describe(target)
	}
	if err := copyInto(dir, source, target); err != nil {
		return Descriptor{}, err
	}
	descriptor, err := Describe(target)
	if err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	installed, err := Open(target)
	if err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	defer installed.Close()
	if err := installed.Validate(); err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	return descriptor, nil
}

// InstallBytes installs a bundle held in memory -- an embedded asset, a
// download -- into dir under its versioned name, and reports the descriptor of
// the installed build. It keeps every promise [Install] makes: the bytes are
// validated before anything is written, the copy stages under a temporary name
// a concurrent scan passes over, an already-present build is a no-op, and the
// installed file is reopened and validated because the copy is what will serve.
func InstallBytes(dir string, source []byte) (Descriptor, error) {
	opened, err := NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		return Descriptor{}, err
	}
	manifest := opened.Manifest
	if err := opened.Validate(); err != nil {
		return Descriptor{}, err
	}

	target := filepath.Join(dir, VersionedFileName(manifest))
	if _, err := os.Stat(target); err == nil {
		// This exact build is already installed under its own name.
		return Describe(target)
	}
	staged, err := os.CreateTemp(dir, ".installing-*")
	if err != nil {
		return Descriptor{}, fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	defer os.Remove(staged.Name())
	if _, err := staged.Write(source); err != nil {
		staged.Close()
		return Descriptor{}, fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	if err := staged.Close(); err != nil {
		return Descriptor{}, fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	// CreateTemp opens private files; a bundle is an artifact anyone may copy.
	if err := os.Chmod(staged.Name(), 0o644); err != nil {
		return Descriptor{}, fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		return Descriptor{}, fmt.Errorf("install %s: %w", filepath.Base(target), err)
	}
	descriptor, err := Describe(target)
	if err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	installed, err := Open(target)
	if err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	defer installed.Close()
	if err := installed.Validate(); err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	return descriptor, nil
}

// copyInto stages a copy of source beside target and renames it into place.
func copyInto(dir, source, target string) error {
	from, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(source), err)
	}
	defer from.Close()

	staged, err := os.CreateTemp(dir, ".installing-*")
	if err != nil {
		return fmt.Errorf("stage %s: %w", filepath.Base(source), err)
	}
	defer os.Remove(staged.Name())
	if _, err := io.Copy(staged, from); err != nil {
		staged.Close()
		return fmt.Errorf("copy %s: %w", filepath.Base(source), err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("copy %s: %w", filepath.Base(source), err)
	}
	// CreateTemp opens private files; a bundle is an artifact anyone may copy.
	if err := os.Chmod(staged.Name(), 0o644); err != nil {
		return fmt.Errorf("stage %s: %w", filepath.Base(source), err)
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(target), err)
	}
	return nil
}

// WriteIndex derives the listing from descriptors and writes index.json
// beside them. It is the only thing this package writes into a registry
// directory that is not a bundle, and it is always safe to delete: the next
// call derives it again.
func WriteIndex(dir string, candidates []Descriptor) error {
	data, err := MarshalIndex(BuildIndex(candidates))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "index.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// sameFile reports whether two paths name one file, so installing a bundle
// that already sits in the library under its own name copies nothing.
func sameFile(a, b string) bool {
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(left, right)
}
