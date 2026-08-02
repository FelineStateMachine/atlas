// Package tiles reads derived tile pyramids and the stamps that say how they
// were derived.
//
// A lens is one raster pyramid picturing a world. Deriving a pyramid -- finding
// the deepest complete captured level, folding it down, choosing between
// nearest-neighbour and box reduction, recording which tiles exist -- is the
// tile pipeline's work and happens once, into a tile set on disk. Composition
// reads the result: it copies tiles into a bundle and copies the derivation's
// own account of itself into the world payload.
//
// # The derivation stamp
//
// Every pyramid carries a stamp over everything it was derived from: the plan's
// shape (which levels, which window, which encoding), the content hash of every
// source tile that went into it, and a hash of the deriving tool's own source.
// Composition folds that stamp into the bundle's stamp under the part name
//
//	tiles/<pyramid>
//
// and never reads a tile to do it. That is what makes an unchanged volume cheap
// to notice: a rebuild that would write the same bytes computes the same stamp
// without decoding a single raster, and the file it would write is already
// there under a name carrying that stamp.
//
// The mechanism is one-directional on purpose. Composition trusts a pyramid's
// stamp and cannot recompute it, because the inputs -- the captured tiles the
// pyramid was folded down from -- are no longer in front of it. A pyramid whose
// stamp is empty stamps as empty, which is honest: the bundle records that
// nothing was claimed about how those tiles came to be.
//
// # What this package does not do yet
//
// Derivation itself -- frame discovery, the complete-level rule, downsampling,
// coverage bitsets, affine warp variants, offline basemap rendering -- is the
// next wave of this lane. Until it lands, a tile set is read from wherever the
// pipeline left it, and the contract this package publishes is what a deriver
// must fill in. docs/generate.md carries the derivation contract in full.
package tiles

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IndexName is the tile set's own register, beside the pyramids it names.
const IndexName = "index.json"

// Set is an opened tile set: a directory of derived pyramids and the register
// that says what each one is.
type Set struct {
	dir string

	// TileSize is the edge of one tile in pixels, and Size the edge of the
	// world square every pyramid is cut from. They are properties of the whole
	// corpus rather than of one lens, which is why the manifest carries them
	// once and a bundle repeats them once.
	TileSize int
	Size     int

	byTileSet map[string][]Pyramid
	byName    map[string]Pyramid
}

// Pyramid is one derived raster pyramid as its deriver described it.
type Pyramid struct {
	// TileSet is the source path this pyramid was derived from -- the key a
	// source's lens names. Several pyramids may share one: a native picture and
	// an aligned resample of somebody else's raster picture the same ground.
	TileSet string `json:"sourcePath"`
	// Name is the directory the pyramid's tiles sit in, under the tile set
	// root. It is also what a bundle's local pyramid name is derived from.
	Name string `json:"assetPath"`
	// Stamp is the derivation stamp: what these tiles were made from and by.
	Stamp string `json:"stamp"`

	MinZoom    int `json:"minZoom"`
	MaxZoom    int `json:"maxZoom"`
	FullZoom   int `json:"fullZoom"`
	SourceZoom int `json:"sourceZoom"`
	// Window is where in the source tile grid this pyramid was cut from.
	Window Window `json:"grid"`
	// Formats is the file extension of each level, indexed by local zoom.
	Formats []string `json:"formats"`
	// Bounds is the part of the world square the pyramid actually draws.
	Bounds *Box `json:"bounds"`
	// Interpolate says whether the raster is smoothed when magnified: false is
	// pixel art, which is drawn nearest-neighbour.
	Interpolate bool `json:"interpolate"`
	// Background is painted behind the raster, so an omitted tile is invisible
	// rather than a hole.
	Background string `json:"background"`
	// Coverage says which tiles of a level exist, per level. A level with no
	// entry is completely covered.
	Coverage map[string]*Coverage `json:"coverage"`
	// LensName and AlignedWith mark a pyramid that is somebody else's raster
	// resampled into this world: it attaches as one more picture of whichever
	// tile set AlignedWith names.
	LensName    string `json:"name"`
	AlignedWith string `json:"alignedWith"`
}

