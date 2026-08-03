package tiles

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/FelineStateMachine/atlas/internal/generate/tiles/basemap"
)

// A drawn level, and what holds it honest.
//
// Most pictures in the corpus arrive as tiles from somebody's tile server, and
// the deriver's deepest level is those tiles copied through. A city has no tile
// server: what a municipal hub publishes is geometry, so the deepest level is
// drawn here, from that geometry, and the pyramid folds down from it exactly as
// any other pyramid does. Nothing below this file knows the difference.
//
// The archive still holds the rasters, because the crawl that first drew them
// wrote them down, and the plan still names their content hashes. That makes
// the capture a *witness*: every tile this deriver draws must hash to what the
// archive recorded, or the derivation fails naming the tile. It is the strongest
// statement available about a renderer -- not "these bytes look right" but
// "these bytes are the bytes a different implementation produced from the same
// vectors" -- and it is checked on every derivation rather than in a test that
// could be skipped.

// drawing is a level's shapes, prepared once for the level they are drawn at.
type drawing struct {
	renderer *basemap.Renderer
}

// drawingFor prepares a plan's drawing, or answers nil when the plan's picture
// is fetched rather than drawn.
func drawingFor(plan Plan, fullZoom int) (*drawing, error) {
	if plan.Drawing == nil {
		return nil, nil
	}
	// The drawing says which level it is drawn at, and the plan says which
	// level is folded down from. A disagreement would put the linework at the
	// wrong scale everywhere, silently, so it is a refusal.
	if plan.Drawing.Zoom != fullZoom {
		return nil, fmt.Errorf(
			"pyramid %s is drawn at local zoom %d but folds down from level %d",
			plan.Name, plan.Drawing.Zoom, fullZoom)
	}
	shapes := make([]basemap.Feature, 0, len(plan.Drawing.Shapes))
	for _, shape := range plan.Drawing.Shapes {
		shapes = append(shapes, basemap.Feature{
			Role:     basemap.Role(shape.Role),
			Rings:    shape.Rings,
			Lines:    shape.Lines,
			Emphasis: shape.Emphasis,
		})
	}
	return &drawing{renderer: basemap.NewRenderer(shapes, plan.Drawing.Zoom)}, nil
}

// level draws every tile of one level, writes the ones that are not the
// level's background, and holds each against the capture that witnessed it.
//
// Every tile is drawn, including the ones that are omitted as background: the
// point of the witness is that the whole level reproduces, and a tile skipped
// before it was drawn is a tile nothing checked.
func (d *drawing) level(
	root, name string,
	localZoom, origin int,
	tiles []Tile,
	filler string,
	mask *coverageMask,
) error {
	// Drawing is the expensive half of deriving a city, and every tile is
	// independent of every other, so the level is drawn across the machine.
	// Nothing about the result depends on the order they finish in.
	workers := min(runtime.NumCPU(), len(tiles))
	if workers < 1 {
		workers = 1
	}
	var (
		next    = make(chan Tile)
		mu      sync.Mutex
		failure error
		group   sync.WaitGroup
	)
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if failure == nil {
			failure = err
		}
	}

	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for tile := range next {
				x, y := tile.Ref.X-origin, tile.Ref.Y-origin
				body, err := basemap.EncodePNG(d.renderer.Tile(x, y))
				if err != nil {
					fail(fmt.Errorf("draw %d/%d/%d: %w", localZoom, x, y, err))
					continue
				}
				sum := sha256.Sum256(body)
				if got := hex.EncodeToString(sum[:]); got != tile.Ref.ContentHash {
					fail(fmt.Errorf(
						"drawn tile %d/%d/%d hashes to %s, the capture that witnessed it recorded %s "+
							"(the renderer and the capture must agree; see docs/generate.md §4.4)",
						localZoom, x, y, got, tile.Ref.ContentHash))
					continue
				}
				if filler != "" && tile.Ref.ContentHash == filler {
					continue
				}
				if err := writeTile(tilePath(root, name, localZoom, x, y, "png"), body); err != nil {
					fail(err)
					continue
				}
				mu.Lock()
				mask.mark(x, y)
				mu.Unlock()
			}
		}()
	}
	for _, tile := range tiles {
		next <- tile
	}
	close(next)
	group.Wait()
	return failure
}

// writeTile puts one drawn tile where a copied one would have gone.
func writeTile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
