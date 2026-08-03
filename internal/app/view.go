package app

import (
	"html/template"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// View is what every region is rendered with: the library, the volume and
// world in view, and the session that colours them. One type for every region
// is deliberate -- a region is a view of the same application state, not of a
// bespoke payload -- and it is the seam between "the handler decided" and "the
// template rendered".
//
// Everything a template needs must already be a field here. A template that
// has to compute is a decision that belongs in Go (issue #5 §4.5), so the
// fields below are answers rather than materials: the legend arrives as
// sections and rows already counted, the dock as a shortlist and the words
// above it, the viewport as the attributes its state node wears.
type View struct {
	// Title is the page title: the volume in view, or the application.
	Title string

	// Library is every serving volume, in the catalog's order.
	Library []LibraryEntry

	// LibraryDir is where bundles go, in the words of the reader's own
	// machine. Shown by the empty state; a label, never a path.
	LibraryDir string

	// Volume is the volume in view, or nil on the empty-library page.
	Volume *VolumeView

	// Session is the remembered state for the volume in view.
	Session Session

	// Legend is the sidebar: the tree, the isolate chip, the footer count.
	Legend LegendView

	// Dock is the panel beside the map.
	Dock DockView

	// Detail is the open card, or its closed shape.
	Detail DetailView

	// Grid is the cell navigator.
	Grid GridView

	// Overview is the corner locator's chrome. What it draws is the seam's.
	Overview OverviewView

	// Viewport is the inert state node the seam reads.
	Viewport ViewportView

	// Island is the session as JSON, for the page's embedded state island.
	Island template.JS

	// Feature is the feature a detail fragment was asked for.
	Feature string

	// Rows are the progress rows an import has produced so far.
	Rows []ImportRow
}

// LibraryEntry is one volume as the topbar's volume selector lists it.
type LibraryEntry struct {
	Slug    string
	Title   string
	Stamp   string
	Base    string
	Worlds  int
	Current bool
}

// VolumeView is the volume the page is about.
type VolumeView struct {
	Slug   string
	Title  string
	Stamp  string
	Base   string
	Worlds []WorldView
	World  WorldView
	Lenses []LensView

	// Lens is the lens in view and LensIndex its place in the list, which
	// is how the reference implementation's session recorded it and how the
	// golden baselines still read it.
	Lens      string
	LensIndex int

	// Sphere is whether this world declares a surface a globe can be draped
	// over, which is the whole reason the globe toggle is offered.
	Sphere bool
}

// WorldView is one ground within the volume.
type WorldView struct {
	Slug    string
	Title   string
	Parent  string
	Points  int
	Paths   int
	Areas   int
	Current bool
}

// LensView is one raster pyramid picturing the world in view.
type LensView struct {
	Index   int
	Name    string
	Current bool
}

// DockView is the panel: what it counts, why the count is what it is, and the
// shortlist under it.
type DockView struct {
	Open      bool
	Count     string
	Flag      string
	FlagTitle string
	Note      string
	Empty     string
	Rows      []ListEntry
	RailName  string
	Filtered  bool
	Section   string
	FoldLabel string
}

// DetailView is one feature's card.
type DetailView struct {
	Open        bool
	ID          string
	Title       string
	Category    string
	Description string
	Color       string
	Glyph       string
	Icon        string
	Coordinates string
	// Point is whether the card is about a point rather than a shape. The
	// card asks it about one row only: the grid cell, which is a name for
	// somewhere a *point* stands and which the seam fills in
	// (render/viewport.ts, writeCell). A shape has no such place, and the
	// reference hid the row on exactly that branch.
	Point  bool
	Source string
	Links  []DetailLink
	Rows   []DetailRow
}

// DetailLink is one feature the open one points at.
type DetailLink struct {
	ID    string
	Title string
}

// DetailRow is one term and its value in the card's definition list.
type DetailRow struct {
	Key   string
	Label string
	Value string
}

// GridView is the cell navigator's chrome. Which systems a world offers and
// what a cell draws are the analysis lane's (issue #5 §5.4); what is held is
// the session's, and that is all this carries.
type GridView struct {
	Enabled bool
	System  string
	Name    string
	Cell    string
	Subgrid bool
	Offered bool

	// Mark is the short mark the cycle button wears, Next names the system it
	// would step to, and Cycles is whether the button is on the page at all.
	//
	// A world most systems refuse -- every game map, which declares nothing
	// about what its picture is of -- offers geohash alone, and a control that
	// cycles between one thing is a control that does nothing. The reference
	// hid it (`syncGridSystemControl`: `hidden = systems.length < 2`), so the
	// question the button asks a reader is always a real one.
	Mark   string
	Next   string
	Cycles bool

	// Length is how many characters the field accepts: the depth this
	// system's telescope stops at, spelled on the input so a reader cannot
	// type past a floor the record would silently clamp them to.
	Length int
}

// OverviewView is the corner locator's chrome.
type OverviewView struct {
	Docked bool
	Label  string
	World  string
}

// ViewportView is the inert state node: the attributes the seam reads off it,
// and the three lists it carries as <data> children.
type ViewportView struct {
	Volume      string
	Base        string
	World       string
	Lens        string
	LensIndex   int
	Surface     string
	Selected    string
	Search      string
	GridSystem  string
	GridCell    string
	Subgrid     int
	Hidden      []string
	Highlighted []string
	Overrides   []LabelOverride
	Camera      *Camera
}

// LabelOverride is one reader-set label policy, as the state node carries it.
type LabelOverride struct {
	Collection string
	Policy     string
}

// view assembles what the regions are rendered with. The world it shows is
// the session's, falling back to the volume's first world, which is the one a
// volume opens on when nobody has opened it before.
func (a *App) view(held library, volume hostenv.Volume, session Session) View {
	out := View{
		Title:      "Atlas",
		LibraryDir: a.env.Volumes().Location(),
		Session:    session,
	}
	current := ""
	if volume != nil {
		current = volume.Manifest().Volume.Slug
	}
	for _, listed := range held.order {
		manifest := listed.Manifest()
		out.Library = append(out.Library, LibraryEntry{
			Slug:    manifest.Volume.Slug,
			Title:   manifest.Volume.Title,
			Stamp:   bundle.ShortStamp(manifest.Version.Stamp),
			Base:    volumeBase(manifest),
			Worlds:  len(manifest.Worlds),
			Current: manifest.Volume.Slug == current,
		})
	}
	if volume == nil {
		out.Island = island("", nil, nil, Session{})
		return out
	}

	manifest := volume.Manifest()
	shown := VolumeView{
		Slug:  manifest.Volume.Slug,
		Title: manifest.Volume.Title,
		Stamp: bundle.ShortStamp(manifest.Version.Stamp),
		Base:  volumeBase(manifest),
	}
	world := session.World
	if _, ok := worldEntry(manifest, world); !ok {
		world = manifest.Worlds[0].Slug
	}
	for _, entry := range manifest.Worlds {
		listed := WorldView{
			Slug:    entry.Slug,
			Title:   entry.Title,
			Parent:  entry.Parent,
			Points:  entry.Points,
			Paths:   entry.Paths,
			Areas:   entry.Areas,
			Current: entry.Slug == world,
		}
		if listed.Current {
			shown.World = listed
		}
		shown.Worlds = append(shown.Worlds, listed)
	}

	model := a.world(volume, world)
	var lens *payloadLens
	if model != nil {
		for at := range model.Lenses {
			entry := &model.Lenses[at]
			listed := LensView{Index: at, Name: entry.Name}
			if entry.Name == session.Lens || (session.Lens == "" && at == 0) {
				listed.Current = true
				shown.Lens, shown.LensIndex = entry.Name, at
				lens = entry
			}
			shown.Lenses = append(shown.Lenses, listed)
		}
		shown.Sphere = model.Attrs[semconv.KeyGeometrySurface] == "sphere"
	}
	out.Volume = &shown
	out.Title = manifest.Volume.Title

	standing := visible(model, session, lens)
	out.Legend = legend(model, session, standing, lens, shown.Base)
	out.Dock = dockView(session, standing, model)
	out.Detail = a.detail(volume, model, session, session.Selected)
	out.Grid = gridView(session, model)
	out.Overview = OverviewView{Docked: session.Overview.Docked, World: shown.World.Title}
	if out.Overview.Docked {
		out.Overview.Label = "Bring the overview back"
	} else {
		out.Overview.Label = "Put the overview away"
	}
	out.Viewport = viewportView(shown, session)
	out.Island = island(manifest.Volume.Slug, model, &shown, session)
	return out
}

// dockView is the panel's own account of what the filters left standing.
func dockView(session Session, standing visibility, model *worldModel) DockView {
	out := DockView{
		Open:      session.Dock.Open,
		Count:     standing.DockText,
		Flag:      standing.DockFlag,
		FlagTitle: standing.DockFlagTitle,
		Note:      standing.DockNote,
		Empty:     standing.Empty,
		Rows:      standing.Rows(),
		Filtered:  standing.Filtered,
		Section:   session.Dock.Section,
	}
	if out.Open {
		out.FoldLabel = "Put the panel away (⌘⌥B)"
	} else {
		out.FoldLabel = "Bring the panel back (⌘⌥B)"
	}
	// Folded, the panel is a rail with one word on it, and that word used to
	// read the same whether or not a feature was open: the map had something
	// picked out and the only panel saying which was the one just put away.
	// The rail carries the name until the card is closed.
	if model != nil && session.Selected != "" {
		if pin, ok := model.PointByID[session.Selected]; ok {
			out.RailName = railName(pin.Title)
		} else if shape, ok := model.ShapeByID[session.Selected]; ok {
			out.RailName = railName(shape.Title)
		}
	}
	return out
}

// railNameLimit is where a name is cut on the rail. The rail runs the height
// of the window and a long name would run past the bottom of it; this is where
// a reader can still recognise the place.
const railNameLimit = 28

func railName(title string) string {
	runes := []rune(title)
	if len(runes) <= railNameLimit {
		return title
	}
	return strings.TrimRight(string(runes[:railNameLimit-1]), " ") + "…"
}

func gridView(session Session, model *worldModel) GridView {
	out := GridView{
		Enabled: session.Grid.System != "",
		System:  session.Grid.System,
		Cell:    session.Grid.Cell,
		Subgrid: session.Grid.Subgrid > 0,
		Offered: model != nil,
	}
	out.Name, out.Mark, out.Length = systemName(out.System), systemMark(out.System), inputLength(out.System)
	// Which systems divide this world is the world's own answer, and the
	// button is offered only when there is a second one to step to.
	offered := cells.ApplicableSystems(worldAttrs(model))
	out.Cycles = len(offered) > 1 && out.System != ""
	out.Next = systemName(nextSystem(offered, out.System))
	return out
}

// viewportView fills the inert state node. Data flows one way through it: the
// server writes, the seam reads and reconciles, and the only things that come
// back are a pick and a settled camera (issue #5 §5.5).
func viewportView(shown VolumeView, session Session) ViewportView {
	out := ViewportView{
		Volume:      shown.Slug,
		Base:        shown.Base,
		World:       shown.World.Slug,
		Lens:        shown.Lens,
		LensIndex:   shown.LensIndex,
		Selected:    session.Selected,
		Search:      session.Search,
		GridSystem:  session.Grid.System,
		GridCell:    session.Grid.Cell,
		Subgrid:     session.Grid.Subgrid,
		Hidden:      sortedIDs(session.Hidden),
		Highlighted: sortedIDs(session.Highlighted),
	}
	out.Surface = "plane"
	if shown.Sphere {
		out.Surface = "sphere"
	}
	for collection, policy := range session.Labels {
		out.Overrides = append(out.Overrides, LabelOverride{Collection: collection, Policy: policy})
	}
	sort.Slice(out.Overrides, func(i, j int) bool {
		return out.Overrides[i].Collection < out.Overrides[j].Collection
	})
	if camera, ok := session.Cameras[shown.World.Slug]; ok {
		out.Camera = &camera
	}
	return out
}

// ---------------------------------------------------------------------------
// The card
// ---------------------------------------------------------------------------

// attributeLabels are the curated words a few attributes wear on a card. An
// attribute with no curated word wears its key, which is honest and rare.
// Keys come from the registry rather than as literals, so a key that is
// renamed there is a compile error here (issue #5 §9).
var attributeLabels = map[string]string{semconv.KeyHydroHUC12: "HUC-12"}

// reservedPrefixes are the namespaces that are machinery rather than
// material: how a thing is drawn, whether its name speaks, how thick its line
// is, what shape it is. A card is about the place, not about the rendering.
//
// They are derived from the registry's own keys rather than spelled out, so a
// new rendering key lands inside the fence by construction: an attribute that
// governs how a thing draws is machinery whatever it is called.
var reservedPrefixes = namespacesOf(
	semconv.KeyRenderAs, semconv.KeyLabelPolicy,
	semconv.KeyStrokeWidthPx, semconv.KeyGeometryKind,
)

// namespacesOf is the dotted namespace each key belongs to, with its trailing
// dot: atlas.render.as becomes atlas.render.
func namespacesOf(keys ...string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if at := strings.LastIndexByte(key, '.'); at > 0 {
			out = append(out, key[:at+1])
		}
	}
	return out
}

