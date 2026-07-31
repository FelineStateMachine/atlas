package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// A layer usually publishes the window its tiles occupy at each zoom, and the
// crawl asks for what is inside it. Some games publish none at all -- Fallout
// 76 and Marathon among them -- and a crawl with no window to read had nothing
// to ask for and captured nothing.
//
// What every map does say is where it opens, and that point is on the map by
// definition. So the shallowest level is found by fetching the tile under it
// and spreading outward from whatever answers, until the frontier runs out.
// The levels below need no window of their own: they follow the tiles that
// held content, which is what a published layer does after its first level too.

// searchBudget caps the spread. The shallowest level of a map is a handful of
// tiles -- Appalachia is one -- so a search that has read this many has
// misread something, and should stop rather than walk the whole world.
const searchBudget = 512

// foundWindow is where a layer's tiles turned out to be, and which way round
// they had to be asked for to find them.
type foundWindow struct {
	window     tileWindow
	transposed bool
	found      bool
}

// searchWindow spreads outward from the tile the map opens on. Neighbours are
// taken in all eight directions, so a coastline meeting a corner does not end
// the search early.
func searchWindow(
	ctx context.Context,
	fetcher *fetcher,
	base string,
	set apiTileSet,
	zoom int,
	centre [2]int,
	extension string,
) (foundWindow, error) {
	// The two axes are published the other way round on some layers, and with
	// no window to compare neighbours in there is nothing yet to measure that
	// against. The tile the map opens on settles it instead: whichever way
	// round answers is the way the search reads the rest.
	for _, transposed := range []bool{false, true} {
		exists, err := tileExists(ctx, fetcher, base, set, zoom, centre, extension, transposed)
		if err != nil {
			return foundWindow{}, err
		}
		if !exists {
			continue
		}
		window, err := spread(ctx, fetcher, base, set, zoom, centre, extension, transposed)
		if err != nil {
			return foundWindow{}, err
		}
		return foundWindow{window: window, transposed: transposed, found: true}, nil
	}
	return foundWindow{}, nil
}

func spread(
	ctx context.Context,
	fetcher *fetcher,
	base string,
	set apiTileSet,
	zoom int,
	centre [2]int,
	extension string,
	transposed bool,
) (tileWindow, error) {
	span := 1 << zoom
	window := tileWindow{minX: centre[0], minY: centre[1], maxX: centre[0], maxY: centre[1]}
	tested := map[[2]int]bool{centre: true}
	queue := [][2]int{centre}

	for len(queue) > 0 && len(tested) <= searchBudget {
		at := queue[0]
		queue = queue[1:]
		if at != centre {
			exists, err := tileExists(ctx, fetcher, base, set, zoom, at, extension, transposed)
			if err != nil {
				return tileWindow{}, err
			}
			if !exists {
				continue
			}
		}
		window.minX, window.minY = min(window.minX, at[0]), min(window.minY, at[1])
		window.maxX, window.maxY = max(window.maxX, at[0]), max(window.maxY, at[1])
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				next := [2]int{at[0] + dx, at[1] + dy}
				if (dx == 0 && dy == 0) || tested[next] ||
					next[0] < 0 || next[1] < 0 || next[0] >= span || next[1] >= span {
					continue
				}
				tested[next] = true
				queue = append(queue, next)
			}
		}
	}
	return window, nil
}

func tileExists(
	ctx context.Context,
	fetcher *fetcher,
	base string,
	set apiTileSet,
	zoom int,
	at [2]int,
	extension string,
	transposed bool,
) (bool, error) {
	first, second := at[0], at[1]
	if transposed {
		first, second = second, first
	}
	url := fmt.Sprintf("%s/games/%s/%d/%d/%d.%s", base, set.Path, zoom, first, second, extension)
	if _, _, err := fetcher.get(ctx, url); err != nil {
		if errors.Is(err, errAbsent) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// childWindow is where the next level down can hold anything, given what the
// level above turned out to hold. It stands in for a published window on a
// layer that has none.
func childWindow(live map[[2]int]bool) (tileWindow, bool) {
	first := true
	var window tileWindow
	for parent := range live {
		left, top := parent[0]*2, parent[1]*2
		if first {
			window = tileWindow{minX: left, minY: top, maxX: left + 1, maxY: top + 1}
			first = false
			continue
		}
		window.minX, window.minY = min(window.minX, left), min(window.minY, top)
		window.maxX, window.maxY = max(window.maxX, left+1), max(window.maxY, top+1)
	}
	return window, !first
}

// startTile is the tile holding the point the map opens on, in the same
// projection the tile pyramid is cut in.
func startTile(full *apiMapFull, zoom int) ([2]int, bool) {
	latitude, ok := numberValue(full.InitialLatitude)
	if !ok {
		return [2]int{}, false
	}
	longitude, ok := numberValue(full.InitialLongitude)
	if !ok {
		return [2]int{}, false
	}
	span := float64(int(1) << zoom)
	x := (longitude + 180) / 360 * span
	y := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * span
	if math.IsNaN(x) || math.IsNaN(y) {
		return [2]int{}, false
	}
	limit := (1 << zoom) - 1
	return [2]int{
		min(max(int(x), 0), limit),
		min(max(int(y), 0), limit),
	}, true
}

// numberValue reads a JSON number that the archive has also been seen to quote.
func numberValue(raw json.RawMessage) (float64, bool) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
