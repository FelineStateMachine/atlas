package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// The session record is the server's half of the state-placement rule
// (issue #5 §4.1): discrete application state lives on the server and in URLs,
// continuous interaction state lives in the seam. Everything a reader would be
// annoyed to lose across a restart is here; everything that changes sixty
// times a second is not, except the camera, which reports upward once per
// settle so "reopen where you left off" survives a restart.
//
// One record per volume, plus a small record naming the volume last open. The
// schema is documented in docs/app.md and versioned by SessionSchema: a record
// written by a newer schema is ignored rather than half-read, which costs a
// reader their layout once and never corrupts it.

// SessionSchema is the version of the record shape below.
//
// Version 2 is the templates wave. The arrangement grew the fields the legend
// tree actually has -- which shape rows are unfolded, which ground is
// highlighted, where the corner locator and the panel are put -- and lost the
// solo list, because isolating turned out to be a move on the hide set rather
// than a state of its own (see applySolo, and legend.go's soloChip, which
// derives the chip from the hide set so it is right however that set was
// reached). A version-1 record is passed over rather than half-read, which
// costs a reader their layout once.
const SessionSchema = 2

// Session is one volume's remembered state.
type Session struct {
	Schema int    `json:"schema"`
	Volume string `json:"volume"`

	// Stamp is the serving build the record was last written against. A
	// record whose stamp is behind the serving build is still read -- slugs
	// outlive builds -- but the difference is what tells the events stream
	// that the page in front of the reader is looking at an older volume.
	Stamp string `json:"stamp,omitempty"`

	World string `json:"world,omitempty"`
	Lens  string `json:"lens,omitempty"`

	// Hidden collections, Collapsed sections, Expanded shape rows and
	// Highlighted features are named by their ids in the world payload; all
	// four are kept sorted so a record is stable to diff and two paths to
	// the same state produce the same bytes.
	//
	// Arranged is what tells a fresh record from an arranged one. Three of
	// these sets have non-empty defaults that a world's own curation
	// supplies -- the payload's hidden collections, the folded Zones
	// section, the unfolded shape rows -- and an empty set is a reader's
	// deliberate "show everything", not an absence. Without this flag the
	// two are the same bytes.
	Hidden      []string `json:"hidden,omitempty"`
	Collapsed   []string `json:"collapsed,omitempty"`
	Expanded    []string `json:"expanded,omitempty"`
	Highlighted []string `json:"highlighted,omitempty"`
	Arranged    bool     `json:"arranged,omitempty"`

	// Labels is the per-collection label-policy override: collection id to
	// policy name. An absent entry means the policy the conventions imply.
	Labels map[string]string `json:"labels,omitempty"`

	Search   string   `json:"search,omitempty"`
	Dock     Dock     `json:"dock"`
	Detail   Detail   `json:"detail"`
	Grid     Grid     `json:"grid"`
	Sidebar  Sidebar  `json:"sidebar"`
	Overview Overview `json:"overview"`
	Selected string   `json:"selected,omitempty"`

	// Cameras is the last settled camera per world, reported by the seam.
	// It is the one piece of continuous state the server keeps, and it is
	// kept because a reader expects a volume to open where they left it.
	Cameras map[string]Camera `json:"cameras,omitempty"`

	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Dock is the readout beside the map: whether it is open, which section of it
// is showing, and whether the reader has put it away themselves.
//
// The panel starts out of the way, because the map is the reason the window is
// open, and comes out by itself the first time it has something to say. Once
// -- and only until the reader has answered the question by folding it by
// hand, which is what Dismissed records. After that it stays where they put it
// and the map is theirs.
type Dock struct {
	Open      bool   `json:"open"`
	Dismissed bool   `json:"dismissed,omitempty"`
	Section   string `json:"section,omitempty"`
}

// Detail is the card for one feature.
type Detail struct {
	Open bool `json:"open"`
}

// Grid is the cell system in play: which system, which cell is held, and how
// far the subgrid is opened under it.
type Grid struct {
	System  string `json:"system,omitempty"`
	Cell    string `json:"cell,omitempty"`
	Subgrid int    `json:"subgrid,omitempty"`
}

// Sidebar is the legend column. It is spelled as the thing a reader does to
// it rather than the state it is usually in, so the zero value is the ordinary
// page: a sidebar nobody has collapsed.
type Sidebar struct {
	Collapsed bool `json:"collapsed,omitempty"`
}

// Overview is the corner locator. Docked means put away against the edge --
// the same shape of choice as the panel, and remembered with the volume rather
// than with one of its worlds, because where the corner of the screen is
// wanted is a preference about the atlas.
type Overview struct {
	Docked bool `json:"docked,omitempty"`
}

// Camera is one world's settled view, in the seam's own terms. The server
// stores it and hands it back; it never reasons about it.
type Camera struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Zoom     float64 `json:"zoom"`
	Rotation float64 `json:"rotation,omitempty"`
	At       string  `json:"at,omitempty"`
}

