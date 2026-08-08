package compose

import (
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// worldGrid and lens are the generator's staging model. They are not a wire
// format: nativeVolume turns them into CoordinateSpace and RasterPyramid rows.
type worldGrid struct {
	SourceZoom int
	FirstTile  int
}

type lens struct {
	Name        string
	Tiles       string
	MinZoom     int
	MaxZoom     int
	FullZoom    int
	SourceZoom  int
	Formats     []string
	Bounds      *tiles.Box
	Surface     *tiles.Box
	Interpolate bool
	Background  string
	Shard       int64
	Coverage    map[string]*tiles.Coverage
}

type origin struct {
	Source        string `json:"source"`
	Slug          string `json:"slug,omitempty"`
	Origin        bool   `json:"origin,omitempty"`
	DonorFeatures counts `json:"donorFeatures"`
	Added         int    `json:"added"`
}

type counts struct {
	Point int `json:"point"`
	Path  int `json:"path"`
	Area  int `json:"area"`
}

func tally(collections []composedCollection) counts {
	var out counts
	for _, collection := range collections {
		switch collection.Kind {
		case doc.KindPoint:
			out.Point += len(collection.Features)
		case doc.KindPath:
			out.Path += len(collection.Features)
		default:
			out.Area += len(collection.Features)
		}
	}
	return out
}
