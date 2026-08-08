package wailshost

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
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

func TestConcurrentNativeFileIntakeIsSerializedAndRefreshesOnce(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "first.atlas"), filepath.Join(dir, "second.atlas")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var active, maximum, installed, refreshed atomic.Int32
	window := Window{
		install: func(_ string, _ io.Reader) error {
			current := active.Add(1)
			for {
				held := maximum.Load()
				if current <= held || maximum.CompareAndSwap(held, current) {
					break
				}
			}
			if installed.Add(1) == 1 {
				close(started)
				<-release
			}
			active.Add(-1)
			return nil
		},
		refresh: func(context.Context) { refreshed.Add(1) },
	}
	ctx := context.Background()
	window.live.Store(&ctx)
	var calls sync.WaitGroup
	calls.Add(2)
	go func() { defer calls.Done(); window.QueuePaths(paths[:1]) }()
	<-started
	queued := make(chan struct{})
	go func() {
		defer calls.Done()
		window.QueuePaths(paths[1:])
		close(queued)
	}()
	<-queued
	close(release)
	calls.Wait()
	if installed.Load() != 2 || maximum.Load() != 1 || refreshed.Load() != 1 {
		t.Fatalf("installed=%d maximum=%d refreshed=%d", installed.Load(), maximum.Load(), refreshed.Load())
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
