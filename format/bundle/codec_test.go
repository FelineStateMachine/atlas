package bundle_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// codecCases are the payloads the packing has to survive. Each is packed,
// unpacked, and packed again: the structs must come back equal and the bytes
// must come back identical, which together say the encoding is total and has
// no slack a producer could vary.
var codecCases = []struct {
	name      string
	locations []bundle.Location
}{
	{"nothing at all", nil},
	{"an empty slice", []bundle.Location{}},
	{"one location", []bundle.Location{{ID: 1, Title: "Origin"}}},
	{"a title-less location", []bundle.Location{{ID: 7}}},
	{"several with owners", []bundle.Location{
		{ID: 1, Title: "Origin", Lat: 12.5, Lng: -3.25, Owner: 0},
		{ID: 2, Title: "Peak", Lat: 80, Lng: 170, Owner: 1},
		{ID: 3, Title: "Trench", Lat: -80, Lng: -170, Owner: 65535},
	}},
	{"members and shards", []bundle.Location{
		{ID: 10, Title: "Inside", Member: 900, Shard: 3},
		{ID: 11, Title: "Outside", Member: 0, Shard: 0},
	}},
	{"negative identities", []bundle.Location{
		{ID: -1, Title: "Below zero", Member: -5, Shard: -2},
	}},
	{"the signed extremes", []bundle.Location{
		{ID: math.MaxInt32, Member: math.MinInt32, Shard: math.MaxInt32, Title: "Edge"},
	}},
	{"titles that are not ascii", []bundle.Location{
		{ID: 1, Title: "Ölüdeniz"},
		{ID: 2, Title: "東京"},
		{ID: 3, Title: "Rio de la Plata \x00 with a null"},
	}},
	{"a long title", []bundle.Location{{ID: 1, Title: strings.Repeat("verylong ", 200)}}},
	{"empty titles between full ones", []bundle.Location{
		{ID: 1, Title: "First"}, {ID: 2, Title: ""}, {ID: 3, Title: "Third"},
	}},
}

func TestCodecRoundTripsBytesAndStructs(t *testing.T) {
	for _, test := range codecCases {
		t.Run(test.name, func(t *testing.T) {
			packed := bundle.PackLocations(test.locations)
			decoded, err := bundle.UnpackLocations(packed)
			if err != nil {
				t.Fatalf("unpack: %v", err)
			}
			if len(decoded) != len(test.locations) {
				t.Fatalf("unpacked %d locations, packed %d", len(decoded), len(test.locations))
			}
			for index, want := range test.locations {
				// Coordinates are single precision on the wire, so the round
				// trip is through float32 and back rather than exact.
				want.Lat = float64(float32(want.Lat))
				want.Lng = float64(float32(want.Lng))
				if !reflect.DeepEqual(decoded[index], want) {
					t.Errorf("location %d came back as %+v, want %+v", index, decoded[index], want)
				}
			}
			again := bundle.PackLocations(decoded)
			if !bytes.Equal(again, packed) {
				t.Errorf("repacking changed %d bytes into %d", len(packed), len(again))
			}
		})
	}
}

func TestPackedHeaderIsTheDocumentedLayout(t *testing.T) {
	locations := []bundle.Location{{ID: 1, Title: "ab"}, {ID: 2, Title: "cde"}}
	data := bundle.PackLocations(locations)

	if got := string(data[:8]); got != bundle.LocationMagic {
		t.Errorf("magic = %q, want %q", got, bundle.LocationMagic)
	}
	if got := binary.LittleEndian.Uint16(data[8:]); got != bundle.LocationVersion {
		t.Errorf("version = %d, want %d", got, bundle.LocationVersion)
	}
	if got := binary.LittleEndian.Uint32(data[10:]); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
	if data[14] != 0 || data[15] != 0 {
		t.Errorf("the reserved bytes carry %v", data[14:16])
	}
	// Header, five uint32 columns, the offsets, the owners, then the titles:
	// 16 + 2*20 + 3*4 + 2*2 + 5.
	if want := 16 + 2*20 + 3*4 + 2*2 + 5; len(data) != want {
		t.Errorf("payload is %d bytes, want %d", len(data), want)
	}
}

