package oshost_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
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
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Volume:        bundle.Volume{Slug: b.slug, Title: b.title},
		Version: bundle.Version{
			Stamp:     bundle.HashBytes([]byte(b.slug + b.createdAt + b.world)),
			CreatedAt: b.createdAt,
			Revision:  b.revision,
		},
		TileGrid: bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
		Worlds: []bundle.WorldEntry{{
			Slug: b.world, Title: b.title + " ground", Points: 1, UpdatedAt: b.createdAt,
		}},
	}
	path := filepath.Join(dir, bundle.VersionedFileName(manifest))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatal(err)
	}
	detail := `{"lenses":[{"tiles":"` + b.world + `","minZoom":0,"maxZoom":0,"formats":["jpg"]}],` +
		`"collections":[{"id":1,"title":"Marker","kind":"point","visible":true}]}`
	if err := writer.AddDeflated(bundle.WorldEntryName(b.world, bundle.WorldSuffix), []byte(detail)); err != nil {
		t.Fatal(err)
	}
	packed := bundle.PackLocations([]bundle.Location{{ID: 1, Title: "Origin"}})
	if err := writer.AddStored(bundle.WorldEntryName(b.world, bundle.PackedSuffix), bytes.NewReader(packed)); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDeflated(bundle.WorldEntryName(b.world, bundle.TextSuffix), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddStored(bundle.TilesPrefix+b.world+"/0/0/0.jpg", bytes.NewReader([]byte("raster"))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// The fold is format/bundle's; what this checks is that the walk hands it
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

	volumes := store.Volumes()
	if len(volumes) != 2 {
		t.Fatalf("%d volumes serving, want 2", len(volumes))
	}
	if got := volumes[0].Manifest().Volume.Slug; got != "mars" {
		t.Errorf("volumes are listed %q first, want them sorted by slug", got)
	}
	tunic := volumes[1].Manifest()
	if tunic.Version.CreatedAt != "2026-02-01T00:00:00Z" {
		t.Errorf("serving the build of %s, want the newest capture", tunic.Version.CreatedAt)
	}

	entry, size, err := volumes[1].Open(bundle.WorldEntryName("overworld", bundle.WorldSuffix))
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	payload, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(payload)) != size {
		t.Errorf("entry announced %d bytes and read %d", size, len(payload))
	}
	if _, _, err := volumes[1].Open("worlds/not-a-world.json"); err == nil {
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
