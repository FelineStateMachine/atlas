package oshost_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
)

// build writes a real bundle through the real writer, so the store under test
// meets an archive the format would accept and nothing opaque is checked in.
type build struct {
	slug      string
	title     string
	createdAt string
	revision  int
	world     string
}

func (b build) write(t *testing.T, dir string) string {
	t.Helper()
	if b.world == "" {
		b.world = "overworld"
	}
	volume := vnext.Volume{
		ID: b.slug, Title: b.title,
		Worlds: []vnext.World{{
			ID: b.world, Title: b.title + " ground",
			CoordinateSpace: vnext.CoordinateSpace{ID: "space", Kind: "synthetic", Unit: "pixel", Definition: "atlas:plane", SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192, Extent: [4]float64{0, 0, 8192, 8192}},
			FeatureSets: []vnext.FeatureSet{{ID: "markers", Title: "Markers", SemanticType: "geometry.point", Features: []vnext.Feature{{
				ID: "origin", Title: "Origin", Geometry: vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{0, 0}}}}}},
			}}}},
			RasterPyramids: []vnext.RasterPyramid{{ID: "base", Name: "Base", Codec: "image/jpeg", TileSize: 256, MinZoom: 0, MaxZoom: 0, FullZoom: 0, SourceZoom: 13, Template: "tiles/" + b.world + "/{z}/{x}/{y}.jpg", Formats: []string{"jpg"}}},
			Presentation:   vnext.Presentation{ID: "default", Title: "Default", Styles: []vnext.Style{{ID: "marker"}}, Layers: []vnext.Layer{{ID: "markers", FeatureSet: "markers", Style: "marker", Visible: true}}},
		}},
	}
	packed, err := vnext.Compile(volume)
	if err != nil {
		t.Fatal(err)
	}
	packed.Release.CreatedAt = b.createdAt
	packed.Release.Revision = b.revision
	packed.Blobs = append(packed.Blobs, vnext.Blob{Name: "tiles/" + b.world + "/0/0/0.jpg", Data: []byte("raster")})
	path := filepath.Join(dir, b.slug+"-"+b.createdAt[5:7]+b.createdAt[8:10]+".atlas")
	file, err := os.Create(path)
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
	return path
}

