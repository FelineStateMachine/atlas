package bundle_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// These tests read a real registry directory -- an installed library of
// .atlas files -- and hold this package to the bytes already on disk. They
// are the only place the clean-room implementation meets the artifacts of the
// one it replaces, which is what makes them worth the awkwardness of an
// environment variable.
//
// Set ATLAS_REGISTRY_DIR to a bundles directory to run them. Unset, they
// skip, so a machine with no library -- CI, a fresh checkout -- stays green.
const registryDirEnv = "ATLAS_REGISTRY_DIR"

func realBundles(t *testing.T) []string {
	t.Helper()
	dir := os.Getenv(registryDirEnv)
	if dir == "" {
		t.Skipf("set %s to a bundles directory to check against real bundles", registryDirEnv)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*"+bundle.Extension))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skipf("%s holds no bundles", dir)
	}
	return paths
}

// openV3 opens a bundle if it is one this package reads, and reports the raw
// manifest bytes exactly as the archive carries them. Older format versions
// are skipped rather than failed: a library accumulates history, and refusing
// a version this package does not know is the documented behavior, not a bug
// to assert against every old file.
func openV3(t *testing.T, path string) (*bundle.Reader, []byte, bool) {
	t.Helper()
	reader, err := bundle.Open(path)
	if err != nil {
		return nil, nil, false
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		reader.Close()
		t.Fatal(err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name != bundle.ManifestName {
			continue
		}
		source, err := file.Open()
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		raw, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		return reader, raw, true
	}
	reader.Close()
	t.Fatalf("%s opened without a manifest", path)
	return nil, nil, false
}

// The manifest schema is stamped over, so a re-encode that differs by one
// byte is a different stamp for every bundle ever built. This is the check
// that says the clean-room schema is the same schema.
func TestRealManifestsReEncodeExactly(t *testing.T) {
	var checked int
	for _, path := range realBundles(t) {
		reader, raw, ok := openV3(t, path)
		if !ok {
			continue
		}
		encoded, err := bundle.MarshalManifest(reader.Manifest)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, raw) {
			t.Errorf("%s re-encodes differently:\n got %s\nwant %s", filepath.Base(path), encoded, raw)
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Skip("the library holds no bundles of a format version this package reads")
	}
	t.Logf("%d real manifests re-encoded byte for byte", checked)
}

// The file name is derived from the manifest alone, so a library built by
// another implementation must be named exactly as this one would have named
// it. It is the end-to-end check on the stamp's short form and the capture-day
// rule together.
func TestRealFileNamesAreDerivable(t *testing.T) {
	var checked int
	for _, path := range realBundles(t) {
		reader, _, ok := openV3(t, path)
		if !ok {
			continue
		}
		derived := bundle.VersionedFileName(reader.Manifest)
		reader.Close()
		if derived != filepath.Base(path) {
			t.Errorf("%s would be named %s", filepath.Base(path), derived)
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Skip("the library holds no bundles of a format version this package reads")
	}
	t.Logf("%d real file names derived from their manifests", checked)
}

// Every packed payload in the library is decoded and packed again; the bytes
// must come back identical. This is the codec's parity check against an
// implementation that is not this one.
func TestRealPackedPayloadsRoundTrip(t *testing.T) {
	var worlds, locations int
	for _, path := range realBundles(t) {
		reader, _, ok := openV3(t, path)
		if !ok {
			continue
		}
		for _, entry := range reader.Manifest.Worlds {
			name := bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix)
			if !reader.Stored(name) {
				t.Errorf("%s %s is not stored uncompressed", filepath.Base(path), name)
			}
			data, err := reader.ReadEntry(name)
			if err != nil {
				t.Errorf("%s: %v", filepath.Base(path), err)
				continue
			}
			packed, err := bundle.OpenPacked(data)
			if err != nil {
				t.Errorf("%s %s: %v", filepath.Base(path), name, err)
				continue
			}
			if packed.Len() != entry.Points {
				t.Errorf("%s %s packs %d, the manifest says %d",
					filepath.Base(path), entry.Slug, packed.Len(), entry.Points)
			}
			if again := bundle.PackLocations(packed.All()); !bytes.Equal(again, data) {
				t.Errorf("%s %s repacked to %d bytes from %d",
					filepath.Base(path), entry.Slug, len(again), len(data))
			}
			worlds++
			locations += packed.Len()
		}
		reader.Close()
	}
	if worlds == 0 {
		t.Skip("the library holds no bundles of a format version this package reads")
	}
	t.Logf("%d real packed payloads round-tripped, %d locations in all", worlds, locations)
}

// Every bundle in the library must pass the validation a producer ran before
// writing it -- the offline scan, the per-kind counts, the conventions it
// declares. A failure here is either a real defect in the library or a rule
// this package reads more strictly than the producer wrote.
func TestRealBundlesValidate(t *testing.T) {
	if testing.Short() {
		t.Skip("validation reads every payload of every bundle")
	}
	var checked int
	for _, path := range realBundles(t) {
		reader, _, ok := openV3(t, path)
		if !ok {
			continue
		}
		err := reader.Validate()
		reader.Close()
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Skip("the library holds no bundles of a format version this package reads")
	}
	t.Logf("%d real bundles validated", checked)
}

// A scan of the real library must fold to one serving build per volume, and
// the index derived from it must list every build.
func TestRealLibraryFolds(t *testing.T) {
	realBundles(t)
	descriptors, skipped, err := bundle.Scan(os.Getenv(registryDirEnv))
	if err != nil {
		t.Fatal(err)
	}
	winners := bundle.Fold(descriptors)
	if len(winners) == 0 {
		t.Fatal("the library folded to nothing")
	}
	for slug, winner := range winners {
		if winner.Slug != slug {
			t.Errorf("%s is served by a build of %s", slug, winner.Slug)
		}
		for _, candidate := range descriptors {
			if candidate.Slug == slug && bundle.Newer(candidate, winner) {
				t.Errorf("%s serves %s while %s is newer", slug, winner.Locator, candidate.Locator)
			}
		}
	}
	index := bundle.BuildIndex(descriptors)
	var listed int
	for _, volume := range index.Volumes {
		listed += len(volume.Versions)
	}
	if listed != len(descriptors) {
		t.Errorf("the index lists %d builds of %d", listed, len(descriptors))
	}
	t.Logf("%d builds, %d volumes, %d skipped (older format versions and strays)",
		len(descriptors), len(winners), len(skipped))
}
