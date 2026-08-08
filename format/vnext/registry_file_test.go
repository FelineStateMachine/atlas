package vnext

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
