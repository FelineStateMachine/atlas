package bundle_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// The fuzz targets. A bundle's bytes arrive from a file somebody else wrote
// and a packed payload's bytes arrive from inside one, so both readers are
// held to the only promise arbitrary bytes allow: never panic, and answer
// garbage with an error rather than with a view over memory that is not
// there. The seeds live here in code rather than in a committed corpus --
// each one is a valid artifact built through the real writer, plus the
// corruptions of it worth steering the fuzzer toward.

// seedBundle writes a tiny valid bundle through the real writer, the same
// shape the synthetic fixtures build, so the seed corpus starts from bytes a
// reader must accept rather than from bytes it may refuse.
func seedBundle(f *testing.F) []byte {
	f.Helper()
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Volume:        bundle.Volume{Slug: "seed", Title: "Seed"},
		Version:       bundle.Version{Stamp: bundle.HashBytes([]byte("seed")), CreatedAt: "2026-01-01T00:00:00Z"},
		TileGrid:      bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
		Worlds:        []bundle.WorldEntry{{Slug: "overworld", Title: "Overworld", Points: 1, UpdatedAt: "2026-01-01T00:00:00Z"}},
	}
	var out bytes.Buffer
	writer, err := bundle.NewWriter(&out, manifest)
	if err != nil {
		f.Fatal(err)
	}
	detail := "{" + fixtureLenses + "," + fixturePointCollection + "}"
	packed := bundle.PackLocations([]bundle.Location{{ID: 1, Title: "Origin"}})
	steps := []error{
		writer.AddDeflated(bundle.WorldEntryName("overworld", bundle.WorldSuffix), []byte(detail)),
		writer.AddStored(bundle.WorldEntryName("overworld", bundle.PackedSuffix), bytes.NewReader(packed)),
		writer.AddDeflated(bundle.WorldEntryName("overworld", bundle.TextSuffix), []byte("{}")),
		writer.AddStored(bundle.TilesPrefix+"overworld/0/0/0.jpg", bytes.NewReader([]byte("raster"))),
		writer.AddDeflated(bundle.IconsPrefix+"marker.svg", []byte("<svg/>")),
		writer.Close(),
	}
	for _, err := range steps {
		if err != nil {
			f.Fatal(err)
		}
	}
	return out.Bytes()
}

// The reader over arbitrary bytes. Whatever arrives, it must not panic; when
// it does open something, what opened is a bundle whose manifest already
// validated, and walking it -- validation, names, every entry -- must hold to
// the same standard.
func FuzzReader(f *testing.F) {
	seed := seedBundle(f)
	f.Add(seed)
	f.Add(seed[:len(seed)/2])
	f.Add(seed[len(seed)/2:])
	flipped := bytes.Clone(seed)
	flipped[len(flipped)/3] ^= 0xff
	f.Add(flipped)
	f.Add([]byte{})
	f.Add([]byte("not a zip at all"))
	f.Add([]byte("PK\x03\x04 but nothing behind the signature"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// A zip inflates up to about a thousandfold, so bounding the input
		// bounds what a hostile archive can make Validate read.
		if len(data) > 1<<16 {
			return
		}
		reader, err := bundle.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		// It opened, so the manifest is one this package accepts; everything
		// downstream must cope with whatever the rest of the archive holds.
		if err := reader.Manifest.Validate(); err != nil {
			t.Fatalf("a reader opened over a manifest it refuses: %v", err)
		}
		_ = bundle.VersionedFileName(reader.Manifest)
		_ = reader.Descriptor()
		_ = reader.Validate()
		for _, name := range reader.Names() {
			_, _ = reader.ReadEntry(name)
			_ = reader.Stored(name)
		}
	})
}

