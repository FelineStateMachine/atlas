package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The captured rasters, as against the derived ones.
//
// A crawler writes what a tile server answered, tile for tile, under the path
// the server used. Nothing composes those bytes: they are frames, and the
// deriver folds them into a pyramid first. So the vocabulary here is the
// crawler's -- a level, a column, a row, the hash of what came back -- and it
// stops at this package's edge, exactly as the capture vocabulary does.
//
// A record's status says whether the bytes are here. Only "cached" ones are,
// and only those are answered for: a level that answered 404 for its corners
// records the refusal so a re-crawl does not ask again, and a deriver reading
// those records would otherwise count a tile that does not exist.

// TileRef is one captured raster: where it sits in the publisher's own grid, and
// the hash of what came back.
type TileRef struct {
	// TileSet is the publisher's path for the pyramid this belongs to,
	// recovered from the URL it was fetched at.
	TileSet string
	// Zoom, X and Y are the publisher's own numbering, not a local one.
	Zoom, X, Y int
	// ContentHash is the archive's content address of the bytes: SHA-256,
	// lowercase hex. It is what a derivation stamp is taken over, so a deriver
	// never has to read a raster to know whether anything moved.
	ContentHash string
	// SetID is the numbered directory the bytes sit in. It is the layout's, not
	// the vocabulary's, and only Raster needs it.
	SetID int64
}

// Tiles lists a world's captured rasters that are actually on disk, grouped by
// the publisher's pyramid path and then by level, each level sorted by row and
// then column -- the order a level is walked in, and the order its stamp is
// taken in.
//
// A world with no captured raster is not an error. Some worlds are pictures
// somebody else derived, and some are simply not crawled yet.
func (a *Archive) Tiles(w WorldRef) (map[string]map[int][]TileRef, error) {
	var records []struct {
		ContentHash string `json:"contentHash"`
		Status      string `json:"status"`
		TileSetID   int64  `json:"tileSetId"`
		URL         string `json:"url"`
		X           int    `json:"x"`
		Y           int    `json:"y"`
		Zoom        int    `json:"zoom"`
	}
	path := filepath.Join(w.dir, "tiles", "index.json")
	if err := readJSON(path, &records); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s has no tile index", ErrNotReady, w.dir)
		}
		return nil, err
	}
	out := make(map[string]map[int][]TileRef)
	for _, record := range records {
		if record.Status != "cached" || record.ContentHash == "" {
			continue
		}
		set := tileSetOf(record.URL, record.Zoom)
		if set == "" {
			continue
		}
		if out[set] == nil {
			out[set] = make(map[int][]TileRef)
		}
		out[set][record.Zoom] = append(out[set][record.Zoom], TileRef{
			TileSet:     set,
			Zoom:        record.Zoom,
			X:           record.X,
			Y:           record.Y,
			ContentHash: record.ContentHash,
			SetID:       record.TileSetID,
		})
	}
	for _, levels := range out {
		for _, level := range levels {
			sort.Slice(level, func(i, j int) bool {
				if level[i].Y != level[j].Y {
					return level[i].Y < level[j].Y
				}
				return level[i].X < level[j].X
			})
		}
	}
	return out, nil
}

// Raster is where a captured tile's bytes sit, and the encoding the file name
// gives them. A record whose file is gone reports os.ErrNotExist, which a
// deriver treats as a tile that is not there rather than as a failure: an
// archive is filled by hand and an interrupted crawl leaves records ahead of
// bytes.
func (a *Archive) Raster(w WorldRef, t TileRef) (path, format string, err error) {
	pattern := filepath.Join(w.dir, "tiles",
		"set-"+strconv.FormatInt(t.SetID, 10),
		strconv.Itoa(t.Zoom), strconv.Itoa(t.X), strconv.Itoa(t.Y)+".*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", "", err
	}
	if len(matches) == 0 {
		return "", "", os.ErrNotExist
	}
	sort.Strings(matches)
	return matches[0], NormalizeFormat(strings.TrimPrefix(filepath.Ext(matches[0]), ".")), nil
}

// NormalizeFormat spells an encoding the one way the pipeline spells it. JPEG
// answers to two names on disk and to one everywhere else; anything unrecognized
// is read as JPEG, which is what a tile server that names nothing serves.
func NormalizeFormat(value string) string {
	value = strings.ToLower(strings.TrimPrefix(value, "."))
	switch value {
	case "jpeg":
		return "jpg"
	case "png", "webp":
		return value
	default:
		return "jpg"
	}
}

// tileSetOf recovers the publisher's pyramid path from the URL a tile was
// fetched at: everything between the publisher's own marker segment and the
// zoom. The markers are the layout's, kept verbatim like the rest of it --
// MapGenie serves under /games/, IGN under /wikimaps/, and everyone else under
// /tiles/, including the rendered basemaps whose URLs were never fetchable at
// all.
func tileSetOf(rawURL string, zoom int) string {
	for _, marker := range []string{"/games/", "/wikimaps/", "/tiles/"} {
		_, rest, found := strings.Cut(rawURL, marker)
		if !found {
			continue
		}
		path, _, found := strings.Cut(rest, "/"+strconv.Itoa(zoom)+"/")
		if !found {
			continue
		}
		return path
	}
	return ""
}
