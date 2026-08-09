// Package minting declares the capability shared by the pure Atlas web
// application and a native host that can turn an authored region into a
// validated Atlas file. It contains contracts only; filesystem and network
// work remain in the implementation supplied by the host.
package minting

import "context"

// Request is the compact, user-owned recipe for one geographic Atlas.
type Request struct {
	Title       string
	Bounds      [4]float64
	DetailZoom  int
	Topo        bool
	Roads       bool
	Hydro       bool
	Counties    bool
	RoadLabel   string
	HydroLabel  string
	CountyLabel string
	RoadColor   string
	HydroColor  string
	HydroFill   string
	CountyColor string
}

// Preview is the bounded plan shown before a build begins.
type Preview struct {
	Pixels         int64
	RasterTiles    int64
	Requests       int
	EstimatedBytes int64
}

// Event is one stage reported by the deterministic authoring pipeline.
type Event struct {
	Stage    string
	Message  string
	Current  int
	Total    int
	Bytes    int64
	Cached   bool
	Artifact string
}

// Result identifies the installed volume the application should open.
type Result struct {
	Slug  string
	World string
	Title string
	Stamp string
}

// Minter is an optional native capability. Headless hosts omit it; the
// desktop supplies it without teaching the HTTP application about paths.
type Minter interface {
	Default() Request
	Preview(Request) (Preview, error)
	Mint(context.Context, Request, func(Event)) (Result, error)
}
