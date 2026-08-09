package authoring

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/minting"
)

func TestNativeMinterPersistsOneRecipeAndReturnsTheInstalledVolume(t *testing.T) {
	root := t.TempDir()
	minter, err := NewNativeMinter(NativeMinterOptions{
		RecipesDir: filepath.Join(root, "recipes"),
		CacheDir:   filepath.Join(root, "cache"),
		LibraryDir: filepath.Join(root, "library"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var built BuildOptions
	minter.build = func(_ context.Context, options BuildOptions) (BuildResult, error) {
		built = options
		if options.Event != nil {
			options.Event(Event{Stage: "publish", Message: "native build installed", Artifact: "/library/my-region.atlas"})
		}
		return BuildResult{Descriptor: vnext.Descriptor{Slug: "my-region", Title: "My Region", Stamp: "abc123"}}, nil
	}
	request := minting.Request{
		Title: "My Region", Bounds: [4]float64{-105.1, 39.6, -104.9, 39.8}, DetailZoom: 12,
		Topo: true, Roads: true, Hydro: true, Counties: true,
	}
	var events []minting.Event
	result, err := minter.Mint(context.Background(), request, func(event minting.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Slug != "my-region" || result.World != "my-region" || result.Title != "My Region" || result.Stamp != "abc123" {
		t.Fatalf("mint result = %+v", result)
	}
	if built.CacheDir != filepath.Join(root, "cache") || built.LibraryDir != filepath.Join(root, "library") || built.ProjectPath != filepath.Join(root, "recipes", "my-region"+ProjectExtension) {
		t.Fatalf("build options = %+v", built)
	}
	project, err := LoadProject(built.ProjectPath)
	if err != nil {
		t.Fatalf("load managed recipe: %v", err)
	}
	if project.ID != "my-region" || project.Title != "My Region" {
		t.Fatalf("managed recipe identity = %q / %q", project.ID, project.Title)
	}
	entries, err := os.ReadDir(filepath.Join(root, "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "my-region"+ProjectExtension {
		t.Fatalf("managed recipes = %v", entries)
	}
	if len(events) != 1 || events[0].Stage != "publish" || events[0].Artifact == "" {
		t.Fatalf("mint events = %+v", events)
	}
}

func TestNativeMinterPreviewUsesTheSameRecipeAsMint(t *testing.T) {
	minter, err := NewNativeMinter(NativeMinterOptions{
		RecipesDir: filepath.Join(t.TempDir(), "recipes"), CacheDir: t.TempDir(), LibraryDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := minter.Default()
	request.Title = "A Map"
	preview, err := minter.Preview(request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Pixels < 1 || preview.RasterTiles < 1 || preview.Requests != 7 || preview.EstimatedBytes < 1 {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestNativeMinterPublishesARevisionPastEveryExistingBuild(t *testing.T) {
	descriptors := []vnext.Descriptor{
		{Slug: "my-map", Revision: 2},
		{Slug: "another-map", Revision: 99},
		{Slug: "my-map", Revision: 7},
	}
	if got := nextMintRevision(descriptors, "my-map"); got != 8 {
		t.Fatalf("next revision = %d, want 8", got)
	}
	if got := nextMintRevision(descriptors, "new-map"); got != 1 {
		t.Fatalf("new revision = %d, want 1", got)
	}
}
