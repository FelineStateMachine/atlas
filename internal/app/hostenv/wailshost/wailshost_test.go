package wailshost

import (
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
