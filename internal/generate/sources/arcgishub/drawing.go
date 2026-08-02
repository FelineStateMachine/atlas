package arcgishub

import "github.com/FelineStateMachine/atlas/internal/generate/doc"

// The city's picture, stated as shapes.
//
// A municipal open-data hub publishes geometry and no tiles, so the ground a
// city's bundle stands on is drawn from that geometry rather than fetched. What
// this file produces is the drawing the tile deriver rasterizes: every curated
// layer that carries a role, projected onto the world square through the very
// window the pins are projected through, so the raster and the features cannot
// disagree about where the ground is.
//
// It is not the same pass as shapeCollections, and deliberately so. A
// collection is what a reader clicks; a drawing is what the city looks like.
// Every road centreline draws and none of them is a legend row; only named
// trails are legend rows and all trail segments draw; a shape's emphasis varies
// row by row where a collection's features are buckets of many rows.

// drawShapes builds the drawing from a normalized capture, reading each layer's
// role and emphasis out of the same curation entry the rest of the layer's
// judgement lives in.
//
// The order is the capture's own canonical order -- datasets by slug, rows by
// id -- and not the curation table's, which is the order everything else in
// this reader is emitted in. That is a deliberate exception, for a reason worth
// writing down rather than a mistake worth tidying.
//
// Shapes of one role are unioned rather than painted over one another, so no
// ordering should be visible in the picture. "Should" is the load-bearing word:
// a rasterizer accumulates coverage in floating point and floating-point
// addition is not associative, so an order-blind drawing is a claim about
// arithmetic rather than about geometry. The claim was measured -- reversing
// the whole shape list moves no tile of this city -- and the order is pinned to
// the capture anyway, because pinning it costs nothing and means the pyramid's
// bytes never rest on that measurement holding for the next city.
func drawShapes(raw *capture, curated city) *doc.Drawing {
	roles := make(map[string]*dataset)
	table := curated.datasets()
	for at := range table {
		if table[at].Role != "" {
			roles[table[at].Slug] = &table[at]
		}
	}

	out := &doc.Drawing{Zoom: raw.Basemap.MaxZoom}
	for _, captured := range raw.Datasets {
		layer, drawn := roles[captured.Slug]
		if !drawn {
			continue
		}
		for _, row := range captured.Features {
			emphasis := 0.0
			if layer.Emphasis != nil {
				emphasis = layer.Emphasis(row.Fields)
			}
			switch row.Geometry.Type {
			case geometryRings:
				// Each polygon is its own shape, so the winding convention --
				// first ring ground, the rest holes -- reads one polygon at a
				// time rather than across a whole multipart row.
				for _, polygon := range row.Geometry.Rings {
					shape := doc.Shape{Role: layer.Role, Emphasis: emphasis}
					for _, ring := range polygon {
						shape.Rings = append(shape.Rings, raw.Window.project(ring))
					}
					if len(shape.Rings) > 0 {
						out.Shapes = append(out.Shapes, shape)
					}
				}
			case geometryLines:
				shape := doc.Shape{Role: layer.Role, Emphasis: emphasis}
				for _, line := range row.Geometry.Lines {
					shape.Lines = append(shape.Lines, raw.Window.project(line))
				}
				if len(shape.Lines) > 0 {
					out.Shapes = append(out.Shapes, shape)
				}
			}
			// A point draws nothing: pins are features, not ground.
		}
	}
	if len(out.Shapes) == 0 {
		return nil
	}
	return out
}

// project lands a path of true coordinates on the world square. A position
// spelled with fewer than two numbers is dropped rather than guessed at; the
// path it was in survives without it, because a hub's occasional malformed
// vertex is not a reason to lose a street.
func (w window) project(path [][]float64) [][2]float64 {
	out := make([][2]float64, 0, len(path))
	for _, position := range path {
		if len(position) < 2 {
			continue
		}
		x, y := w.worldPixel(position[0], position[1])
		out = append(out, [2]float64{x, y})
	}
	return out
}
