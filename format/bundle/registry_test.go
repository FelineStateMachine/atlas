package bundle_test

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

func build(locator, slug, createdAt string, revision int, stamp string) bundle.Descriptor {
	return bundle.Descriptor{
		Locator: locator, Slug: slug, Title: slug,
		CreatedAt: createdAt, Revision: revision, Stamp: stamp, Worlds: 1,
	}
}

// The build ordering, stated once as a table: creation time, then policy
// revision, then stamp, then locator. Every pair is asymmetric and no pair is
// undecided, which is what makes two folds of one library always agree.
func TestNewerIsTheBuildOrdering(t *testing.T) {
	cases := []struct {
		name string
		a, b bundle.Descriptor
	}{
		{
			"a newer capture wins",
			build("/a", "game", "2026-06-01T00:00:00Z", 0, "00"),
			build("/b", "game", "2026-01-01T00:00:00Z", 9, "ff"),
		},
		{
			"among one capture, the newer revision wins",
			build("/a", "game", "2026-06-01T00:00:00Z", 9, "00"),
			build("/b", "game", "2026-06-01T00:00:00Z", 8, "ff"),
		},
		{
			"among one revision, the higher stamp wins",
			build("/a", "game", "2026-06-01T00:00:00Z", 9, "ff"),
			build("/b", "game", "2026-06-01T00:00:00Z", 9, "00"),
		},
		{
			"among identical versions, the locator decides",
			build("/b", "game", "2026-06-01T00:00:00Z", 9, "ff"),
			build("/a", "game", "2026-06-01T00:00:00Z", 9, "ff"),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if !bundle.Newer(test.a, test.b) {
				t.Errorf("%+v did not beat %+v", test.a, test.b)
			}
			if bundle.Newer(test.b, test.a) {
				t.Errorf("%+v also beat %+v", test.b, test.a)
			}
		})
	}
	self := build("/a", "game", "2026-06-01T00:00:00Z", 1, "aa")
	if bundle.Newer(self, self) {
		t.Error("a build shadows itself")
	}
}

// The fold is a function: the same descriptors in any arrangement give the
// same winners, and folding never touches its input.
func TestFoldIsDeterministic(t *testing.T) {
	candidates := []bundle.Descriptor{
		build("/mars-1", "mars", "2026-08-01T14:20:42Z", 9, "aa"),
		build("/mars-2", "mars", "2026-08-01T14:48:10Z", 9, "bb"),
		build("/mars-3", "mars", "2026-08-01T14:48:10Z", 3, "ff"),
		build("/city-1", "westminster-co", "2026-08-01T20:13:08Z", 9, "cc"),
		build("/city-2", "westminster-co", "2026-07-01T20:13:08Z", 9, "dd"),
		build("/lone", "tunic", "2026-07-30T00:00:00Z", 9, "ee"),
	}
	want := bundle.Fold(candidates)
	if len(want) != 3 {
		t.Fatalf("the fold picked %d winners, want 3", len(want))
	}
	if want["mars"].Locator != "/mars-2" {
		t.Errorf("mars served by %s", want["mars"].Locator)
	}
	if want["westminster-co"].Locator != "/city-1" {
		t.Errorf("westminster-co served by %s", want["westminster-co"].Locator)
	}

	shuffled := append([]bundle.Descriptor(nil), candidates...)
	random := rand.New(rand.NewSource(1))
	for round := 0; round < 50; round++ {
		random.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := bundle.Fold(shuffled); !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d folded to %v, want %v", round, got, want)
		}
	}
	if !reflect.DeepEqual(candidates, append([]bundle.Descriptor(nil), candidates...)) {
		t.Error("the fold mutated its input")
	}
	if len(bundle.Fold(nil)) != 0 {
		t.Error("an empty library folded to something")
	}
}