// detail builds the card for one feature, or the closed card when nothing is
// selected. Prose, links and the attributes that only matter once a card is
// open all come out of worlds/<slug>.text, which is why it is read here and
// not when the world is stood up.
func (a *App) detail(volume hostenv.Volume, model *worldModel, session Session, id string) DetailView {
	if id == "" || !session.Detail.Open || model == nil {
		return DetailView{}
	}
	out := DetailView{Open: true, ID: id}
	text := a.text(volume, model.Slug)

	switch pin, isPoint := model.PointByID[id]; {
	case isPoint:
		out.Title = pin.Title
		out.Category = strings.Join(nonEmpty(pin.Collection.Group, pin.Collection.Title), " / ")
		out.Color = collectionColor(pin.Collection)
		out.Glyph = initials(pin.Collection.Title)
		out.Icon = pin.Collection.Icon
		out.Coordinates = strconv.FormatFloat(pin.Lat, 'f', 6, 64) + ", " +
			strconv.FormatFloat(pin.Lng, 'f', 6, 64)
		// Which is what earns this card the empty cell row: a place a point
		// stands is a place the cell systems can name.
		out.Point = true
		out.Source = model.Origin
	default:
		shape, isShape := model.ShapeByID[id]
		if !isShape {
			return DetailView{}
		}
		out.Title = shape.Title
		out.Category = strings.Join(nonEmpty(shape.Collection.Group, shape.Collection.Title), " / ")
		if out.Category == "" {
			out.Category = shape.Subtitle
		}
		out.Color = colorFor(shape.ID)
		out.Source = model.Origin
		out.Rows = append(out.Rows, attributeRows(shape.Attrs)...)
	}

	if held, ok := text[id]; ok {
		out.Description = cleanDescription(held.Description)
		for _, link := range held.Links {
			linked := strconv.FormatInt(link, 10)
			title := linked
			if pin, ok := model.PointByID[linked]; ok {
				title = pin.Title
			} else if shape, ok := model.ShapeByID[linked]; ok {
				title = shape.Title
			}
			out.Links = append(out.Links, DetailLink{ID: linked, Title: title})
		}
		out.Rows = append(out.Rows, attributeRows(held.Attrs)...)
	}
	if out.Description == "" {
		out.Description = "No description is included in the archive."
	}
	sort.SliceStable(out.Rows, func(i, j int) bool { return out.Rows[i].Label < out.Rows[j].Label })
	return out
}

