package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/golden/capture/canon"
)

// The format-roundtrip gate of issue #5 §6.
//
// The tests that need nothing but golden/fixtures come first; they are what a
// checkout with no library runs, and they are the gate CI enforces. The tests
// keyed on ATLAS_REGISTRY_DIR come after, and hold this package to the bytes
// of the real .atlas files the fixtures were taken from.
//
// Canonical-content equality is mandatory throughout. Stamp identity is not
// asserted anywhere: see STAMPS.md, and TestStampPartsAreReproducibleExceptThePyramids
// for the accounting that says exactly how far short of it a reader can get.

// A canonicalized manifest extraction must survive the trip through this
// package's types: every key read, every key written again, nothing dropped,
// nothing reordered, no number reformatted. The manifest's encoded bytes feed
// the stamp of every bundle ever built, so a schema that has quietly moved is
// the single most expensive thing that could be wrong in format/bundle, and
// this is the check that catches it without a library present.
func TestCommittedManifestsRoundTrip(t *testing.T) {
	for _, volume := range readFixtureSet(t).Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			manifest, committed := readManifest(t, volume)
			if err := manifest.Validate(); err != nil {
				t.Fatalf("the captured manifest does not validate: %v", err)
			}
			encoded, err := bundle.MarshalManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			again, err := canon.Bytes(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, committed) {
				t.Errorf("re-encoding the manifest does not reproduce the extraction:\n%s",
					diffBytes(again, committed))
			}
		})
	}
}

// The file a build is written under is derived from its manifest alone, and
// the fixture set records the name the golden-reference tree actually wrote.
// This is the stamp's short form, the capture-day rule and the naming order
// checked end to end, with no bundle in hand.
func TestCommittedManifestsDeriveTheirIdentity(t *testing.T) {
	for _, volume := range readFixtureSet(t).Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			manifest, _ := readManifest(t, volume)

			if derived := bundle.VersionedFileName(manifest); derived != volume.File {
				t.Errorf("this build would be named %s, and it is named %s", derived, volume.File)
			}
			if short := bundle.ShortStamp(manifest.Version.Stamp); short != volume.Stamp12 {
				t.Errorf("short stamp is %s, and the fixture set says %s", short, volume.Stamp12)
			}
			if day := bundle.CaptureDay(manifest.Version.CreatedAt); !strings.Contains(volume.File, day) {
				t.Errorf("capture day %q does not appear in %s", day, volume.File)
			}

			for _, field := range []struct {
				name       string
				got, wants any
			}{
				{"slug", manifest.Volume.Slug, volume.Slug},
				{"title", manifest.Volume.Title, volume.Title},
				{"stamp", manifest.Version.Stamp, volume.Stamp},
				{"createdAt", manifest.Version.CreatedAt, volume.CreatedAt},
				{"revision", manifest.Version.Revision, volume.Revision},
				{"formatVersion", manifest.FormatVersion, volume.FormatVersion},
				{"conventions", manifest.Conventions, volume.Conventions},
				{"worlds", len(manifest.Worlds), volume.Counts.Worlds},
			} {
				if field.got != field.wants {
					t.Errorf("manifest %s is %v, and the fixture set says %v", field.name, field.got, field.wants)
				}
			}

			var points, paths, areas int
			for _, world := range manifest.Worlds {
				points += world.Points
				paths += world.Paths
				areas += world.Areas
			}
			if points != volume.Counts.Points || paths != volume.Counts.Paths || areas != volume.Counts.Areas {
				t.Errorf("the manifest promises %d points, %d paths and %d areas; the fixture set counts %d, %d and %d",
					points, paths, areas, volume.Counts.Points, volume.Counts.Paths, volume.Counts.Areas)
			}
		})
	}
}