// Window is a tile window: the zoom it is measured at and its first tile.
type Window struct {
	SourceZoom int `json:"sourceZoom"`
	FirstTile  int `json:"firstTile"`
}

// Box is a rectangle of the world square, in world pixels.
type Box struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Coverage is which tiles of one level exist: a bounding box in tiles, and a
// row-major bitset over it, least significant bit first, base64.
type Coverage struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Bits string `json:"bits"`
}

// Open reads a tile set's register. The path is the register file itself, so a
// caller names one artefact rather than a directory and a convention.
func Open(indexPath string) (*Set, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read tile set: %w", err)
	}
	var register struct {
		TileSize int       `json:"tileSize"`
		Size     int       `json:"size"`
		Pyramids []Pyramid `json:"lenses"`
	}
	if err := json.Unmarshal(data, &register); err != nil {
		return nil, fmt.Errorf("decode %s: %w", indexPath, err)
	}
	if register.TileSize <= 0 || register.Size <= 0 {
		return nil, fmt.Errorf("%s declares no tile grid", indexPath)
	}
	set := &Set{
		dir:       filepath.Dir(indexPath),
		TileSize:  register.TileSize,
		Size:      register.Size,
		byTileSet: make(map[string][]Pyramid),
		byName:    make(map[string]Pyramid, len(register.Pyramids)),
	}
	for _, pyramid := range register.Pyramids {
		set.byName[pyramid.Name] = pyramid
		key := pyramid.TileSet
		if pyramid.AlignedWith != "" {
			key = pyramid.AlignedWith
		}
		set.byTileSet[key] = append(set.byTileSet[key], pyramid)
	}
	return set, nil
}

// Dir is where the tile set's pyramids sit.
func (s *Set) Dir() string { return s.dir }

// Native is the pyramid derived from a source's own tile set, and whether one
// exists. A world whose lens names a tile set nothing derived is not ready to
// compose.
func (s *Set) Native(tileSet string) (Pyramid, bool) {
	for _, pyramid := range s.byTileSet[tileSet] {
		if pyramid.AlignedWith == "" && pyramid.TileSet == tileSet {
			return pyramid, true
		}
	}
	return Pyramid{}, false
}

// Aligned lists the pyramids that are somebody else's raster resampled into the
// world this tile set pictures, in register order. They attach as extra lenses
// after the native ones.
func (s *Set) Aligned(tileSet string) []Pyramid {
	var out []Pyramid
	for _, pyramid := range s.byTileSet[tileSet] {
		if pyramid.AlignedWith == tileSet {
			out = append(out, pyramid)
		}
	}
	return out
}

// Tile is one raster inside a pyramid, named as a bundle entry names it:
// "<z>/<x>/<y>.<ext>".
type Tile struct {
	Name string
	Path string
}

// Tiles lists a pyramid's rasters in the order composition writes them, which
// is the order a directory walk yields them: lexical by path segment, so level
// "10" sorts before level "2". The order is part of the bundle's entry order
// and therefore part of what a reader sees when it lists an archive.
func (s *Set) Tiles(p Pyramid) ([]Tile, error) {
	root := filepath.Join(s.dir, p.Name)
	var out []Tile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, Tile{Name: filepath.ToSlash(rel), Path: path})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk pyramid %s: %w", p.Name, err)
	}
	return out, nil
}

// LocalName is what a pyramid is called inside a bundle: its tile-set name with
// the volume's own prefix taken off, so "tunic__world" travels as "world" and a
// bundle never repeats the volume it is.
func LocalName(volume, pyramid string) string {
	return strings.TrimPrefix(pyramid, volume+"__")
}

// StampPart is the name a pyramid's derivation stamp is added to a bundle's
// stamp under. One part per pyramid, however many tiles it holds.
func StampPart(local string) string { return "tiles/" + local }

// Names lists every pyramid in the set, sorted. It exists for the reports that
// say what a tile set holds.
func (s *Set) Names() []string {
	out := make([]string, 0, len(s.byName))
	for name := range s.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Pyramid looks one up by name.
func (s *Set) Pyramid(name string) (Pyramid, bool) {
	p, ok := s.byName[name]
	return p, ok
}
