package bundle

import (
	"encoding/binary"
	"fmt"
	"math"
)

// The ATLASLOC container. Point features are overwhelmingly numbers, and
// numbers written as text cost several times what they measure, so a world's
// points travel as parallel little-endian arrays rather than as JSON.
const (
	// LocationMagic opens every packed payload.
	LocationMagic = "ATLASLOC"

	// LocationVersion is the packing this package reads and writes.
	//
	// Version 2 added the shard column: a world split into layers offers one
	// at a time, and without it every layer's locations drew over every
	// other. Version 3 moved meaning without moving a byte -- Owner now
	// indexes the world payload's collections array rather than a flattened
	// category order, and the fifth column reads as Member, the id of the
	// area feature containing the point.
	LocationVersion = 3

	// locationHeader is the fixed prologue: 8 bytes of magic, a uint16
	// version, a uint32 count, and two reserved bytes that keep every column
	// that follows four-byte aligned.
	locationHeader = 16
)

// Location is one point feature as the packed payload carries it.
//
// Owner is the position of the feature's collection in the world payload's
// collections array, which is the only collection identity a reader needs
// once the payload has listed the collections in that same order. Member is
// the id of the area feature containing this point, zero for none. Shard is
// the layer of a split world the point belongs to, zero when the world is
// whole.
//
// Lat and Lng are float64 here and single precision on the wire, which
// resolves far finer than a world is drawn. ID, Member, and Shard are int64
// here and signed 32-bit on the wire.
type Location struct {
	ID     int64
	Title  string
	Lat    float64
	Lng    float64
	Member int64
	Shard  int64
	Owner  uint16
}

// PackLocations lays the locations out as parallel arrays: the fixed header,
// then five uint32 columns, then the title offsets, then the uint16 owner
// column, then the concatenated title bytes. Four-byte fields come first so a
// reader can view each column directly without copying or realigning.
//
// The encoding is total and deterministic: the same locations in the same
// order always produce the same bytes.
func PackLocations(locations []Location) []byte {
	count := len(locations)

	titles := make([]byte, 0, count*12)
	offsets := make([]uint32, count+1)
	for index, location := range locations {
		offsets[index] = uint32(len(titles))
		titles = append(titles, location.Title...)
	}
	offsets[count] = uint32(len(titles))

	out := make([]byte, packedSize(count)+len(titles))
	copy(out, LocationMagic)
	binary.LittleEndian.PutUint16(out[8:], LocationVersion)
	binary.LittleEndian.PutUint32(out[10:], uint32(count))
	// out[14:16] is reserved, and keeps the columns four-byte aligned.

	at := locationHeader
	put32 := func(value uint32) {
		binary.LittleEndian.PutUint32(out[at:], value)
		at += 4
	}
	for _, location := range locations {
		put32(uint32(int32(location.ID)))
	}
	for _, location := range locations {
		put32(math.Float32bits(float32(location.Lat)))
	}
	for _, location := range locations {
		put32(math.Float32bits(float32(location.Lng)))
	}
	for _, location := range locations {
		put32(uint32(int32(location.Member)))
	}
	for _, location := range locations {
		put32(uint32(int32(location.Shard)))
	}
	for _, offset := range offsets {
		put32(offset)
	}
	for _, location := range locations {
		binary.LittleEndian.PutUint16(out[at:], location.Owner)
		at += 2
	}
	copy(out[at:], titles)
	return out
}

// UnpackLocations decodes a packed payload into structs. It is the whole-
// payload face of [OpenPacked], for callers that want the locations as
// ordinary Go values.
func UnpackLocations(data []byte) ([]Location, error) {
	packed, err := OpenPacked(data)
	if err != nil {
		return nil, err
	}
	return packed.All(), nil
}

// Packed is a decoded view over an ATLASLOC payload that borrows the caller's
// bytes. Nothing is copied when it opens and nothing is allocated per
// location, so a reader can walk a million points and pay only for the fields
// it actually reads.
//
// The columns are read on demand through encoding/binary rather than
// reinterpreted as typed slices: Go cannot alias a []byte as a []uint32
// without unsafe, and a bundle's bytes arrive at whatever alignment the
// archive gave them. A JavaScript reader building typed-array views over the
// same buffer sees exactly these columns at exactly these offsets, which is
// why the header pads to sixteen bytes.
//
// The view is read-only and must not outlive the bytes it was opened over.
type Packed struct {
	data   []byte
	count  int
	id     int // byte offset of each column within data
	lat    int
	lng    int
	member int
	shard  int
	offset int
	owner  int
	titles int
}