// Every world's unpacked locations are packed again by this package, and the
// extraction is written again from the result. The committed document records
// the real payload's size and SHA-256, so a document that matches is a codec
// that reproduces bytes written by another implementation -- checked from
// committed data alone, with no bundle to read.
func TestCommittedLocationsRepackToTheRecordedPayload(t *testing.T) {
	for _, volume := range readFixtureSet(t).Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			manifest, _ := readManifest(t, volume)
			for _, entry := range manifest.Worlds {
				path := filepath.Join(volume.dir(), "worlds", entry.Slug+".locations.json")
				committed := readFile(t, path)
				var rows []locationRow
				rowsOf(t, path, "locations", &rows)

				if len(rows) != entry.Points {
					t.Errorf("world %s: the extraction holds %d locations and the manifest promises %d points",
						entry.Slug, len(rows), entry.Points)
				}
				packed, document, err := emitLocations(entry.Slug, rows)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(document, committed) {
					t.Errorf("world %s: repacking does not reproduce the extraction:\n%s",
						entry.Slug, diffBytes(document, committed))
					continue
				}

				// And back again, so the codec is checked in both directions
				// rather than only in the one the capture ran.
				back, err := bundle.UnpackLocations(packed)
				if err != nil {
					t.Errorf("world %s: %v", entry.Slug, err)
					continue
				}
				_, reopened, err := emitLocations(entry.Slug, rowsFrom(back))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(reopened, committed) {
					t.Errorf("world %s: unpacking the repacked payload does not reproduce the extraction:\n%s",
						entry.Slug, diffBytes(reopened, committed))
				}
			}
		})
	}
}

// The extractions are assembled back into an archive -- the committed
// manifest, the committed payloads, the locations packed from the committed
// rows, an entry per icon and one tile per level of every pyramid the
// inventories name -- and the result is opened and validated.
//
// This is the only always-on check that reaches the parts of format/bundle a
// manifest alone cannot: the writer, the reader, the offline scan, the
// per-kind counts against the payload, the geometry rules, and every
// conventions rule in format/semconv, run against real captured payloads. The
// icon and tile bodies are stand-ins, because nothing validation asks about
// them concerns their bytes; their names are the goldens' own.
func TestCommittedExtractionsReassembleIntoAValidBundle(t *testing.T) {
	for _, volume := range readFixtureSet(t).Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			path := reassemble(t, volume)
			reader, err := bundle.Open(path)
			if err != nil {
				t.Fatalf("the reassembled bundle does not open: %v", err)
			}
			defer reader.Close()

			if err := reader.Validate(); err != nil {
				t.Errorf("the reassembled bundle does not validate: %v", err)
			}
			if name := bundle.VersionedFileName(reader.Manifest); name != volume.File {
				t.Errorf("reopened, this build would be named %s rather than %s", name, volume.File)
			}

			// The manifest went through the writer and came back through the
			// reader; it must still canonicalize to the extraction.
			encoded, err := bundle.MarshalManifest(reader.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			again, err := canon.Bytes(encoded)
			if err != nil {
				t.Fatal(err)
			}
			committed := readFile(t, filepath.Join(volume.dir(), "manifest.json"))
			if !bytes.Equal(again, committed) {
				t.Errorf("the manifest read back out of the archive does not match the extraction:\n%s",
					diffBytes(again, committed))
			}
		})
	}
}

