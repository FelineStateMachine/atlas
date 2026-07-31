package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

// Most maps address a tile as /{zoom}/{x}/{y}, but some publish the two axes
// the other way round. Nothing in the API says which, and a transposed map
// still returns a real tile for every request, so the mistake is invisible
// until the tiles are laid side by side: neighbours that belong together share
// an edge, and neighbours that do not are unrelated pictures.
//
// probeAxisOrder measures that. It reads one small square block -- the same set
// of URLs either way round -- and reports whether reading the first component
// as the row lines the block up better.
const (
	axisProbeSpan    = 3
	axisProbeSamples = 32
)

func probeAxisOrder(
	ctx context.Context,
	fetcher *fetcher,
	base string,
	set apiTileSet,
	zoom int,
	window tileWindow,
	extension string,
) (bool, error) {
	span := axisProbeSpan
	if width := window.maxX - window.minX + 1; span > width {
		span = width
	}
	if height := window.maxY - window.minY + 1; span > height {
		span = height
	}
	if span < 2 {
		return false, nil
	}
	// Sample from the middle, where a map is likeliest to hold content: two
	// blank tiles share an edge perfectly whichever way round they are read.
	originA := window.minX + (window.maxX-window.minX+1-span)/2
	originB := window.minY + (window.maxY-window.minY+1-span)/2

	block := make(map[[2]int]image.Image, span*span)
	for a := range span {
		for b := range span {
			url := fmt.Sprintf("%s/games/%s/%d/%d/%d.%s",
				base, set.Path, zoom, originA+a, originB+b, extension)
			body, _, err := fetcher.get(ctx, url)
			if err != nil {
				continue
			}
			decoded, _, err := image.Decode(bytes.NewReader(body))
			if err != nil {
				continue
			}
			block[[2]int{a, b}] = decoded
		}
	}
	if len(block) < span*span {
		// An incomplete probe cannot be trusted either way; assume the usual
		// order rather than transpose on partial evidence.
		return false, nil
	}

	direct := seamCost(block, false, span)
	transposed := seamCost(block, true, span)
	if direct < 0 || transposed < 0 {
		return false, nil
	}
	return transposed < direct, nil
}

// seamCost is the average colour distance across the vertical seams of a block,
// laid out either directly or transposed. Lower means the tiles belong together.
func seamCost(block map[[2]int]image.Image, transpose bool, span int) float64 {
	at := func(column, row int) image.Image {
		if transpose {
			return block[[2]int{row, column}]
		}
		return block[[2]int{column, row}]
	}
	var total float64
	var seams int
	for row := range span {
		for column := 0; column < span-1; column++ {
			left, right := at(column, row), at(column+1, row)
			if left == nil || right == nil {
				continue
			}
			total += edgeDistance(left, right)
			seams++
		}
	}
	if seams == 0 {
		return -1
	}
	return total / float64(seams)
}

// edgeDistance compares the right-hand column of one tile with the left-hand
// column of the tile that should sit beside it.
func edgeDistance(left, right image.Image) float64 {
	lb, rb := left.Bounds(), right.Bounds()
	height := min(lb.Dy(), rb.Dy())
	if height == 0 {
		return 0
	}
	step := max(1, height/axisProbeSamples)
	var total float64
	var count int
	for offset := 0; offset < height; offset += step {
		lr, lg, lbl, _ := left.At(lb.Max.X-1, lb.Min.Y+offset).RGBA()
		rr, rg, rbl, _ := right.At(rb.Min.X, rb.Min.Y+offset).RGBA()
		total += absDiff(lr, rr) + absDiff(lg, rg) + absDiff(lbl, rbl)
		count += 3
	}
	if count == 0 {
		return 0
	}
	return total / float64(count) / 257 // back to 0-255
}

func absDiff(a, b uint32) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}
