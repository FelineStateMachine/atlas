package vnext

import (
	"bytes"
	"crypto/sha256"
	"errors"
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
	descriptor, _, err := InstallWithStatus(dir, source, applicationSchema)
	return descriptor, err
}

// InstallWithStatus validates and atomically installs the exact opened source.
// present reports that an identical target was already committed.
func InstallWithStatus(dir, source string, applicationSchema Schema) (Descriptor, bool, error) {
	file, err := os.Open(source)
	if err != nil {
		return Descriptor{}, false, fmt.Errorf("open %s: %w", filepath.Base(source), err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Descriptor{}, false, fmt.Errorf("stat %s: %w", filepath.Base(source), err)
	}
	return installFrom(dir, file, info.Size(), source, applicationSchema)
}

type installSource interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

func installFrom(dir string, source installSource, size int64, sourcePath string, applicationSchema Schema) (Descriptor, bool, error) {
	opened, err := Open(source, size, applicationSchema)
	if err != nil {
		return Descriptor{}, false, err
	}
	if err := opened.Validate(); err != nil {
		return Descriptor{}, false, fmt.Errorf("validate native source: %w", err)
	}
	descriptor := DescriptorOf(sourcePath, opened.VolumeID, opened.Release, size)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Descriptor{}, false, fmt.Errorf("create library: %w", err)
	}
	target := filepath.Join(dir, VersionedFileName(descriptor.Slug, descriptor.release()))
	if sourcePath != "" && sameFile(sourcePath, target) {
		descriptor.Locator = target
		return descriptor, true, nil
	}
	if _, err := os.Stat(target); err == nil {
		return validateExisting(target, source, size, descriptor, applicationSchema)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Descriptor{}, false, fmt.Errorf("stat %s: %w", filepath.Base(target), err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return Descriptor{}, false, fmt.Errorf("rewind validated source: %w", err)
	}
	installed, err := stageInto(dir, source, target)
	if err != nil {
		return Descriptor{}, false, err
	}
	if !installed {
		return validateExisting(target, source, size, descriptor, applicationSchema)
	}
	if err := syncDirectory(dir); err != nil {
		os.Remove(target)
		return Descriptor{}, false, fmt.Errorf("sync Atlas library: %w", err)
	}
	committed, err := validateInstalled(target, applicationSchema)
	if err != nil {
		os.Remove(target)
		_ = syncDirectory(dir)
		return Descriptor{}, false, err
	}
	return committed, false, nil
}

func InstallBytes(dir string, source []byte, applicationSchema Schema) (Descriptor, error) {
	descriptor, _, err := installFrom(dir, bytes.NewReader(source), int64(len(source)), "", applicationSchema)
	return descriptor, err
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

func stageInto(dir string, source io.Reader, target string) (bool, error) {
	staged, err := os.CreateTemp(dir, ".installing-*")
	if err != nil {
		return false, fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName)
	if _, err := io.Copy(staged, source); err != nil {
		staged.Close()
		return false, fmt.Errorf("stage %s: %w", filepath.Base(target), err)
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return false, fmt.Errorf("sync %s: %w", filepath.Base(target), err)
	}
	if err := staged.Close(); err != nil {
		return false, fmt.Errorf("close %s: %w", filepath.Base(target), err)
	}
	if err := os.Chmod(stagedName, 0o644); err != nil {
		return false, fmt.Errorf("mode %s: %w", filepath.Base(target), err)
	}
	if err := os.Link(stagedName, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("install %s: %w", filepath.Base(target), err)
	}
	return true, nil
}

func validateExisting(target string, source installSource, sourceSize int64, expected Descriptor, applicationSchema Schema) (Descriptor, bool, error) {
	held, err := validateInstalled(target, applicationSchema)
	if err != nil {
		return Descriptor{}, false, fmt.Errorf("existing stamp target is invalid: %w", err)
	}
	if held.Slug != expected.Slug || held.Stamp != expected.Stamp || held.Size != sourceSize {
		return Descriptor{}, false, fmt.Errorf("existing %s disagrees with its stamp identity", filepath.Base(target))
	}
	same, err := sameContent(source, sourceSize, target)
	if err != nil {
		return Descriptor{}, false, err
	}
	if !same {
		return Descriptor{}, false, fmt.Errorf("existing %s differs byte-for-byte for stamp %s", filepath.Base(target), ShortStamp(expected.Stamp))
	}
	return held, true, nil
}

func sameContent(source io.ReadSeeker, sourceSize int64, target string) (bool, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("rewind validated source: %w", err)
	}
	left := sha256.New()
	written, err := io.Copy(left, source)
	if err != nil {
		return false, fmt.Errorf("hash validated source: %w", err)
	}
	if written != sourceSize {
		return false, fmt.Errorf("validated source changed length while installing")
	}
	right, err := os.Open(target)
	if err != nil {
		return false, fmt.Errorf("open existing %s: %w", filepath.Base(target), err)
	}
	defer right.Close()
	rightHash := sha256.New()
	otherSize, err := io.Copy(rightHash, right)
	if err != nil {
		return false, fmt.Errorf("hash existing %s: %w", filepath.Base(target), err)
	}
	return otherSize == sourceSize && bytes.Equal(left.Sum(nil), rightHash.Sum(nil)), nil
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