// attributeRows turns a feature's attributes into the rows its card shows:
// the curated label where one exists, the raw key where none does, and nothing
// at all for machinery or for the geographic pair the card already has a row
// for.
func attributeRows(attrs map[string]string) []DetailRow {
	var out []DetailRow
	for key, value := range attrs {
		if key == semconv.KeyGeoLat || key == semconv.KeyGeoLon {
			continue
		}
		reserved := false
		for _, prefix := range reservedPrefixes {
			if strings.HasPrefix(key, prefix) {
				reserved = true
				break
			}
		}
		if reserved {
			continue
		}
		label := attributeLabels[key]
		if label == "" {
			label = key
		}
		out = append(out, DetailRow{Key: key, Label: label, Value: value})
	}
	return out
}

// linkText is the markdown link a source's prose arrives wearing. The card
// keeps the words and drops the address: a bundle carries no runtime URL, and
// a link nobody can follow is noise in a sentence.
var linkText = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

func cleanDescription(value string) string {
	out := linkText.ReplaceAllString(value, "$1")
	out = strings.Map(func(r rune) rune {
		switch r {
		case '*', '_', '`', '>', '#', '\r':
			return -1
		}
		return r
	}, out)
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// worldEntry finds one world in a manifest.
func worldEntry(m bundle.Manifest, slug string) (bundle.WorldEntry, bool) {
	for _, entry := range m.Worlds {
		if entry.Slug == slug {
			return entry, true
		}
	}
	return bundle.WorldEntry{}, false
}
