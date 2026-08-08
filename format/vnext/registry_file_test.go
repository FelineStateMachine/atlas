package vnext

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInstallStagesValidatedVNextBundleAndScanRejectsLegacy(t *testing.T) {
	t.Parallel()

	packed, err := Compile(minimalVolume())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	packed.Release.CreatedAt = "2026-08-01T20:13:08Z"
	var data bytes.Buffer
	if err := Write(&data, packed); err != nil {
		t.Fatalf("write: %v", err)
	}

	source := filepath.Join(t.TempDir(), "download.atlas")
	if err := os.WriteFile(source, data.Bytes(), 0o644); err != nil {
		t.Fatalf("source: %v", err)
	}
	library := t.TempDir()
	installed, err := Install(library, source, StandardSchema())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if filepath.Base(installed.Locator) != VersionedFileName("minimal", installed.release()) {
		t.Fatalf("installed as %s", installed.Locator)
	}

	legacy := filepath.Join(library, "legacy.atlas")
	if err := os.WriteFile(legacy, []byte("not-vnext"), 0o644); err != nil {
		t.Fatalf("legacy: %v", err)
	}
	descriptors, skipped, err := Scan(library, StandardSchema())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(descriptors) != 1 || len(skipped) != 1 || !strings.Contains(skipped[0].Err.Error(), "open Atlas archive") {
		t.Fatalf("scan = %#v, skipped = %#v", descriptors, skipped)
	}
}

func TestInstallRefusesCorruptionBeforeLibraryWrite(t *testing.T) {
	t.Parallel()

	library := t.TempDir()
	source := filepath.Join(t.TempDir(), "broken.atlas")
	if err := os.WriteFile(source, []byte("broken"), 0o644); err != nil {
		t.Fatalf("source: %v", err)
	}
	if _, err := Install(library, source, StandardSchema()); err == nil {
		t.Fatal("broken source was installed")
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatalf("read library: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("broken install left %d files", len(entries))
	}
}

func TestInstallRefusesAnInvalidOrDifferentExistingStampTarget(t *testing.T) {
	t.Parallel()

	source, data := nativeInstallFixture(t)
	for _, test := range []struct {
		name   string
		mutate func(string) error
	}{
		{name: "corrupt", mutate: func(path string) error {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			contents[len(contents)/2] ^= 0xff
			return os.WriteFile(path, contents, 0o644)
		}},
		{name: "different bytes", mutate: func(path string) error {
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, writeErr := file.WriteString("different container bytes")
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			library := t.TempDir()
			installed, present, err := InstallWithStatus(library, source, StandardSchema())
			if err != nil || present {
				t.Fatalf("initial install = %+v, present=%v, %v", installed, present, err)
			}
			if err := test.mutate(installed.Locator); err != nil {
				t.Fatal(err)
			}
			if _, _, err := InstallWithStatus(library, source, StandardSchema()); err == nil {
				t.Fatal("changed stamp target was accepted")
			}
			if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, data) {
				t.Fatal("validated source changed while refusing the target")
			}
		})
	}
}

func TestConcurrentInstallsCreateOneImmutableTarget(t *testing.T) {
	t.Parallel()

	source, _ := nativeInstallFixture(t)
	library := t.TempDir()
	const workers = 16
	results := make(chan Descriptor, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			descriptor, err := Install(library, source, StandardSchema())
			if err != nil {
				errors <- err
				return
			}
			results <- descriptor
		}()
	}
	group.Wait()
	close(errors)
	close(results)
	for err := range errors {
		t.Errorf("concurrent install: %v", err)
	}
	var locator string
	for result := range results {
		if locator == "" {
			locator = result.Locator
		}
		if result.Locator != locator {
			t.Errorf("concurrent target = %s, want %s", result.Locator, locator)
		}
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != Extension {
		t.Fatalf("library entries = %v, want one committed Atlas", entries)
	}
}

func nativeInstallFixture(t *testing.T) (string, []byte) {
	t.Helper()
	packed, err := Compile(minimalVolume())
	if err != nil {
		t.Fatal(err)
	}
	packed.Release.CreatedAt = "2026-08-08T12:00:00Z"
	var data bytes.Buffer
	if err := Write(&data, packed); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "sample-region.atlas")
	if err := os.WriteFile(source, data.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return source, data.Bytes()
}
