package basemap

import "image/color"

// Role is what a shape is to the ground -- which stroke of the drawing it
// belongs to, never which dataset it arrived in. A source's curation assigns
// roles; this package knows only how each one draws, which is what keeps a
// publisher's vocabulary out of the renderer (issue #5 §5.1).
type Role string

// The roles the drawing knows. A shape carrying anything else draws nothing:
// an unknown role is a curation entry ahead of the style table, and silence is
// the honest answer to it.
const (
	RoleParcel   Role = "parcel"
	RolePark     Role = "park"
	RoleWater    Role = "water"
	RoleStreet   Role = "street"
	RoleTrail    Role = "trail"
	RoleBoundary Role = "boundary"
)

// Style is one role's whole appearance: the colour its ground is flooded with,
// the colour and width of its linework, and the on/off rhythm of a dashed
// stroke. Widths are world-true -- spelled at ReferenceZoom and scaled with
// depth -- so a street keeps its width on the ground however deep the pyramid
// goes.
type Style struct {
	Fill        color.NRGBA
	Stroke      color.NRGBA
	StrokeWidth float64
	Dash        [2]float64
}

// ReferenceZoom is the local zoom the width column is spelled at. It is
// exported because it is the one number in the style table a reader of a city
// pyramid has to know to predict what a level looks like.
const ReferenceZoom = 6

// Background is the flat ground every tile starts as: a step off the app's own
// near-black, so the map reads as a surface and the palette accents stay the
// information. It is also what the deriver samples when it decides which tile
// of a level is the one worth omitting.
var Background = color.NRGBA{R: 0x14, G: 0x18, B: 0x1d, A: 0xff}

// styles is the drawing itself. The colours keep to the app's identity:
// neutral greys for everyday linework, a warm earth tone for trails, navy for
// water, and the cerulean accent spent on exactly one line -- the city's own
// boundary.
var styles = map[Role]Style{
	RoleParcel:   {Stroke: color.NRGBA{R: 0x23, G: 0x2a, B: 0x31, A: 0xff}, StrokeWidth: 1},
	RolePark:     {Fill: color.NRGBA{R: 0x1f, G: 0x2b, B: 0x22, A: 0xff}},
	RoleWater:    {Fill: color.NRGBA{R: 0x15, G: 0x25, B: 0x39, A: 0xff}, Stroke: color.NRGBA{R: 0x1f, G: 0x38, B: 0x52, A: 0xff}, StrokeWidth: 1},
	RoleStreet:   {Stroke: color.NRGBA{R: 0x3d, G: 0x44, B: 0x4d, A: 0xff}, StrokeWidth: 2.5},
	RoleTrail:    {Stroke: color.NRGBA{R: 0x8a, G: 0x6a, B: 0x44, A: 0xff}, StrokeWidth: 1.5, Dash: [2]float64{6, 4}},
	RoleBoundary: {Stroke: color.NRGBA{R: 0x3a, G: 0xa5, B: 0xc9, A: 0xff}, StrokeWidth: 3.5},
}

// zOrder is the order the roles land on the ground, and it is the drawing's
// only opinion about layering: parcel texture first, then ground covers, water
// over parks so a lake punches through one, streets over water so a bridge
// reads as a bridge, trails above streets, and the boundary last as the single
// accent. Nothing within a role is ordered -- a role's shapes are unioned, so
// two overlapping parks are one park.
var zOrder = []Role{RoleParcel, RolePark, RoleWater, RoleStreet, RoleTrail, RoleBoundary}
