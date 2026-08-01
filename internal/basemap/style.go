package basemap

import "image/color"

// Role names what a feature is to the ground: not which dataset it came
// from, but which stroke of the drawing it belongs to. The curated tables
// in arcgismap assign roles; this package only knows how each one draws.
type Role string

const (
	RoleBackground Role = "background"
	RoleParcel     Role = "parcel"
	RolePark       Role = "park"
	RoleWater      Role = "water"
	RoleStreet     Role = "street"
	RoleTrail      Role = "trail"
	RoleBoundary   Role = "boundary"
)

// Style is how one role draws: a fill for ground, a stroke for lines and
// rims, widths in pixels at the reference zoom. Dash is the on/off rhythm
// of a dashed stroke, zero for solid.
type Style struct {
	Fill        color.NRGBA
	Stroke      color.NRGBA
	StrokeWidth float64
	Dash        [2]float64
}

// referenceZoom is the zoom the style table's widths are spelled at. A
// render at another depth scales them, so a stroke keeps its ground truth
// -- constant world width -- however deep the pyramid goes.
const referenceZoom = 6

// Background is the ground everything else draws upon: a step off the
// app's own near-black, so the map reads as a surface and the palette
// accents stay the information.
var Background = color.NRGBA{R: 0x14, G: 0x18, B: 0x1d, A: 0xff}

// styles is the whole drawing, keyed by role. The colors keep to the app's
// identity: neutral grays for the everyday linework, the warm earth tone
// for trails, navy for water, and the cerulean accent spent on exactly one
// line -- the city's own boundary.
var styles = map[Role]Style{
	RoleParcel:   {Stroke: color.NRGBA{R: 0x23, G: 0x2a, B: 0x31, A: 0xff}, StrokeWidth: 1},
	RolePark:     {Fill: color.NRGBA{R: 0x1f, G: 0x2b, B: 0x22, A: 0xff}},
	RoleWater:    {Fill: color.NRGBA{R: 0x15, G: 0x25, B: 0x39, A: 0xff}, Stroke: color.NRGBA{R: 0x1f, G: 0x38, B: 0x52, A: 0xff}, StrokeWidth: 1},
	RoleStreet:   {Stroke: color.NRGBA{R: 0x3d, G: 0x44, B: 0x4d, A: 0xff}, StrokeWidth: 2.5},
	RoleTrail:    {Stroke: color.NRGBA{R: 0x8a, G: 0x6a, B: 0x44, A: 0xff}, StrokeWidth: 1.5, Dash: [2]float64{6, 4}},
	RoleBoundary: {Stroke: color.NRGBA{R: 0x3a, G: 0xa5, B: 0xc9, A: 0xff}, StrokeWidth: 3.5},
}

// zOrder is the order roles land on the ground: texture first, then ground
// covers, water over parks so lakes punch through, streets over water for
// bridges, trails above streets, and the boundary on top as the one accent.
var zOrder = []Role{RoleParcel, RolePark, RoleWater, RoleStreet, RoleTrail, RoleBoundary}