// OpenPacked validates a packed payload's header and returns a view over it.
// It refuses a payload with the wrong magic, an unreadable version, or a
// length that cannot hold the columns its count promises.
func OpenPacked(data []byte) (*Packed, error) {
	if len(data) < locationHeader || string(data[:8]) != LocationMagic {
		return nil, fmt.Errorf("location payload is not in the expected form")
	}
	if version := binary.LittleEndian.Uint16(data[8:]); version != LocationVersion {
		return nil, fmt.Errorf("location payload is version %d, and this reads %d", version, LocationVersion)
	}
	count := int(binary.LittleEndian.Uint32(data[10:]))
	if count < 0 || len(data) < packedSize(count) {
		return nil, fmt.Errorf("location payload is truncated")
	}

	packed := &Packed{data: data, count: count}
	at := locationHeader
	for _, column := range []*int{&packed.id, &packed.lat, &packed.lng, &packed.member, &packed.shard} {
		*column = at
		at += count * 4
	}
	packed.offset = at
	at += (count + 1) * 4
	packed.owner = at
	at += count * 2
	packed.titles = at

	// The title region is variable, so it is the one thing the header cannot
	// bound. Walking the offsets once at open costs a pass over one column and
	// buys a view that can never panic on a corrupt payload, which matters
	// because these bytes arrive from a file somebody else wrote.
	var previous uint32
	for index := 0; index <= count; index++ {
		reach := packed.column(packed.offset, index)
		if reach < previous {
			return nil, fmt.Errorf("location title offsets run backwards at %d", index)
		}
		previous = reach
	}
	if packed.titles+int(previous) > len(data) {
		return nil, fmt.Errorf("location titles are truncated")
	}
	return packed, nil
}

// packedSize is the fixed part of a payload holding count locations,
// everything but the title bytes.
func packedSize(count int) int {
	return locationHeader + count*20 + (count+1)*4 + count*2
}

// Len is how many locations the payload holds.
func (p *Packed) Len() int { return p.count }

// ID, Lat, Lng, Member, Shard, and Owner read one column of one location.
// The index is not bounds-checked beyond what Go's own slicing does.
func (p *Packed) ID(index int) int64     { return int64(int32(p.column(p.id, index))) }
func (p *Packed) Lat(index int) float64  { return float64(math.Float32frombits(p.column(p.lat, index))) }
func (p *Packed) Lng(index int) float64  { return float64(math.Float32frombits(p.column(p.lng, index))) }
func (p *Packed) Member(index int) int64 { return int64(int32(p.column(p.member, index))) }
func (p *Packed) Shard(index int) int64  { return int64(int32(p.column(p.shard, index))) }
func (p *Packed) Owner(index int) uint16 {
	return binary.LittleEndian.Uint16(p.data[p.owner+index*2:])
}

// TitleBytes returns one location's title as a slice of the payload, with no
// copy. The bytes belong to the payload and must not be modified.
func (p *Packed) TitleBytes(index int) []byte {
	start := p.titles + int(p.titleStart(index))
	end := p.titles + int(p.titleEnd(index))
	return p.data[start:end]
}

// Title returns one location's title as a string, which is the one place a
// read allocates.
func (p *Packed) Title(index int) string { return string(p.TitleBytes(index)) }

// At assembles one location as a struct.
func (p *Packed) At(index int) Location {
	return Location{
		ID:     p.ID(index),
		Title:  p.Title(index),
		Lat:    p.Lat(index),
		Lng:    p.Lng(index),
		Member: p.Member(index),
		Shard:  p.Shard(index),
		Owner:  p.Owner(index),
	}
}

// All assembles every location as a struct. Packing the result again
// reproduces the payload byte for byte.
func (p *Packed) All() []Location {
	out := make([]Location, p.count)
	for index := range out {
		out[index] = p.At(index)
	}
	return out
}

func (p *Packed) column(base, index int) uint32 {
	return binary.LittleEndian.Uint32(p.data[base+index*4:])
}

func (p *Packed) titleStart(index int) uint32 { return p.column(p.offset, index) }
func (p *Packed) titleEnd(index int) uint32   { return p.column(p.offset, index+1) }