func TestPackedViewAgreesWithStructs(t *testing.T) {
	locations := []bundle.Location{
		{ID: 5, Title: "Origin", Lat: 12.5, Lng: -3.25, Member: 2, Shard: 1, Owner: 3},
		{ID: 6, Title: "", Lat: -80, Lng: 170, Owner: 0},
	}
	packed, err := bundle.OpenPacked(bundle.PackLocations(locations))
	if err != nil {
		t.Fatal(err)
	}
	if packed.Len() != len(locations) {
		t.Fatalf("Len = %d, want %d", packed.Len(), len(locations))
	}
	decoded := packed.All()
	for index := range decoded {
		if packed.ID(index) != decoded[index].ID ||
			packed.Lat(index) != decoded[index].Lat ||
			packed.Lng(index) != decoded[index].Lng ||
			packed.Member(index) != decoded[index].Member ||
			packed.Shard(index) != decoded[index].Shard ||
			packed.Owner(index) != decoded[index].Owner ||
			packed.Title(index) != decoded[index].Title {
			t.Errorf("column reads disagree with struct %d: %+v", index, decoded[index])
		}
		if string(packed.TitleBytes(index)) != decoded[index].Title {
			t.Errorf("TitleBytes %d = %q", index, packed.TitleBytes(index))
		}
		if !reflect.DeepEqual(packed.At(index), decoded[index]) {
			t.Errorf("At(%d) = %+v, want %+v", index, packed.At(index), decoded[index])
		}
	}
}

// The view borrows: a title read out of it must be a slice of the payload,
// not a copy, which is the whole reason the raw accessor exists.
func TestPackedTitleBytesBorrow(t *testing.T) {
	data := bundle.PackLocations([]bundle.Location{{ID: 1, Title: "Origin"}})
	packed, err := bundle.OpenPacked(data)
	if err != nil {
		t.Fatal(err)
	}
	title := packed.TitleBytes(0)
	if len(title) == 0 {
		t.Fatal("the title read as empty")
	}
	data[len(data)-len(title)] = 'o'
	if string(title) != "origin" {
		t.Errorf("the title did not track the payload: %q", title)
	}
}

func TestOpenPackedRefusesWhatItCannotRead(t *testing.T) {
	sound := bundle.PackLocations([]bundle.Location{{ID: 1, Title: "Origin"}, {ID: 2, Title: "Peak"}})

	corrupt := func(mutate func([]byte) []byte) []byte {
		copied := append([]byte(nil), sound...)
		return mutate(copied)
	}
	cases := []struct {
		name    string
		data    []byte
		mention string
	}{
		{"nothing", nil, "expected form"},
		{"a short header", sound[:12], "expected form"},
		{"the wrong magic", corrupt(func(d []byte) []byte {
			copy(d, "NOTALOCS")
			return d
		}), "expected form"},
		{"a future version", corrupt(func(d []byte) []byte {
			binary.LittleEndian.PutUint16(d[8:], bundle.LocationVersion+1)
			return d
		}), "version"},
		{"a past version", corrupt(func(d []byte) []byte {
			binary.LittleEndian.PutUint16(d[8:], 2)
			return d
		}), "version"},
		{"a count beyond the bytes", corrupt(func(d []byte) []byte {
			binary.LittleEndian.PutUint32(d[10:], 1<<20)
			return d
		}), "truncated"},
		{"a truncated title region", sound[:len(sound)-3], "truncated"},
		{"offsets that run backwards", corrupt(func(d []byte) []byte {
			// The offsets column starts after the header and five uint32
			// columns of two entries each.
			binary.LittleEndian.PutUint32(d[16+2*20:], 99)
			return d
		}), "backwards"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := bundle.OpenPacked(test.data)
			if err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Errorf("opened with %v, want a complaint about %q", err, test.mention)
			}
		})
	}
}

// A payload written by one build must be readable by another byte for byte,
// so the layout is pinned here as literal bytes rather than as a description
// of itself. Two locations, one owner apart, with a title each.
func TestPackedLayoutIsPinned(t *testing.T) {
	data := bundle.PackLocations([]bundle.Location{
		{ID: 1, Title: "ab", Lat: 1, Lng: -1, Member: 2, Shard: 3, Owner: 0},
		{ID: 2, Title: "c", Lat: 0, Lng: 0, Member: 0, Shard: 0, Owner: 1},
	})
	want := []byte{
		'A', 'T', 'L', 'A', 'S', 'L', 'O', 'C', // magic
		3, 0, // version
		2, 0, 0, 0, // count
		0, 0, // reserved
		1, 0, 0, 0, 2, 0, 0, 0, // id
		0, 0, 128, 63, 0, 0, 0, 0, // lat: 1.0, 0.0
		0, 0, 128, 191, 0, 0, 0, 0, // lng: -1.0, 0.0
		2, 0, 0, 0, 0, 0, 0, 0, // member
		3, 0, 0, 0, 0, 0, 0, 0, // shard
		0, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, // title offsets, count+1 of them
		0, 0, 1, 0, // owner
		'a', 'b', 'c', // titles
	}
	if !bytes.Equal(data, want) {
		t.Errorf("packed as\n%v\nwant\n%v", data, want)
	}
}
