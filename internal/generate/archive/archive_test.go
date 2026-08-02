package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fixture lays out a small archive holding two volumes: one crawled, one whose
// register entry names a directory nothing has filled.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "archive.json"), `{"games":[
		{"directory":"games/tunic-115","id":115,"title":"TUNIC","url":"https://example.test/tunic"},
		{"directory":"games/mars-1","id":1,"title":"Mars","source":"nasa-trek"},
		{"directory":"games/absent-9","id":9,"title":"Absent"}
	]}`)
	volume := filepath.Join(root, "games", "tunic-115")
	write(t, filepath.Join(volume, "game.json"), `{"id":115,"title":"TUNIC","maps":[
		{"directory":"games/tunic-115/maps/world-427","id":427,"slug":"world","title":"World"}
	]}`)
	world := filepath.Join(volume, "maps", "world-427")
	write(t, filepath.Join(world, "snapshots", "index.json"), `[
		{"capturedAt":"2026-07-01T00:00:00Z","contentHash":"old","kind":"map","sourceId":427,"sourceUrl":"/a"},
		{"capturedAt":"2026-07-30T03:57:41.529Z","contentHash":"new","kind":"map","sourceId":427,"sourceUrl":"/b"}
	]`)
	write(t, filepath.Join(world, "snapshots", "map", "old.json"), `{"v":1}`)
	write(t, filepath.Join(world, "snapshots", "map", "new.json"), `{"v":2}`)
	write(t, filepath.Join(volume, "icons", "fox_shrine.svg"), "<svg/>")
	write(t, filepath.Join(volume, "icons", "marker.png"), "PNG")
	// A world the register does not name: the directory listing is the truth.
	write(t, filepath.Join(volume, "maps", "unlisted-999", "snapshots", "index.json"), `[]`)
	return root
}

func TestVolumes(t *testing.T) {
	store, err := Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	volumes := store.Volumes()
	if len(volumes) != 3 {
		t.Fatalf("%d volumes", len(volumes))
	}
	tests := []struct {
		index  int
		source string
		title  string
	}{
		{0, DefaultSource, "TUNIC"},
		{1, "nasa-trek", "Mars"},
		{2, DefaultSource, "Absent"},
	}
	for _, tt := range tests {
		if volumes[tt.index].Source != tt.source || volumes[tt.index].Title != tt.title {
			t.Errorf("volume %d is %s/%s, want %s/%s", tt.index,
				volumes[tt.index].Source, volumes[tt.index].Title, tt.source, tt.title)
		}
	}
}

func TestWorlds(t *testing.T) {
	store, err := Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worlds, err := store.Worlds(store.Volumes()[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) != 2 {
		t.Fatalf("%d worlds, want the registered one and the unregistered one", len(worlds))
	}
	if worlds[0].Slug != "" {
		t.Errorf("the unregistered world claimed slug %q", worlds[0].Slug)
	}
	if worlds[1].Slug != "world" || worlds[1].ID != 427 || worlds[1].Title != "World" {
		t.Errorf("the registered world is %+v", worlds[1])
	}

	t.Run("an unfilled volume is not ready", func(t *testing.T) {
		_, err := store.Worlds(store.Volumes()[2])
		if !errors.Is(err, ErrNotReady) {
			t.Fatalf("Worlds = %v, want not-ready", err)
		}
	})
}

// TestNewest pins the one ordering rule: times compare as strings, and only the
// newest capture is ever read.
func TestNewest(t *testing.T) {
	store, err := Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worlds, err := store.Worlds(store.Volumes()[0])
	if err != nil {
		t.Fatal(err)
	}
	newest, err := store.Newest(worlds[1])
	if err != nil {
		t.Fatal(err)
	}
	if newest.ContentHash != "new" || newest.Kind != "map" || newest.SourceID != 427 {
		t.Errorf("newest capture is %+v", newest)
	}
	body, err := store.Body(worlds[1], newest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"v":2}` {
		t.Errorf("body is %s", body)
	}

	t.Run("an empty index is not ready", func(t *testing.T) {
		if _, err := store.Newest(worlds[0]); !errors.Is(err, ErrNotReady) {
			t.Fatalf("Newest = %v, want not-ready", err)
		}
	})
}

func TestArtwork(t *testing.T) {
	store, err := Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	volume := store.Volumes()[0]
	tests := []struct {
		name, key, want string
	}{
		{"svg is preferred", "fox_shrine", "fox_shrine.svg"},
		{"png where there is no svg", "marker", "marker.png"},
		{"nothing captured is not an error", "missing", ""},
		{"a key that could climb out is refused", "../../archive", ""},
		{"an empty key", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, _, err := store.Artwork(volume, tt.key, ".svg", ".png")
			if err != nil {
				t.Fatalf("Artwork: %v", err)
			}
			if file != tt.want {
				t.Errorf("Artwork(%q) = %q, want %q", tt.key, file, tt.want)
			}
		})
	}
}

func TestValidArtworkKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"fox_shrine", true},
		{"a-b-9", true},
		{"", false},
		{"../x", false},
		{"a/b", false},
		{"a.b", false},
	}
	for _, tt := range tests {
		if got := ValidArtworkKey(tt.key); got != tt.want {
			t.Errorf("ValidArtworkKey(%q) = %t", tt.key, got)
		}
	}
}

func TestTrimRoot(t *testing.T) {
	if got := TrimRoot("/a", "/a/b/c"); got != "b/c" {
		t.Errorf("TrimRoot = %q", got)
	}
	if got := TrimRoot("/a", "/elsewhere"); got != "/elsewhere" {
		t.Errorf("a path outside the root should be left whole, got %q", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