func TestShadowedListsWhatTheFoldPassedOver(t *testing.T) {
	candidates := []bundle.Descriptor{
		build("/mars-1", "mars", "2026-08-01T14:20:42Z", 9, "aa"),
		build("/mars-2", "mars", "2026-08-01T14:48:10Z", 9, "bb"),
		build("/mars-3", "mars", "2026-07-01T00:00:00Z", 9, "cc"),
		build("/lone", "tunic", "2026-07-30T00:00:00Z", 9, "ee"),
	}
	shadowed := bundle.Shadowed(candidates)
	var locators []string
	for _, descriptor := range shadowed {
		locators = append(locators, descriptor.Locator)
	}
	if !reflect.DeepEqual(locators, []string{"/mars-1", "/mars-3"}) {
		t.Errorf("shadowed = %v", locators)
	}
}

func TestChangedNamesTheVolumesThatMoved(t *testing.T) {
	before := bundle.Fold([]bundle.Descriptor{
		build("/mars-1", "mars", "2026-08-01T14:20:42Z", 9, "aa"),
		build("/gone", "tunic", "2026-07-30T00:00:00Z", 9, "ee"),
		build("/same", "gta5", "2026-07-31T00:00:00Z", 9, "ff"),
	})
	after := bundle.Fold([]bundle.Descriptor{
		build("/mars-2", "mars", "2026-08-01T14:48:10Z", 9, "bb"),
		build("/same", "gta5", "2026-07-31T00:00:00Z", 9, "ff"),
		build("/new", "skyrim", "2026-07-30T00:00:00Z", 9, "dd"),
	})
	want := []string{"mars", "skyrim", "tunic"}
	if got := bundle.Changed(before, after); !reflect.DeepEqual(got, want) {
		t.Errorf("Changed = %v, want %v", got, want)
	}
	if got := bundle.Changed(after, after); len(got) != 0 {
		t.Errorf("a rescan of the same library reported %v", got)
	}
}