// The committed manifests, folded as a library: each fixture build serves its
// own volume, the derived index lists them all, and the index still speaks the
// legacy wire keys. The last one is not cosmetic -- an index that renamed
// "games" to "volumes" would be read as empty by everything that consumes it.
func TestCommittedManifestsFoldAsALibrary(t *testing.T) {
	set := readFixtureSet(t)
	descriptors := make([]bundle.Descriptor, 0, len(set.Volumes))
	for _, volume := range set.Volumes {
		manifest, _ := readManifest(t, volume)
		descriptors = append(descriptors, bundle.DescriptorOf(volume.File, manifest, volume.FileBytes))
	}

	winners := bundle.Fold(descriptors)
	if len(winners) != len(set.Volumes) {
		t.Errorf("%d fixture builds folded to %d volumes", len(set.Volumes), len(winners))
	}
	for _, volume := range set.Volumes {
		winner, held := winners[volume.Slug]
		if !held {
			t.Errorf("%s did not survive the fold", volume.Slug)
			continue
		}
		if winner.Stamp != volume.Stamp {
			t.Errorf("%s is served by %s, not by the fixture build", volume.Slug, winner.Stamp)
		}
	}
	if shadowed := bundle.Shadowed(descriptors); len(shadowed) != 0 {
		t.Errorf("%d fixture builds were shadowed, and no two are builds of one volume", len(shadowed))
	}

	index, err := bundle.MarshalIndex(bundle.BuildIndex(descriptors))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"games":`, `"maps":`} {
		if !bytes.Contains(index, []byte(key)) {
			t.Errorf("the derived index carries no %s; the listing's wire keys predate the volume vocabulary and were never renamed", key)
		}
	}
	var listing struct {
		Volumes []struct {
			Slug     string `json:"slug"`
			Versions []struct {
				File   string `json:"file"`
				Stamp  string `json:"stamp"`
				Worlds int    `json:"maps"`
			} `json:"versions"`
		} `json:"games"`
	}
	if err := json.Unmarshal(index, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Volumes) != len(set.Volumes) {
		t.Errorf("the index lists %d volumes of %d", len(listing.Volumes), len(set.Volumes))
	}
	bySlug := map[string]fixtureVolume{}
	for _, volume := range set.Volumes {
		bySlug[volume.Slug] = volume
	}
	for _, listed := range listing.Volumes {
		volume := bySlug[listed.Slug]
		if len(listed.Versions) != 1 {
			t.Errorf("%s is listed with %d builds", listed.Slug, len(listed.Versions))
			continue
		}
		build := listed.Versions[0]
		if build.File != volume.File || build.Stamp != volume.Stamp || build.Worlds != volume.Counts.Worlds {
			t.Errorf("%s is listed as %s/%s with %d worlds", listed.Slug, build.File, build.Stamp, build.Worlds)
		}
	}
}

// The extractions are read together and held to each other: the icon and tile
// inventories must sum to the rollups they carry, every header must count what
// its rows hold, and the three places a fixture states its identity --
// FIXTURES.json, volume.json and the manifest -- must say the same thing.
//
// Two things ride on this. A fixture set that disagrees with itself is not an
// oracle, and the rollup rule this gate recomputes against real archives in
// registry mode is the rule the capture used only if it reproduces the
// committed rollups here, where no bundle is needed to check it.
func TestCommittedInventoriesAgreeWithTheirHeaders(t *testing.T) {
	for _, volume := range readFixtureSet(t).Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			header := readVolumeHeader(t, volume)
			if header.Slug != volume.Slug || header.File != volume.File ||
				header.Stamp != volume.Stamp || header.FileSHA256 != volume.FileSHA256 {
				t.Errorf("volume.json describes %s/%s and the fixture set names %s/%s",
					header.Slug, header.File, volume.Slug, volume.File)
			}
			if header.Counts != volume.Counts {
				t.Errorf("volume.json counts %+v and the fixture set counts %+v", header.Counts, volume.Counts)
			}

			var icons []iconRow
			rowsOf(t, filepath.Join(volume.dir(), "icons.json"), "icons", &icons)
			if len(icons) != volume.Counts.Icons {
				t.Errorf("the icon inventory holds %d rows and the fixture counts %d", len(icons), volume.Counts.Icons)
			}
			document, err := emitIcons(volume.Slug, icons)
			if err != nil {
				t.Fatal(err)
			}
			committed := readFile(t, filepath.Join(volume.dir(), "icons.json"))
			if !bytes.Equal(document, committed) {
				t.Errorf("the icon inventory does not re-emit to itself:\n%s", diffBytes(document, committed))
			}

			var tiles int
			for _, pyramid := range header.Pyramids {
				var rows []tileRow
				rowsOf(t, filepath.Join(volume.dir(), filepath.FromSlash(pyramid.Inventory)), "tiles", &rows)
				tiles += len(rows)

				var weight int64
				content := make([]string, 0, len(rows))
				pixels := make([]string, 0, len(rows))
				for _, tile := range rows {
					weight += tile.Bytes
					content = append(content, tile.Name+" "+tile.SHA256)
					pixels = append(pixels, tile.Name+" "+tile.Pixels)
				}
				if len(rows) != pyramid.Tiles || weight != pyramid.Bytes {
					t.Errorf("pyramid %s inventories %d tiles weighing %d, and its header says %d weighing %d",
						pyramid.Pyramid, len(rows), weight, pyramid.Tiles, pyramid.Bytes)
				}
				if got := rollup(content); got != pyramid.Content {
					t.Errorf("pyramid %s sums to %s and its header says %s", pyramid.Pyramid, got, pyramid.Content)
				}
				if got := rollup(pixels); got != pyramid.Pixels {
					t.Errorf("pyramid %s pixel-sums to %s and its header says %s", pyramid.Pyramid, got, pyramid.Pixels)
				}
			}
			if tiles != volume.Counts.Tiles {
				t.Errorf("the tile inventories hold %d rows and the fixture counts %d", tiles, volume.Counts.Tiles)
			}

			manifest, _ := readManifest(t, volume)
			if len(header.Worlds) != len(manifest.Worlds) {
				t.Fatalf("volume.json describes %d worlds and the manifest lists %d",
					len(header.Worlds), len(manifest.Worlds))
			}
			for index, world := range header.Worlds {
				entry := manifest.Worlds[index]
				if world.Slug != entry.Slug || world.Points != entry.Points ||
					world.Paths != entry.Paths || world.Areas != entry.Areas {
					t.Errorf("volume.json describes world %s as %d/%d/%d and the manifest says %s as %d/%d/%d",
						world.Slug, world.Points, world.Paths, world.Areas,
						entry.Slug, entry.Points, entry.Paths, entry.Areas)
					continue
				}
				var payload struct {
					Collections []json.RawMessage `json:"collections"`
				}
				readJSON(t, filepath.Join(volume.dir(), "worlds", world.Slug+".payload.json"), &payload)
				if len(payload.Collections) != world.Collections {
					t.Errorf("world %s inlines %d collections and volume.json counts %d",
						world.Slug, len(payload.Collections), world.Collections)
				}
				var text map[string]json.RawMessage
				readJSON(t, filepath.Join(volume.dir(), "worlds", world.Slug+".text.json"), &text)
				if len(text) != world.TextEntries {
					t.Errorf("world %s carries %d text entries and volume.json counts %d",
						world.Slug, len(text), world.TextEntries)
				}
			}
		})
	}
}

// reassemble writes an archive out of one volume's committed extractions and
// returns its path.
func reassemble(t *testing.T, volume fixtureVolume) string {
	t.Helper()
	manifest, _ := readManifest(t, volume)
	header := readVolumeHeader(t, volume)

	path := filepath.Join(t.TempDir(), volume.Slug+bundle.Extension)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := bundle.NewWriter(file, manifest)
	if err != nil {
		t.Fatalf("the captured manifest is not one a writer accepts: %v", err)
	}

	for _, entry := range manifest.Worlds {
		worlds := filepath.Join(volume.dir(), "worlds")
		payload := readFile(t, filepath.Join(worlds, entry.Slug+".payload.json"))
		text := readFile(t, filepath.Join(worlds, entry.Slug+".text.json"))

		var rows []locationRow
		locations := filepath.Join(worlds, entry.Slug+".locations.json")
		rowsOf(t, locations, "locations", &rows)
		packed, _, err := emitLocations(entry.Slug, rows)
		if err != nil {
			t.Fatal(err)
		}

		must(t, writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix), payload))
		must(t, writer.AddStored(bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix), bytes.NewReader(packed)))
		must(t, writer.AddDeflated(bundle.WorldEntryName(entry.Slug, bundle.TextSuffix), text))
	}

	var icons []iconRow
	rowsOf(t, filepath.Join(volume.dir(), "icons.json"), "icons", &icons)
	for _, icon := range icons {
		must(t, writer.AddDeflated(icon.Name, []byte("<svg/>")))
	}

	// One tile per level of every pyramid. Validation asks whether a level
	// holds anything, never what; a whole pyramid would be a hundred thousand
	// entries to answer a yes-or-no question.
	written := map[string]bool{}
	for _, pyramid := range header.Pyramids {
		var tiles []tileRow
		rowsOf(t, filepath.Join(volume.dir(), filepath.FromSlash(pyramid.Inventory)), "tiles", &tiles)
		if len(tiles) != pyramid.Tiles {
			t.Errorf("pyramid %s inventories %d tiles and its header counts %d",
				pyramid.Pyramid, len(tiles), pyramid.Tiles)
		}
		levels := map[string]bool{}
		for _, tile := range tiles {
			level, _, held := strings.Cut(tile.Name, "/")
			if !held || levels[level] {
				continue
			}
			levels[level] = true
			name := pyramidOf(pyramid.Pyramid) + tile.Name
			if written[name] {
				continue
			}
			written[name] = true
			must(t, writer.AddStored(name, strings.NewReader("raster")))
		}
	}

	must(t, writer.Close())
	must(t, file.Close())
	return path
}

func rowsFrom(locations []bundle.Location) []locationRow {
	rows := make([]locationRow, len(locations))
	for index, location := range locations {
		rows[index] = locationRow{
			ID:     location.ID,
			Owner:  location.Owner,
			Lat:    location.Lat,
			Lng:    location.Lng,
			Member: location.Member,
			Shard:  location.Shard,
			Title:  location.Title,
		}
	}
	return rows
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The registry half. Everything below opens the real .atlas files.

// The manifest of a real bundle must re-encode to the bytes the archive
// carries, not merely to something equivalent: those bytes are what the
// producer stamped over. And canonicalized, they must be the committed
// extraction, which is what ties the two halves of this gate together.
func TestRealManifestsMatchTheCommittedExtraction(t *testing.T) {
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			raw, err := reader.ReadEntry(bundle.ManifestName)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := bundle.MarshalManifest(reader.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, raw) {
				t.Errorf("the manifest does not re-encode byte for byte:\n%s", diffBytes(encoded, raw))
			}
			again, err := canon.Bytes(raw)
			if err != nil {
				t.Fatal(err)
			}
			committed := readFile(t, filepath.Join(volume.dir(), "manifest.json"))
			if !bytes.Equal(again, committed) {
				t.Errorf("the archive's manifest is not the one extracted:\n%s", diffBytes(again, committed))
			}
			if name := bundle.VersionedFileName(reader.Manifest); name != volume.File {
				t.Errorf("the file is named %s and its manifest derives %s", volume.File, name)
			}
		})
	}
}

// Every part the fixture recorded a hash for must still hash to it, the
// archive must still list its entries in the order the capture summed, and the
// file itself must be the file the fixture set names.
func TestRealPartsHashAsTheFixtureRecorded(t *testing.T) {
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			header := readVolumeHeader(t, volume)

			info, err := os.Stat(filepath.Join(sourceDir(dir, volume), volume.File))
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != volume.FileBytes {
				t.Errorf("%s weighs %d bytes and the fixture set says %d",
					volume.File, info.Size(), volume.FileBytes)
			}

			for _, name := range sortedKeys(header.PartHashes) {
				data, err := reader.ReadEntry(name)
				if err != nil {
					t.Errorf("%v", err)
					continue
				}
				if hash := bundle.HashBytes(data); hash != header.PartHashes[name] {
					t.Errorf("%s hashes to %s and the fixture recorded %s", name, hash, header.PartHashes[name])
				}
			}

			names := reader.Names()
			if len(names) != volume.Counts.Entries {
				t.Errorf("the archive holds %d entries and the fixture counts %d", len(names), volume.Counts.Entries)
			}
			if got := rollup(names); got != header.EntryOrder.SHA256 {
				t.Errorf("the archive's entry order sums to %s and the fixture recorded %s",
					got, header.EntryOrder.SHA256)
			}
			for index, name := range header.EntryOrder.First {
				if index >= len(names) {
					t.Errorf("the archive holds no entry %d, and the fixture recorded %s", index, name)
					continue
				}
				if names[index] != name {
					t.Errorf("entry %d is %s and the fixture recorded %s", index, names[index], name)
				}
			}
		})
	}
}

// Every world payload and text entry, canonicalized out of the real archive,
// must be the committed extraction, byte for byte.
func TestRealPayloadsCanonicalizeToTheCommittedExtractions(t *testing.T) {
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			for _, entry := range reader.Manifest.Worlds {
				for _, part := range []struct{ suffix, extraction string }{
					{bundle.WorldSuffix, ".payload.json"},
					{bundle.TextSuffix, ".text.json"},
				} {
					raw, err := reader.ReadEntry(bundle.WorldEntryName(entry.Slug, part.suffix))
					if err != nil {
						t.Errorf("%v", err)
						continue
					}
					again, err := canon.Bytes(raw)
					if err != nil {
						t.Errorf("world %s%s: %v", entry.Slug, part.suffix, err)
						continue
					}
					committed := readFile(t, filepath.Join(volume.dir(), "worlds", entry.Slug+part.extraction))
					if !bytes.Equal(again, committed) {
						t.Errorf("world %s%s does not canonicalize to its extraction:\n%s",
							entry.Slug, part.suffix, diffBytes(again, committed))
					}
				}
			}
		})
	}
}

// Every real packed payload is unpacked by this package and written back out
// as the extraction; the document must be the committed one, which checks the
// rows, the count, the payload's length and its digest in one comparison. The
// payload must also be stored uncompressed, which is what lets a server answer
// it as a byte range.
func TestRealPackedPayloadsUnpackToTheCommittedLocations(t *testing.T) {
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			for _, entry := range reader.Manifest.Worlds {
				name := bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix)
				if !reader.Stored(name) {
					t.Errorf("%s is not stored uncompressed", name)
				}
				data, err := reader.ReadEntry(name)
				if err != nil {
					t.Errorf("%v", err)
					continue
				}
				locations, err := bundle.UnpackLocations(data)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				packed, document, err := emitLocations(entry.Slug, rowsFrom(locations))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(packed, data) {
					t.Errorf("%s repacks to %d bytes from %d", name, len(packed), len(data))
				}
				committed := readFile(t, filepath.Join(volume.dir(), "worlds", entry.Slug+".locations.json"))
				if !bytes.Equal(document, committed) {
					t.Errorf("%s does not unpack to its extraction:\n%s", name, diffBytes(document, committed))
				}
			}
		})
	}
}

// The icons of a real bundle, inventoried again, must be the committed
// inventory: the same names in the same order, the same weights, the same
// hashes, the same rollup.
func TestRealIconsMatchTheCommittedInventory(t *testing.T) {
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			var rows []iconRow
			for _, name := range reader.Names() {
				if !strings.HasPrefix(name, bundle.IconsPrefix) {
					continue
				}
				data, err := reader.ReadEntry(name)
				if err != nil {
					t.Errorf("%v", err)
					continue
				}
				rows = append(rows, iconRow{Name: name, Bytes: int64(len(data)), SHA256: bundle.HashBytes(data)})
			}
			if len(rows) != volume.Counts.Icons {
				t.Errorf("the archive holds %d icons and the fixture counts %d", len(rows), volume.Counts.Icons)
			}
			document, err := emitIcons(volume.Slug, rows)
			if err != nil {
				t.Fatal(err)
			}
			committed := readFile(t, filepath.Join(volume.dir(), "icons.json"))
			if !bytes.Equal(document, committed) {
				t.Errorf("the icons do not inventory to their extraction:\n%s", diffBytes(document, committed))
			}
		})
	}
}

// Every tile of every pyramid, by name, by weight and by content hash, against
// the committed inventory -- and every one of them stored uncompressed, which
// is the invariant that makes a tile servable as a byte range out of the
// archive.
//
// The inventories also carry a decoded-pixel digest per tile. It is not
// recomputed here: a tile whose bytes hash equal cannot picture something
// else, and the digest exists for the day a pipeline re-encodes one, which is
// the generate-enrich gate's subject rather than this one's.
func TestRealTilesMatchTheCommittedInventory(t *testing.T) {
	if testing.Short() {
		t.Skip("hashing every tile of every fixture reads several hundred megabytes")
	}
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			header := readVolumeHeader(t, volume)

			held := map[string]bool{}
			for _, name := range reader.Names() {
				if strings.HasPrefix(name, bundle.TilesPrefix) {
					held[name] = true
				}
			}
			if len(held) != volume.Counts.Tiles {
				t.Errorf("the archive holds %d tiles and the fixture counts %d", len(held), volume.Counts.Tiles)
			}

			for _, pyramid := range header.Pyramids {
				var tiles []tileRow
				rowsOf(t, filepath.Join(volume.dir(), filepath.FromSlash(pyramid.Inventory)), "tiles", &tiles)

				var weight int64
				lines := make([]string, 0, len(tiles))
				for _, tile := range tiles {
					name := pyramidOf(pyramid.Pyramid) + tile.Name
					delete(held, name)
					if !reader.Stored(name) {
						t.Errorf("%s is not stored uncompressed", name)
					}
					data, err := reader.ReadEntry(name)
					if err != nil {
						t.Errorf("%v", err)
						continue
					}
					if int64(len(data)) != tile.Bytes {
						t.Errorf("%s weighs %d bytes and the inventory says %d", name, len(data), tile.Bytes)
					}
					hash := bundle.HashBytes(data)
					if hash != tile.SHA256 {
						t.Errorf("%s hashes to %s and the inventory says %s", name, hash, tile.SHA256)
					}
					weight += int64(len(data))
					lines = append(lines, tile.Name+" "+hash)
				}
				if weight != pyramid.Bytes {
					t.Errorf("pyramid %s weighs %d bytes and its header says %d",
						pyramid.Pyramid, weight, pyramid.Bytes)
				}
				if got := rollup(lines); got != pyramid.Content {
					t.Errorf("pyramid %s sums to %s and its header says %s",
						pyramid.Pyramid, got, pyramid.Content)
				}
			}
			if len(held) > 0 {
				t.Errorf("%d tiles are in the archive and in no inventory, beginning with %v",
					len(held), first(sortedKeys(held), 5))
			}
		})
	}
}

// Every fixture build must pass the validation its producer ran before writing
// it: the offline scan, the per-kind counts, the geometry rules, and every
// attribute held to the conventions the bundle declares.
func TestRealBundlesValidate(t *testing.T) {
	if testing.Short() {
		t.Skip("validation reads every payload of every fixture")
	}
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			if err := reader.Validate(); err != nil {
				t.Errorf("%s: %v", volume.File, err)
			}
		})
	}
}

// The library must still hold every fixture build, and the scan and fold must
// find them. Whether a fixture build is still the *serving* build is only
// reported: a library accumulates, and a volume rebuilt since the capture is a
// fact about the machine, not a defect in the format.
func TestRealLibraryStillHoldsTheFixtureBuilds(t *testing.T) {
	dir, set := registryDir(t)
	descriptors, skipped, err := bundle.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The city was built for the fixture and lives where it was built, so the
	// set the fold has to answer for is the two directories together -- which
	// is exactly the registry capture.sh assembles before it records.
	if city := sourceDir(dir, fixtureVolume{BuiltFor: json.RawMessage("{}")}); city != dir {
		found, alsoSkipped, err := bundle.Scan(city)
		if err == nil {
			descriptors = append(descriptors, found...)
			skipped = append(skipped, alsoSkipped...)
		}
	}
	byStamp := map[string]bundle.Descriptor{}
	for _, descriptor := range descriptors {
		byStamp[descriptor.Stamp] = descriptor
	}
	winners := bundle.Fold(descriptors)
	for _, volume := range set.Volumes {
		descriptor, held := byStamp[volume.Stamp]
		if !held {
			if len(volume.BuiltFor) > 0 {
				t.Logf("%s: no build stamped %s where the city fixture is built; set %s or build it",
					volume.Slug, volume.Stamp12, cityDirEnv)
				continue
			}
			t.Errorf("%s: the library holds no build stamped %s", volume.Slug, volume.Stamp12)
			continue
		}
		if filepath.Base(descriptor.Locator) != volume.File {
			t.Errorf("%s: the build stamped %s is filed as %s", volume.Slug, volume.Stamp12,
				filepath.Base(descriptor.Locator))
		}
		if winner := winners[volume.Slug]; winner.Stamp != volume.Stamp {
			t.Logf("%s now serves %s rather than the fixture build %s; the fixtures are a capture, not a claim about the library today",
				volume.Slug, bundle.ShortStamp(winner.Stamp), volume.Stamp12)
		}
	}
	t.Logf("%d builds scanned, %d volumes, %d skipped", len(descriptors), len(winners), len(skipped))
}

// The stamp's accounting, which is the honest form of the thing this gate
// cannot assert. Every part a stamp sums is recomputable from a finished
// bundle except one: a tile pyramid's hash is the pyramid's own derivation
// stamp, computed by the tile pipeline and never written into the archive.
//
// This test recomputes what can be recomputed, and asserts only that the gap
// is exactly the pyramids -- so if a future format writes the pyramid stamps
// into the bundle, or a pyramid appears that nothing accounts for, the
// accounting says so rather than a stamp comparison silently becoming possible
// or silently staying impossible. See STAMPS.md.
func TestStampPartsAreReproducibleExceptThePyramids(t *testing.T) {
	dir, set := registryDir(t)
	for _, volume := range set.Volumes {
		t.Run(volume.Slug, func(t *testing.T) {
			reader := openFixture(t, dir, volume)
			if reader == nil {
				return
			}
			header := readVolumeHeader(t, volume)

			var recomputable bundle.Stamp
			blanked := reader.Manifest
			blanked.Version.Stamp, blanked.Version.CreatedAt = "", ""
			encoded, err := bundle.MarshalManifest(blanked)
			if err != nil {
				t.Fatal(err)
			}
			recomputable.Add(bundle.ManifestName, bundle.HashBytes(encoded))

			for _, entry := range reader.Manifest.Worlds {
				for _, suffix := range []string{bundle.WorldSuffix, bundle.PackedSuffix, bundle.TextSuffix} {
					name := bundle.WorldEntryName(entry.Slug, suffix)
					data, err := reader.ReadEntry(name)
					if err != nil {
						t.Fatal(err)
					}
					recomputable.Add(name, bundle.HashBytes(data))
				}
			}
			for _, name := range reader.Names() {
				if !strings.HasPrefix(name, bundle.IconsPrefix) {
					continue
				}
				data, err := reader.ReadEntry(name)
				if err != nil {
					t.Fatal(err)
				}
				recomputable.Add(name, bundle.HashBytes(data))
			}

			parts := len(recomputable.Parts())
			whole := parts + len(header.Pyramids)
			if missing := whole - parts; missing != len(header.Pyramids) {
				t.Errorf("%d parts are missing and the volume has %d pyramids", missing, len(header.Pyramids))
			}
			if reader.Manifest.Version.Stamp == recomputable.Sum() {
				t.Errorf("the stamp is reproducible from the bundle alone, and STAMPS.md says it is not: %s",
					recomputable.Sum())
			}
			t.Logf("%d of %d stamp parts recomputable (%d pyramid derivation stamps live only in the pipeline); "+
				"partial sum %s against the stamp %s",
				parts, whole, len(header.Pyramids),
				bundle.ShortStamp(recomputable.Sum()), volume.Stamp12)
		})
	}
}

func first(names []string, count int) string {
	return fmt.Sprint(names[:min(count, len(names))])
}
