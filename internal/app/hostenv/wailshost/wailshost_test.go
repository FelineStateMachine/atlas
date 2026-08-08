package wailshost

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAtlasPathsAcceptsOnlyNativeVolumeFiles(t *testing.T) {
	t.Parallel()

	got := atlasPaths([]string{"/tmp/world.atlas", "/tmp/readme.txt", "/tmp/legacy.zip"})
	want := []string{"/tmp/world.atlas"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("atlasPaths = %v, want %v", got, want)
	}
}

func TestNativeFileIntakeRefreshesTheOpenWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.atlas")
	if err := os.WriteFile(path, []byte("native volume"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed, refreshed := 0, 0
	window := Window{
		install: func(_ string, _ io.Reader) error {
			installed++
			return nil
		},
		refresh: func(context.Context) { refreshed++ },
	}
	ctx := context.Background()
	window.live.Store(&ctx)
	window.QueuePaths([]string{path})
	if installed != 1 || refreshed != 1 {
		t.Fatalf("installed=%d refreshed=%d, want one install and one refresh", installed, refreshed)
	}
}