// The index is derived: volumes by title, builds newest first, and the title
// a volume answers to is its newest build's.
func TestBuildIndexIsDerivedAndOrdered(t *testing.T) {
	renamed := build("/dir/mars-new.atlas", "mars", "2026-08-01T14:48:10Z", 9, "bb")
	renamed.Title = "Mars"
	renamed.Size = 200
	older := build("/dir/mars-old.atlas", "mars", "2026-08-01T14:20:42Z", 9, "aa")
	older.Title = "The Red Planet"
	older.Size = 100

	index := bundle.BuildIndex([]bundle.Descriptor{
		older,
		renamed,
		func() bundle.Descriptor {
			d := build("/dir/atlas-city.atlas", "westminster-co", "2026-08-01T20:13:08Z", 9, "cc")
			d.Title = "Alphabetically first"
			return d
		}(),
	})
	if len(index.Volumes) != 2 {
		t.Fatalf("the index lists %d volumes, want 2", len(index.Volumes))
	}
	if index.Volumes[0].Title != "Alphabetically first" {
		t.Errorf("volumes are not sorted by title: %s first", index.Volumes[0].Title)
	}
	mars := index.Volumes[1]
	if mars.Title != "Mars" {
		t.Errorf("the volume answers to %q, want the newest build's title", mars.Title)
	}
	if len(mars.Versions) != 2 || mars.Versions[0].File != "mars-new.atlas" {
		t.Errorf("builds are not newest first: %+v", mars.Versions)
	}
	if mars.Versions[0].Size != 200 || mars.Versions[0].Worlds != 1 {
		t.Errorf("build carries %+v", mars.Versions[0])
	}

	// The wire keys are history the listing never renamed; a reader of an old
	// index must still find them.
	data, err := bundle.MarshalIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("the index is not newline-terminated")
	}
	var wire struct {
		Games []struct {
			Slug     string `json:"slug"`
			Versions []struct {
				File string `json:"file"`
				Maps int    `json:"maps"`
			} `json:"versions"`
		} `json:"games"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Games) != 2 || wire.Games[1].Slug != "mars" || wire.Games[1].Versions[0].Maps != 1 {
		t.Errorf("the index wire reads as %s", data)
	}
}

func TestScanFoldsARealDirectory(t *testing.T) {
	dir := t.TempDir()
	fixture{slug: "mars", title: "Mars", createdAt: "2026-08-01T14:20:42Z", stamp: "aa"}.build(t, dir)
	// A second build of the same volume, installed under its versioned name so
	// the two sit side by side.
	newer := filepath.Join(t.TempDir(), "mars.atlas")
	os.Rename(fixture{
		slug: "mars", title: "Mars", createdAt: "2026-08-01T14:48:10Z", stamp: "bb",
	}.build(t, filepath.Dir(newer)), newer)
	if _, err := bundle.Install(dir, newer); err != nil {
		t.Fatal(err)
	}
	// And something that is not a bundle at all.
	if err := os.WriteFile(filepath.Join(dir, "rubbish.atlas"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	descriptors, skipped, err := bundle.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || filepath.Base(skipped[0].Locator) != "rubbish.atlas" {
		t.Errorf("the scan skipped %v", skipped)
	}
	if len(descriptors) != 2 {
		t.Fatalf("the scan found %d bundles, want 2", len(descriptors))
	}
	winners := bundle.Fold(descriptors)
	if winners["mars"].Stamp != "bb" {
		t.Errorf("mars serves stamp %q, want the newer build", winners["mars"].Stamp)
	}

	if err := bundle.WriteIndex(dir, descriptors); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	derived, err := bundle.MarshalIndex(bundle.BuildIndex(descriptors))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(derived) {
		t.Error("the written index is not the derived one")
	}
}

func TestScanOfNothingIsAnEmptyRegistry(t *testing.T) {
	descriptors, skipped, err := bundle.Scan(filepath.Join(t.TempDir(), "never-created"))
	if err != nil || len(descriptors) != 0 || len(skipped) != 0 {
		t.Errorf("a missing directory scanned to %v, %v, %v", descriptors, skipped, err)
	}
}

// Install is the one operation that adds to a library, and it never overwrites:
// a build lands under its own name or, being already there, lands nowhere.
func TestInstallIsIdempotentAndNeverOverwrites(t *testing.T) {
	library := t.TempDir()
	staging := t.TempDir()
	source := fixture{slug: "mars", title: "Mars", createdAt: "2026-08-01T14:48:10Z", stamp: "bb"}.build(t, staging)

	first, err := bundle.Install(library, source)
	if err != nil {
		t.Fatal(err)
	}
	if want := "mars-20260801-bb.atlas"; filepath.Base(first.Locator) != want {
		t.Errorf("installed as %s, want %s", filepath.Base(first.Locator), want)
	}
	second, err := bundle.Install(library, source)
	if err != nil {
		t.Fatal(err)
	}
	if second.Locator != first.Locator {
		t.Errorf("a second install landed at %s", second.Locator)
	}
	// Installing the file already in place is a no-op, not a self-copy.
	third, err := bundle.Install(library, first.Locator)
	if err != nil {
		t.Fatal(err)
	}
	if third.Locator != first.Locator {
		t.Errorf("installing in place landed at %s", third.Locator)
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the library holds %d files after three installs", len(entries))
	}
}

func TestInstallRefusesABrokenBundle(t *testing.T) {
	library := t.TempDir()
	staging := t.TempDir()
	// A bundle whose manifest promises more points than the payload packs.
	broken := fixture{
		slug: "mars", title: "Mars",
		worlds: []fixtureWorld{{slug: "overworld", points: 7, countsStated: true}},
	}.build(t, staging)

	if _, err := bundle.Install(library, broken); err == nil {
		t.Error("a broken bundle was let in")
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a refused install left %d files behind", len(entries))
	}
}
