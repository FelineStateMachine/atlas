package vnext

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type Skipped struct {
	Locator string
	Err     error
}

func (skipped Skipped) Error() string { return skipped.Locator + ": " + skipped.Err.Error() }
func (skipped Skipped) Unwrap() error { return skipped.Err }

// File owns the operating-system handle behind a Reader.
type File struct {
	*Reader
	source *os.File
	size   int64
	path   string
}

func OpenFile(path string, applicationSchema Schema) (*File, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	stat, err := source.Stat()
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("stat %s: %w", filepath.Base(path), err)
	}
	reader, err := Open(source, stat.Size(), applicationSchema)
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &File{Reader: reader, source: source, size: stat.Size(), path: path}, nil
}

func (file *File) Close() error { return file.source.Close() }

func (file *File) Descriptor() Descriptor {
	return DescriptorOf(file.path, file.VolumeID, file.Release, file.size)
}

func Describe(path string, applicationSchema Schema) (Descriptor, error) {
	file, err := OpenFile(path, applicationSchema)
	if err != nil {
		return Descriptor{}, err
	}
	defer file.Close()
	return file.Descriptor(), nil
}

func Scan(dir string, applicationSchema Schema) ([]Descriptor, []Skipped, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*"+Extension))
	if err != nil {
		return nil, nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	sort.Strings(paths)
	descriptors := make([]Descriptor, 0, len(paths))
	var skipped []Skipped
	for _, path := range paths {
		descriptor, err := Describe(path, applicationSchema)
		if err != nil {
			skipped = append(skipped, Skipped{Locator: path, Err: err})
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, skipped, nil
}

func Install(dir, source string, applicationSchema Schema) (Descriptor, error) {
	opened, err := OpenFile(source, applicationSchema)
	if err != nil {
		return Descriptor{}, err
	}
	if err := opened.Validate(); err != nil {
		opened.Close()
		return Descriptor{}, fmt.Errorf("%s: %w", filepath.Base(source), err)
	}
	descriptor := opened.Descriptor()
	if err := opened.Close(); err != nil {
		return Descriptor{}, fmt.Errorf("close %s: %w", filepath.Base(source), err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Descriptor{}, fmt.Errorf("create library: %w", err)
	}
	target := filepath.Join(dir, VersionedFileName(descriptor.Slug, descriptor.release()))
	if sameFile(source, target) {
		return Describe(target, applicationSchema)
	}
	if _, err := os.Stat(target); err == nil {
		return Describe(target, applicationSchema)
	}
	if err := copyInto(dir, source, target); err != nil {
		return Descriptor{}, err
	}
	return validateInstalled(target, applicationSchema)
}

func InstallBytes(dir string, source []byte, applicationSchema Schema) (Descriptor, error) {
	opened, err := Open(bytes.NewReader(source), int64(len(source)), applicationSchema)
	if err != nil {
		return Descriptor{}, err
	}
	if err := opened.Validate(); err != nil {
		return Descriptor{}, err
	}
	descriptor := DescriptorOf("", opened.VolumeID, opened.Release, int64(len(source)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Descriptor{}, fmt.Errorf("create library: %w", err)
	}
	target := filepath.Join(dir, VersionedFileName(descriptor.Slug, descriptor.release()))
	if _, err := os.Stat(target); err == nil {
		return Describe(target, applicationSchema)
	}
	if err := stageInto(dir, bytes.NewReader(source), target); err != nil {
		return Descriptor{}, err
	}
	return validateInstalled(target, applicationSchema)
}

func validateInstalled(target string, applicationSchema Schema) (Descriptor, error) {
	installed, err := OpenFile(target, applicationSchema)
	if err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	defer installed.Close()
	if err := installed.Validate(); err != nil {
		return Descriptor{}, fmt.Errorf("installed %s: %w", filepath.Base(target), err)
	}
	return installed.Descriptor(), nil
}

func copyInto(dir, source, target string) error {
	from, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(source), err)
	}
	defer from.Close()
	return stageInto(dir, from, target)
}

func stageInto(dir string, source io.Reader, target string) error {
	staged, err := os.CreateTemp(dir, ".installing-*")
	if err != nil {
		return fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName)
	if _, err := io.Copy(staged, source); err != nil {
		staged.Close()
		return fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return fmt.Errorf("sync %s: %w", filepath.Base(target), err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(target), err)
	}
	if err := os.Chmod(stagedName, 0o644); err != nil {
		return fmt.Errorf("mode %s: %w", filepath.Base(target), err)
	}
	if err := os.Rename(stagedName, target); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(target), err)
	}
	return nil
}

func sameFile(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func (descriptor Descriptor) release() Release {
	return Release{
		Title: descriptor.Title, CreatedAt: descriptor.CreatedAt, Revision: descriptor.Revision,
		Stamp: descriptor.Stamp, Worlds: descriptor.Worlds,
	}
}