// The fold is format/vnext's; what this checks is that the walk hands it
// every build and serves the one it picks.
func TestVolumesServesTheFoldsWinner(t *testing.T) {
	dir := t.TempDir()
	build{slug: "tunic", title: "TUNIC", createdAt: "2026-01-01T00:00:00Z"}.write(t, dir)
	newer := build{slug: "tunic", title: "TUNIC", createdAt: "2026-02-01T00:00:00Z"}
	newer.write(t, dir)
	build{slug: "mars", title: "Mars", createdAt: "2026-01-15T00:00:00Z", world: "global"}.write(t, dir)

	store, err := oshost.NewVolumes(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	volumes := store.Volumes()
	if len(volumes) != 2 {
		t.Fatalf("%d volumes serving, want 2", len(volumes))
	}
	if got := volumes[0].Info().Slug; got != "mars" {
		t.Errorf("volumes are listed %q first, want them sorted by slug", got)
	}
	tunic := volumes[1].Info()
	if tunic.Release.CreatedAt != "2026-02-01T00:00:00Z" {
		t.Errorf("serving the build of %s, want the newest capture", tunic.Release.CreatedAt)
	}

	payload, err := volumes[1].Blob("tiles/overworld/0/0/0.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "raster" {
		t.Errorf("blob = %q", payload)
	}
	if _, err := volumes[1].Blob("worlds/not-a-world.json"); err == nil {
		t.Error("an entry the bundle does not hold opened")
	}
}

func TestVolumesRescanReportsWhatMoved(t *testing.T) {
	dir := t.TempDir()
	build{slug: "tunic", title: "TUNIC", createdAt: "2026-01-01T00:00:00Z"}.write(t, dir)
	store, err := oshost.NewVolumes(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	if changed, err := store.Rescan(); err != nil || len(changed) != 0 {
		t.Fatalf("a rescan over an unchanged library reported %v, %v", changed, err)
	}

	build{slug: "mars", title: "Mars", createdAt: "2026-01-15T00:00:00Z", world: "global"}.write(t, dir)
	changed, err := store.Rescan()
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "mars" {
		t.Fatalf("rescan reported %v, want just the arrival", changed)
	}
}

func TestVolumesInstall(t *testing.T) {
	library := t.TempDir()
	elsewhere := t.TempDir()
	source := build{slug: "tunic", title: "TUNIC", createdAt: "2026-01-01T00:00:00Z"}.write(t, elsewhere)

	store, err := oshost.NewVolumes(library)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if len(store.Volumes()) != 0 {
		t.Fatal("an empty library is not empty")
	}

	content, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := store.Install(filepath.Base(source), content)
	content.Close()
	if err != nil {
		t.Fatal(err)
	}
	if installed.Slug != "tunic" || installed.Already {
		t.Errorf("install reported %+v, want a first arrival of tunic", installed)
	}
	if len(installed.Changed) != 1 || installed.Changed[0] != "tunic" {
		t.Errorf("install reported %v changed, want tunic", installed.Changed)
	}
	if len(store.Volumes()) != 1 {
		t.Error("the import did not rescan")
	}

	// The same build again: a successful import that copies nothing and
	// changes nothing, because the file name carries the stamp.
	content, err = os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Install(filepath.Base(source), content)
	content.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !again.Already || len(again.Changed) != 0 {
		t.Errorf("reinstalling the same build reported %+v, want a no-op", again)
	}

	if _, err := store.Install("junk.atlas", bytes.NewReader([]byte("not a zip"))); err == nil {
		t.Error("a file that is not a bundle was let into the library")
	}
	if entries, _ := os.ReadDir(library); len(entries) != 1 {
		t.Errorf("the library holds %d files after a refused import, want 1", len(entries))
	}
}

func TestSessionsRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := oshost.NewSessions(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load("volume.tunic.json"); !errors.Is(err, hostenv.ErrNoSession) {
		t.Fatalf("loading a record nobody wrote = %v, want ErrNoSession", err)
	}
	if err := store.Save("volume.tunic.json", []byte(`{"world":"world"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("volume.tunic.json", []byte(`{"world":"other"}`)); err != nil {
		t.Fatal(err)
	}
	held, err := store.Load("volume.tunic.json")
	if err != nil || string(held) != `{"world":"other"}` {
		t.Fatalf("Load = %q, %v, want the second write whole", held, err)
	}

	names, err := store.Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "volume.tunic.json" {
		t.Fatalf("Names = %v", names)
	}
	if err := store.Save("../escape.json", nil); err == nil {
		t.Error("a name that climbs out of the directory was accepted")
	}
}

// Deleting a record, which is the whole of the blunt reset: the arrangement a
// volume opens with is synthesized from the world itself, so a record that is
// gone is a volume that comes back fresh.
//
// The half of the contract easiest to get wrong is the second one. A record
// that is not there is already what the caller asked for, so a missing file is
// success -- otherwise a reader who pressed reset twice would meet an error
// about a file they were right to want gone.
func TestSessionsDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := oshost.NewSessions(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Delete("volume.tunic.json"); err != nil {
		t.Errorf("deleting a record nobody wrote = %v, want a quiet success", err)
	}
	if err := store.Save("volume.tunic.json", []byte(`{"world":"world"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("app.json", []byte(`{"volume":"tunic"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("volume.tunic.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("volume.tunic.json"); !errors.Is(err, hostenv.ErrNoSession) {
		t.Errorf("the record was still readable after a delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "volume.tunic.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record's file is still on disk: %v", err)
	}
	// One record went, not the directory: which volume the reader was last in
	// is a different fact, and it is nobody's to take away here.
	names, err := store.Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "app.json" {
		t.Errorf("Names = %v, want the pointer left standing", names)
	}
	if err := store.Delete("volume.tunic.json"); err != nil {
		t.Errorf("deleting a record twice = %v, want a quiet success", err)
	}
	if err := store.Delete("../escape.json"); err == nil {
		t.Error("a name that climbs out of the directory was accepted")
	}
}

func TestHeadlessHostCannotPickAFile(t *testing.T) {
	host, err := oshost.New(oshost.Options{BundlesDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := host.PickFile(context.Background()); !errors.Is(err, hostenv.ErrNotAvailable) {
		t.Fatalf("PickFile on a host with no window = %v, want ErrNotAvailable", err)
	}
	if _, err := host.Sessions().Names(); err != nil {
		t.Fatalf("a host with no session directory has no sessions, not an error: %v", err)
	}
}
