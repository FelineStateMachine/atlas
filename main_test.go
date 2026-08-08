package main

import (
	"bytes"
	"image/jpeg"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
)

func includedEarth(t *testing.T) []byte {
	t.Helper()
	names, err := fs.Glob(included, "included/*.atlas")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("embedded %d volumes, want one Earth", len(names))
	}
	data, err := included.ReadFile(names[0])
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openIncluded(t *testing.T) *vnext.Reader {
	t.Helper()
	data := includedEarth(t)
	reader, err := vnext.Open(bytes.NewReader(data), int64(len(data)), vnext.StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func TestIncludedEarthIsNativeAndSemantic(t *testing.T) {
	reader := openIncluded(t)
	if err := reader.Validate(); err != nil {
		t.Fatal(err)
	}
	if reader.VolumeID != "earth" || reader.Release.Title != "Earth" || reader.Release.CreatedAt != "2026-08-03T16:21:07Z" {
		t.Fatalf("identity/release = %s %#v", reader.VolumeID, reader.Release)
	}
	volume, err := reader.Volume()
	if err != nil {
		t.Fatal(err)
	}
	if len(volume.Worlds) != 1 || volume.Worlds[0].ID != "earth" {
		t.Fatalf("worlds = %#v", volume.Worlds)
	}
	points, areas := 0, 0
	for _, set := range volume.Worlds[0].FeatureSets {
		for _, feature := range set.Features {
			switch feature.Geometry.Kind {
			case vnext.GeometryPoint:
				points++
			case vnext.GeometryPolygon:
				areas++
			}
		}
	}
	if points != 202 || areas != 177 {
		t.Fatalf("Earth holds %d points and %d areas", points, areas)
	}
	if len(reader.TableNames()) != 16 {
		t.Fatalf("native bundle has %d typed tables", len(reader.TableNames()))
	}
}

func TestIncludedEarthPyramidOpensAtEveryLevel(t *testing.T) {
	reader := openIncluded(t)
	perLevel := map[string]int{}
	for _, name := range reader.BlobNames() {
		if !strings.HasPrefix(name, "tiles/earth/") {
			continue
		}
		parts := strings.Split(name, "/")
		perLevel[parts[2]]++
	}
	want := map[string]int{"0": 1, "1": 2, "2": 8, "3": 32, "4": 128, "5": 512, "6": 2048}
	for level, count := range want {
		if perLevel[level] != count {
			t.Errorf("level %s has %d tiles, want %d", level, perLevel[level], count)
		}
	}
	for _, name := range []string{"tiles/earth/0/0/0.jpg", "tiles/earth/6/63/31.jpg"} {
		data, err := reader.Blob(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestInstallIncludedIsIdempotentAndPreservesOtherBuilds(t *testing.T) {
	library := t.TempDir()
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(library)
	if len(entries) != 1 {
		t.Fatalf("first install wrote %d files", len(entries))
	}
	installed := filepath.Join(library, entries[0].Name())
	before, _ := os.Stat(installed)
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(installed)
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatal("idempotent install touched file")
	}
	other := writeDistinctEarth(t, library)
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("other build was removed")
	}
	entries, _ = os.ReadDir(library)
	if len(entries) != 2 {
		t.Fatalf("library holds %d files, want two builds", len(entries))
	}
}

func writeDistinctEarth(t *testing.T, dir string) string {
	t.Helper()
	volume := vnext.Volume{ID: "earth", Title: "Earth", Worlds: []vnext.World{{
		ID: "flat", Title: "Flat", CoordinateSpace: vnext.CoordinateSpace{ID: "flat-space", Kind: "synthetic", Unit: "pixel", Definition: "atlas:plane", Extent: [4]float64{0, 0, 1, 1}},
		Presentation: vnext.Presentation{ID: "default", Title: "Default"},
	}}}
	packed, err := vnext.Compile(volume)
	if err != nil {
		t.Fatal(err)
	}
	packed.Release.CreatedAt = "2026-09-01T00:00:00Z"
	temporary := filepath.Join(dir, "other.tmp")
	file, err := os.Create(temporary)
	if err != nil {
		t.Fatal(err)
	}
	if err := vnext.Write(file, packed); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	descriptor, err := vnext.Describe(temporary, vnext.StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, vnext.VersionedFileName(descriptor.Slug, vnext.Release{Title: descriptor.Title, CreatedAt: descriptor.CreatedAt, Revision: descriptor.Revision, Stamp: descriptor.Stamp, Worlds: descriptor.Worlds}))
	if err := os.Rename(temporary, target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestDesktopStartupAndApplicationServeNativeEarth(t *testing.T) {
	data := t.TempDir()
	library := filepath.Join(data, "bundles")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installIncluded(library); err != nil {
		t.Fatal(err)
	}
	host, err := oshost.New(oshost.Options{BundlesDir: library, SessionsDir: filepath.Join(data, "sessions")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Volumes().(interface{ Close() error }).Close() })
	volumes := host.Volumes().Volumes()
	if len(volumes) != 1 || volumes[0].Info().Slug != "earth" {
		t.Fatalf("startup volumes = %#v", volumes)
	}
	server := httptest.NewServer(app.New(host, app.Options{}))
	defer server.Close()
	catalog := get(t, server.Client(), server.URL+"/data/catalog.json")
	if !strings.Contains(string(catalog), `"earth"`) {
		t.Fatalf("catalog = %s", catalog)
	}
	page := get(t, server.Client(), server.URL+"/v/earth/earth")
	if !strings.Contains(string(page), "Earth") {
		t.Fatal("Earth page did not open")
	}
	stamp := vnext.ShortStamp(volumes[0].Info().Stamp)
	schema := get(t, server.Client(), server.URL+"/data/v/earth/"+stamp+"/schema.json")
	if !bytes.Contains(schema, []byte(`"types"`)) {
		t.Fatal("native schema endpoint is not canonical schema JSON")
	}
	features := get(t, server.Client(), server.URL+"/data/v/earth/"+stamp+"/data/features.pack")
	if !bytes.HasPrefix(features, []byte("ATLASPK\x00")) {
		t.Fatal("native feature table endpoint has no typed-block framing")
	}
	tile := get(t, server.Client(), server.URL+"/data/v/earth/"+stamp+"/tiles/earth/0/0/0.jpg")
	if _, err := jpeg.Decode(bytes.NewReader(tile)); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, client *http.Client, url string) []byte {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
