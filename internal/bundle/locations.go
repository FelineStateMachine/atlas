package bundle

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	locationMagic = "ATLASLOC"
	// 2 added the shard column. A map split into layers offers one at a time,
	// and without it every layer's locations were drawn over every other.
	locationVersion = 2
)

// Location is one pin as the packed payload carries it. Owner is the position
// of the pin's category in the map's flattened category order, which is the
// only category identity the viewer needs once the payload JSON has listed
// the categories in that same order.
type Location struct {
	ID     int64
	Title  string
	Lat    float64
	Lng    float64
	Region int64
	Shard  int64
	Owner  uint16
}

// PackLocations lays the locations out as parallel arrays, four-byte fields
// first so a reader can view each one directly without copying or realigning.
// Coordinates are single precision, which resolves far finer than a game map
// is drawn.
func PackLocations(locations []Location) []byte {
	count := len(locations)

	titles := make([]byte, 0, count*12)
	offsets := make([]uint32, count+1)
	for index, location := range locations {
		offsets[index] = uint32(len(titles))
		titles = append(titles, location.Title...)
	}
	offsets[count] = uint32(len(titles))

	size := 16 + count*20 + (count+1)*4 + count*2 + len(titles)
	out := make([]byte, size)
	copy(out, locationMagic)
	binary.LittleEndian.PutUint16(out[8:], locationVersion)
	binary.LittleEndian.PutUint32(out[10:], uint32(count))
	// out[14:16] is reserved, and keeps the arrays four-byte aligned.

	at := 16
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
		put32(uint32(int32(location.Region)))
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

// UnpackLocations is the reader of PackLocations, the same layout the viewer
// decodes in the browser. It exists so a change to the packing that the
// viewer could not read fails in Go -- in the bundler's validation and in
// tests -- rather than on the map.
func UnpackLocations(data []byte) ([]Location, error) {
	if len(data) < 16 || string(data[:8]) != locationMagic {
		return nil, fmt.Errorf("location payload is not in the expected form")
	}
	if version := binary.LittleEndian.Uint16(data[8:]); version != locationVersion {
		return nil, fmt.Errorf("location payload is version %d, and this reads %d", version, locationVersion)
	}
	count := int(binary.LittleEndian.Uint32(data[10:]))
	if size := 16 + count*20 + (count+1)*4 + count*2; count < 0 || len(data) < size {
		return nil, fmt.Errorf("location payload is truncated")
	}

	at := 16
	u32 := func(index int) uint32 { return binary.LittleEndian.Uint32(data[at+index*4:]) }
	next := func(bytes int) { at += bytes }

	out := make([]Location, count)
	for index := range out {
		out[index].ID = int64(int32(u32(index)))
	}
	next(count * 4)
	for index := range out {
		out[index].Lat = float64(math.Float32frombits(u32(index)))
	}
	next(count * 4)
	for index := range out {
		out[index].Lng = float64(math.Float32frombits(u32(index)))
	}
	next(count * 4)
	for index := range out {
		out[index].Region = int64(int32(u32(index)))
	}
	next(count * 4)
	for index := range out {
		out[index].Shard = int64(int32(u32(index)))
	}
	next(count * 4)
	offsets := make([]uint32, count+1)
	for index := range offsets {
		offsets[index] = u32(index)
	}
	next((count + 1) * 4)
	for index := range out {
		out[index].Owner = binary.LittleEndian.Uint16(data[at+index*2:])
	}
	next(count * 2)

	titles := data[at:]
	if count > 0 && int(offsets[count]) > len(titles) {
		return nil, fmt.Errorf("location titles are truncated")
	}
	for index := range out {
		out[index].Title = string(titles[offsets[index]:offsets[index+1]])
	}
	return out, nil
}