// The packed-locations codec over arbitrary bytes: opening must never panic,
// and any payload that does open must round-trip -- unpack, pack, unpack --
// to the same locations and then to identical bytes, so the packing is a
// fixed point rather than a drift.
func FuzzPackedLocations(f *testing.F) {
	empty := bundle.PackLocations(nil)
	full := bundle.PackLocations([]bundle.Location{
		{ID: 1, Title: "Origin"},
		{ID: -7, Title: "Ölüdeniz", Lat: 12.5, Lng: -3.25, Member: 900, Shard: 3, Owner: 65535},
	})
	f.Add(empty)
	f.Add(full)
	f.Add(full[:len(full)-3])
	f.Add(full[:8])
	miscounted := bytes.Clone(full)
	miscounted[10] = 0xff // a count the payload's length cannot hold
	f.Add(miscounted)
	f.Add([]byte("ATLASLOC"))
	f.Add([]byte{})
	// The fuzzer's own first find: a well-formed payload whose latitude
	// column spells a signaling NaN, which is why locations compare by bits
	// below rather than by ==.
	f.Add([]byte("ATLASLOC\x03\x00\x02\x00\x00\x0000000000000000000000\xff\xff" +
		"00000000000000000000\x00\x00\x00\x00\x06\x00\x00\x00\x10\x00\x00\x00" +
		"00000000000000000000"))

	f.Fuzz(func(t *testing.T, data []byte) {
		locations, err := bundle.UnpackLocations(data)
		if err != nil {
			return
		}
		packed := bundle.PackLocations(locations)
		again, err := bundle.UnpackLocations(packed)
		if err != nil {
			t.Fatalf("a payload this package packed does not unpack: %v", err)
		}
		if !sameLocations(again, locations) {
			t.Fatalf("%d locations came back as %d different ones", len(locations), len(again))
		}
		if final := bundle.PackLocations(again); !bytes.Equal(final, packed) {
			t.Fatalf("packing is not a fixed point: %d bytes, then %d", len(packed), len(final))
		}
	})
}

// sameLocations compares two decodings field for field, with coordinates
// compared by their bits: a payload may carry a NaN, and a NaN that survives
// the trip is a round-trip even though it will not say so under ==.
func sameLocations(got, want []bundle.Location) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		g, w := got[index], want[index]
		if g.ID != w.ID || g.Title != w.Title || g.Member != w.Member ||
			g.Shard != w.Shard || g.Owner != w.Owner ||
			math.Float64bits(g.Lat) != math.Float64bits(w.Lat) ||
			math.Float64bits(g.Lng) != math.Float64bits(w.Lng) {
			return false
		}
	}
	return true
}

// The codec over arbitrary values: whatever a producer holds, packing and
// unpacking must return it up to the wire's own documented narrowing --
// coordinates to single precision, identities to 32 bits -- and never panic
// on a title that is not text.
func FuzzLocationsSurviveThePacking(f *testing.F) {
	f.Add(int64(1), 12.5, -3.25, int64(0), int64(0), uint16(0), "Origin", "")
	f.Add(int64(-1), -80.0, 170.0, int64(-5), int64(3), uint16(65535), "Ölüdeniz", "東京")
	f.Add(int64(math.MaxInt64), math.Inf(1), math.NaN(), int64(math.MinInt64), int64(math.MaxInt32), uint16(7), "\x00\xff not utf-8", "")

	f.Fuzz(func(t *testing.T, id int64, lat, lng float64, member, shard int64, owner uint16, title, second string) {
		locations := []bundle.Location{
			{ID: id, Title: title, Lat: lat, Lng: lng, Member: member, Shard: shard, Owner: owner},
			{ID: id + 1, Title: second},
		}
		back, err := bundle.UnpackLocations(bundle.PackLocations(locations))
		if err != nil {
			t.Fatalf("a payload this package packed does not unpack: %v", err)
		}
		if len(back) != len(locations) {
			t.Fatalf("%d locations came back as %d", len(locations), len(back))
		}
		want := bundle.Location{
			ID:     int64(int32(id)),
			Title:  title,
			Lat:    float64(float32(lat)),
			Lng:    float64(float32(lng)),
			Member: int64(int32(member)),
			Shard:  int64(int32(shard)),
			Owner:  owner,
		}
		got := back[0]
		if got.ID != want.ID || got.Title != want.Title || got.Member != want.Member ||
			got.Shard != want.Shard || got.Owner != want.Owner {
			t.Fatalf("location came back as %+v, want %+v", got, want)
		}
		// Coordinates compare by bits, so a NaN that went in comes out the
		// same NaN rather than failing its own equality.
		if math.Float64bits(got.Lat) != math.Float64bits(want.Lat) ||
			math.Float64bits(got.Lng) != math.Float64bits(want.Lng) {
			t.Fatalf("coordinates came back as (%v, %v), want (%v, %v)", got.Lat, got.Lng, want.Lat, want.Lng)
		}
	})
}