// pointer is the small record naming the volume the reader was last in, so /
// can send them back to it.
type pointer struct {
	Schema    int    `json:"schema"`
	Volume    string `json:"volume,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

const pointerRecord = "app.json"

// sessionRecord is the record name one volume's session is kept under. The
// slug has already passed bundle.ValidSlug by being in a manifest, and
// hostenv.ValidName holds it again at the store.
func sessionRecord(slug string) string { return "volume." + slug + ".json" }

// session reads one volume's record, answering a fresh one where there is
// nothing to read. A record from a schema this build does not know is passed
// over the same way: a reader loses their layout once, rather than meeting a
// page assembled out of half-understood state.
func (a *App) session(slug string) Session {
	fresh := Session{Schema: SessionSchema, Volume: slug}
	data, err := a.env.Sessions().Load(sessionRecord(slug))
	if err != nil {
		return fresh
	}
	var held Session
	if err := json.Unmarshal(data, &held); err != nil || held.Schema != SessionSchema {
		return fresh
	}
	held.Volume = slug
	return held
}

// saveSession writes one volume's record.
func (a *App) saveSession(s *Session) error {
	s.Schema = SessionSchema
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	sort.Strings(s.Hidden)
	sort.Strings(s.Collapsed)
	sort.Strings(s.Expanded)
	sort.Strings(s.Highlighted)
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	return a.env.Sessions().Save(sessionRecord(s.Volume), data)
}

// lastVolume is the volume the reader was last in, or empty.
func (a *App) lastVolume() string {
	data, err := a.env.Sessions().Load(pointerRecord)
	if err != nil {
		if !errors.Is(err, hostenv.ErrNoSession) {
			return ""
		}
		return ""
	}
	var held pointer
	if err := json.Unmarshal(data, &held); err != nil || held.Schema != SessionSchema {
		return ""
	}
	return held.Volume
}

// rememberVolume records which volume is in front of the reader. It is
// written when a volume's page is served, because a real URL typed or
// bookmarked is as much a choice as one clicked.
func (a *App) rememberVolume(slug string) error {
	data, err := json.Marshal(pointer{
		Schema:    SessionSchema,
		Volume:    slug,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("encode pointer: %w", err)
	}
	return a.env.Sessions().Save(pointerRecord, data)
}

// A concern is one interaction the session answers for. One route per concern
// (issue #5 §4.2), each declaring the regions its answer covers, so a response
// carries exactly the partials the interaction touched and no page-wide
// refresh stands in for thinking about it.
//
// The region set beside each concern is the whole table of what an interaction
// may move; partialTargets (partials.go) is the whole table of where a region
// goes. Between them there is no third place where a swap is decided.
type concern struct {
	apply   func(c *concernContext, form formValues) error
	regions []string
}

// concernContext is what a concern gets to work with: the record it is
// changing and the world that record is about. Several of the moves are
// algebra over the world's own collections -- isolating, folding a whole
// section, unfolding every shape row -- and the alternative to handing the
// model over is a client that computes the answer and posts a set, which is
// the display logic leaking back into the page.
type concernContext struct {
	session *Session
	world   *worldModel
}

// concerns is the table of session routes. One line per concern; adding an
// interaction is adding a line and a function.
//
// Reading the regions column downward is the fastest way to see what the
// application thinks is coupled to what: every filtering move touches the
// legend (its own rows and the isolate chip), the dock (the count and the
// list), and the viewport's state node (what the seam draws). Nothing touches
// the topbar but the things that change what is on offer.
var concerns = map[string]concern{
	"world":       {apply: applyWorld, regions: []string{"topbar", "legend", "dock", "overview", "viewport"}},
	"lens":        {apply: applyLens, regions: []string{"topbar", "overview", "viewport"}},
	"collections": {apply: applyCollections, regions: []string{"legend", "dock", "viewport"}},
	"sections":    {apply: applySections, regions: []string{"legend"}},
	"expand":      {apply: applyExpand, regions: []string{"legend"}},
	"labels":      {apply: applyLabels, regions: []string{"legend", "viewport"}},
	"solo":        {apply: applySolo, regions: []string{"legend", "dock", "viewport"}},
	"search":      {apply: applySearch, regions: []string{"legend", "dock", "viewport"}},
	"highlight":   {apply: applyHighlight, regions: []string{"legend", "dock", "viewport"}},
	"dock":        {apply: applyDock, regions: []string{"dock"}},
	"select":      {apply: applySelect, regions: []string{"legend", "dock", "detail", "viewport"}},
	"grid":        {apply: applyGrid, regions: []string{"grid-navigator", "dock", "viewport"}},
	"overview":    {apply: applyOverview, regions: []string{"overview"}},
	// The camera report names no region. It is a debounced upward whisper
	// rather than an interaction, and swapping any of the chrome in response
	// to a settling camera would fight the reader's own hand. It still
	// answers with the island the dispatcher appends -- an inert script node
	// with no focus to lose, no scroll to reset and nothing on screen that
	// moves -- because otherwise the camera it just wrote would be readable
	// only after the next unrelated request, and the baselines record it on
	// their very first step.
	"view":    {apply: applyView},
	"sidebar": {apply: applySidebar, regions: []string{"shell"}},
}

// formValues is the parsed body of a session POST, read through accessors so
// a concern never has to think about what a missing field looks like.
type formValues struct{ values map[string][]string }

func (f formValues) get(name string) string {
	if held := f.values[name]; len(held) > 0 {
		return strings.TrimSpace(held[0])
	}
	return ""
}

func (f formValues) has(name string) bool { _, sent := f.values[name]; return sent }

// on reads a checkbox-shaped field: present and not a negative word is on.
func (f formValues) on(name string) bool {
	switch strings.ToLower(f.get(name)) {
	case "", "0", "off", "false", "no", "closed", "hidden":
		return false
	default:
		return true
	}
}

func (f formValues) number(name string) (float64, bool) {
	value, err := strconv.ParseFloat(f.get(name), 64)
	return value, err == nil
}

// handleSession is every /session/* route. It reads the volume the
// interaction is about, applies the concern, writes the record, and answers
// with the partials the concern declares.
func (a *App) handleSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("concern")
	held, known := concerns[name]
	if !known {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the session request could not be read", http.StatusBadRequest)
		return
	}
	form := formValues{values: r.PostForm}
	// One record, one writer at a time. Everything from here to the write is
	// a read-modify-write of the same bytes.
	a.writing.Lock()
	defer a.writing.Unlock()

	slug := form.get("volume")
	if err := bundle.ValidSlug(slug); err != nil {
		http.Error(w, "the session request names no volume", http.StatusBadRequest)
		return
	}
	library := a.library()
	volume, serving := library.bySlug[slug]
	if !serving {
		// The volume left the library between the page being rendered and
		// this interaction. Say so plainly; the events stream is what tells
		// the page to catch up.
		http.Error(w, "that volume is not installed", http.StatusNotFound)
		return
	}

	session := a.session(slug)
	session.Stamp = bundle.ShortStamp(volume.Manifest().Version.Stamp)
	a.arrange(volume, &session)
	ctx := &concernContext{session: &session, world: a.world(volume, session.World)}
	if err := held.apply(ctx, form); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.saveSession(&session); err != nil {
		http.Error(w, "the session could not be written", http.StatusInternalServerError)
		return
	}

	// Every answer carries the island, because every answer has just written
	// the record the island publishes. It is inert, it is last, and it is
	// what makes the server's half of the joint diagnostics a reading of the
	// record rather than a reading of the last page render.
	a.writePartials(w, append(held.regions, "island"), a.view(library, volume, session))
}

// arrange fills a fresh record with the arrangement the world itself asks for
// before anybody has arranged it: the collections the payload marks invisible
// are put away, the viewer's own Zones section is folded, and the ungrouped
// shape rows are unfolded so their feature indexes are there the moment the
// section is opened.
//
// It runs exactly once per record, which is what Arranged records. An empty
// hide set afterwards is a reader who asked to see everything, and must not be
// mistaken for a record nobody has touched.
func (a *App) arrange(volume hostenv.Volume, s *Session) {
	if s.Arranged {
		return
	}
	if s.World == "" {
		manifest := volume.Manifest()
		if len(manifest.Worlds) == 0 {
			return
		}
		s.World = manifest.Worlds[0].Slug
	}
	model := a.world(volume, s.World)
	if model == nil {
		return
	}
	s.Hidden = defaultHidden(model)
	s.Collapsed = defaultCollapsed()
	s.Expanded = defaultExpanded(model)
	// The subdivision is shown by default: a grid the reader opens is a grid
	// with its next level drawn, and the baselines record `subgridVisible`
	// true from the first step of every volume -- before any grid is open at
	// all, because the setting outlives the grid being closed.
	if s.Grid.Subgrid == 0 {
		s.Grid.Subgrid = 1
	}
	s.Arranged = true
}

func applyWorld(c *concernContext, form formValues) error {
	world := form.get("world")
	if err := bundle.ValidSlug(world); err != nil {
		return fmt.Errorf("world: %w", err)
	}
	if world != c.session.World {
		// A world is a different ground. A selection, a highlight, a search
		// and a held cell all belong to the ground they were made on, and
		// the arrangement is the new world's to supply.
		c.session.Selected = ""
		c.session.Detail.Open = false
		c.session.Highlighted = nil
		c.session.Search = ""
		c.session.Grid.Cell = ""
		c.session.Labels = nil
		c.session.Lens = ""
		c.session.Arranged = false
	}
	c.session.World = world
	return nil
}

func applyLens(c *concernContext, form formValues) error {
	c.session.Lens = form.get("lens")
	return nil
}

// applyCollections carries every move on the hide set: one row toggled, one
// section's switch thrown, everything shown or everything put away, or the
// whole set replaced at once.
func applyCollections(c *concernContext, form formValues) error {
	s := c.session
	if replacement, sent := form.values["hidden"]; sent {
		s.Hidden = trimmed(replacement)
		return nil
	}
	switch form.get("all") {
	case "show":
		s.Hidden = nil
		return nil
	case "hide":
		s.Hidden = everyCollection(c.world)
		return nil
	}
	if section := form.get("section"); section != "" {
		s.Hidden = toggleSection(c.world, setOf(s.Hidden), section)
		return nil
	}
	id := form.get("collection")
	if id == "" {
		return errors.New("collections: no collection named")
	}
	s.Hidden = toggle(s.Hidden, id, !form.on("visible"))
	return nil
}

// applySections folds legend sections. There is one level of nesting per move
// -- sections hold rows, rows may hold a feature index -- so folding by a depth
// and folding entirely are the same move, and only the one exists.
func applySections(c *concernContext, form formValues) error {
	s := c.session
	switch form.get("all") {
	case "fold":
		s.Collapsed = everySection(c.world)
		return nil
	case "unfold":
		s.Collapsed = nil
		return nil
	}
	id := form.get("section")
	if id == "" {
		return errors.New("sections: no section named")
	}
	s.Collapsed = toggle(s.Collapsed, id, !form.on("open"))
	return nil
}

// applyExpand unfolds a shape row into its feature index.
func applyExpand(c *concernContext, form formValues) error {
	s := c.session
	switch form.get("all") {
	case "fold":
		s.Expanded = nil
		return nil
	case "unfold":
		s.Expanded = defaultExpanded(c.world)
		return nil
	}
	id := form.get("collection")
	if id == "" {
		return errors.New("expand: no collection named")
	}
	s.Expanded = toggle(s.Expanded, id, form.on("open"))
	return nil
}

func applyLabels(c *concernContext, form formValues) error {
	s := c.session
	id := form.get("collection")
	if id == "" {
		return errors.New("labels: no collection named")
	}
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	// A flip is spelled as the move rather than as the destination: the
	// policy turns over, and if the other word is what the producer curated
	// anyway the override has nothing left to say and is dropped rather
	// than stored. That is what keeps a ladder turned over and back from
	// leaving overrides behind it.
	policy := form.get("policy")
	if form.get("flip") != "" || policy == "" {
		if c.world == nil {
			return errors.New("labels: this world could not be read")
		}
		collection, held := c.world.ByID[id]
		if !held {
			return errors.New("labels: no such collection")
		}
		policy = flipLabel(collection, *s)
	}
	if policy == "" {
		delete(s.Labels, id)
	} else {
		s.Labels[id] = policy
	}
	if len(s.Labels) == 0 {
		s.Labels = nil
	}
	return nil
}

// applySolo isolates a row or a section. It is a move on the hide set rather
// than a state of its own, which is why the record grew no field for it: the
// chip the toolbar shows is derived from what is hidden, so it is right
// however that set was reached -- including by switching rows off one at a
// time -- and showing everything is the single, obvious way back.
//
// Isolation stays inside its own domain: point collections isolate against
// point collections, ground against ground, so highlighting a region and then
// asking for only one resource leaves the region standing with the resource
// inside it.
func applySolo(c *concernContext, form formValues) error {
	s := c.session
	section, collection := form.get("section"), form.get("collection")
	if section == "" && collection == "" {
		// The chip itself is the way out: pressing it shows everything.
		s.Hidden = nil
		return nil
	}
	s.Hidden = isolate(c.world, setOf(s.Hidden), section, collection)
	return nil
}

func applySearch(c *concernContext, form formValues) error {
	s := c.session
	s.Search = form.get("q")
	// A search with something to say is worth bringing the panel out for,
	// once, until the reader has said otherwise.
	if s.Search != "" {
		revealDock(s)
	}
	return nil
}

// applyHighlight is the AND-across, OR-within filter: highlighting two
// features of one collection widens the question, and one from each of two
// collections narrows it to the ground they share.
func applyHighlight(c *concernContext, form formValues) error {
	s := c.session
	if form.get("all") == "clear" {
		s.Highlighted = nil
		return nil
	}
	id := form.get("feature")
	if id == "" {
		return errors.New("highlight: no feature named")
	}
	on := !setOf(s.Highlighted)[id]
	if form.has("on") {
		on = form.on("on")
	}
	s.Highlighted = toggle(s.Highlighted, id, on)
	revealDock(s)
	return nil
}

func applyDock(c *concernContext, form formValues) error {
	s := c.session
	s.Dock.Open = form.on("open")
	// The reader working the control themselves is what settles it. Folding
	// it by hand is the answer that stops it opening again; unfolding by
	// hand withdraws that answer, so a panel put away and brought back is
	// once more a panel that keeps up with what is selected.
	if form.on("byHand") {
		s.Dock.Dismissed = !s.Dock.Open
	}
	if section := form.get("section"); section != "" {
		s.Dock.Section = section
	}
	return nil
}

// revealDock brings the panel out the first time there is something in it to
// read, and never again once the reader has put it away themselves.
func revealDock(s *Session) {
	if s.Dock.Open || s.Dock.Dismissed {
		return
	}
	s.Dock.Open = true
}

// applySelect is the hybrid interaction: the seam resolved a canvas pick and
// submits the identity it found through an ordinary request. A row in the
// legend's feature index and a row in the dock submit the same thing by the
// same route, which is why the legend is in the region set -- the index marks
// the row it opened.
func applySelect(c *concernContext, form formValues) error {
	s := c.session
	s.Selected = form.get("feature")
	s.Detail.Open = s.Selected != ""
	if s.Detail.Open {
		revealDock(s)
	}
	return nil
}

// defaultCellSystem is the system a grid opens on. Which systems exist and
// what they divide is the analysis lane's (issue #5 §5.4); which one is chosen
// is the session's, and this is the session's half saying "the first one".
const defaultCellSystem = "geohash"

func applyGrid(c *concernContext, form formValues) error {
	s := c.session
	if system, sent := form.values["system"]; sent {
		switch value := strings.TrimSpace(first(system)); value {
		case "":
			s.Grid = Grid{}
			return nil
		case "toggle":
			// The G key: a grid on a map that has none, and no grid on a map
			// that has one. Closing it takes the held cell with it, because a
			// cell nobody can see is not a place anybody is standing.
			if s.Grid.System != "" {
				s.Grid = Grid{Subgrid: s.Grid.Subgrid}
				return nil
			}
			// The subdivision is not the grid's: a reader who put it away
			// wants it away the next time they open a grid too, which is why
			// every baseline carries `subgridVisible` from its first step,
			// long before any grid is open.
			s.Grid = Grid{System: defaultCellSystem, Subgrid: s.Grid.Subgrid}
		case "cycle":
			// One system, for now. Cycling is written as a move rather than a
			// destination so the day a second system is registered the control
			// does not have to be rewritten -- only this switch.
			s.Grid.System = defaultCellSystem
		default:
			s.Grid.System = value
		}
	}
	if form.on("ascend") {
		// Escape telescopes out: one character of the address at a time, and
		// out of the grid altogether once there is no address left. The two
		// presses the tours record are exactly these two answers.
		if s.Grid.Cell == "" {
			s.Grid = Grid{Subgrid: s.Grid.Subgrid}
			return nil
		}
		runes := []rune(s.Grid.Cell)
		s.Grid.Cell = string(runes[:len(runes)-1])
		return nil
	}
	if cell, sent := form.values["cell"]; sent {
		s.Grid.Cell = strings.TrimSpace(first(cell))
	}
	if form.get("subgrid") == "flip" {
		if s.Grid.Subgrid > 0 {
			s.Grid.Subgrid = 0
		} else {
			s.Grid.Subgrid = 1
		}
		return nil
	}
	if depth, ok := form.number("subgrid"); ok {
		s.Grid.Subgrid = int(depth)
	}
	return nil
}

func applyOverview(c *concernContext, form formValues) error {
	c.session.Overview.Docked = form.on("docked")
	return nil
}

// applyView takes the camera report. It is the only upward flow from the seam
// besides a pick, it is debounced to once per settle, and it answers with no
// content.
func applyView(c *concernContext, form formValues) error {
	s := c.session
	world := form.get("world")
	if world == "" {
		world = s.World
	}
	if err := bundle.ValidSlug(world); err != nil {
		return fmt.Errorf("view: %w", err)
	}
	x, haveX := form.number("x")
	y, haveY := form.number("y")
	zoom, haveZoom := form.number("zoom")
	if !haveX || !haveY || !haveZoom {
		return errors.New("view: a camera report carries x, y and zoom")
	}
	rotation, _ := form.number("rotation")
	if s.Cameras == nil {
		s.Cameras = map[string]Camera{}
	}
	s.Cameras[world] = Camera{
		X: x, Y: y, Zoom: zoom, Rotation: rotation,
		At: time.Now().UTC().Format(time.RFC3339),
	}
	return nil
}

func applySidebar(c *concernContext, form formValues) error {
	c.session.Sidebar.Collapsed = !form.on("open")
	return nil
}

// toggle adds or removes one member of a set kept as a sorted slice.
func toggle(set []string, member string, present bool) []string {
	out := make([]string, 0, len(set)+1)
	for _, held := range set {
		if held != member {
			out = append(out, held)
		}
	}
	if present {
		out = append(out, member)
	}
	sort.Strings(out)
	return out
}

func trimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
