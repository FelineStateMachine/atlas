package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/logging"
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

	// Focused is the ground the reader last went to from a list, and it is a
	// different fact from Selected: closing the card puts the selection down
	// and leaves the index still marking where the reader has been. The
	// reference kept exactly this (`focusedZoneID`), cleared it whenever the
	// ground itself was rebuilt -- a world opened, a split world's layer
	// swapped -- and never on a card being closed.
	Focused string `json:"focused,omitempty"`

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

	// whole marks the one concern whose answer is the page rather than a
	// region set. It is the reset, and it earns the exception by deleting the
	// record every other answer is computed from: there is no arrangement
	// left to render partials out of, and nothing on the page is still
	// standing for a partial to land in. A region list here would be a list
	// of every region there is, which is a page-wide refresh wearing the
	// envelope's clothes.
	whole bool
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
	"world": {apply: applyWorld, regions: []string{"topbar", "legend", "dock", "overview", "viewport"}},
	// A lens is usually a different picture of one ground and moves nothing
	// but the raster. On a split sheet it is a different *layer*, and the
	// ground under the reader changes with it: which shapes the index lists,
	// which features the panel can name, what the footer counts. The legend
	// and the dock are therefore in the set -- a lens swap that left them
	// showing the layer it came from is a page half in one world.
	"lens":        {apply: applyLens, regions: []string{"topbar", "legend", "dock", "overview", "viewport"}},
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
	// The blunt reset, and the only line in this table with no function
	// beside it: what it does is delete the record the functions above work
	// on, which is resetSession's rather than a move on a Session nobody is
	// going to write. It answers with the volume's own address again (see
	// resetSession), and a reader gets back the arrangement the world asks
	// for on a page they did not have to find their way to.
	"reset": {whole: true},
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

	if held.whole {
		a.resetSession(w, volume, slug)
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

// resetSession is the reset ⌘R presses: this volume's record is deleted, and
// the reader is sent back into the volume they are already in.
//
// It is deliberately blunt. Everything a record holds goes at once -- what is
// hidden, folded, unfolded, highlighted and labelled, the search, the panel,
// the card, the grid, the index, the corner locator, every world's camera --
// with one exception written back afterwards: the world and lens the reader
// was standing in, because a refresh is not a departure --
// because the reason to reach for it is a record that has gone wrong in a way
// nobody wants to diagnose one field at a time. Nothing synthesizes defaults
// here: the next load reads no record, and arrange builds the arrangement the
// world itself asks for, which is the one definition of fresh there is.
//
// WHAT IS NOT TOUCHED is the last-volume pointer. The reader is not leaving;
// they are standing in the same volume with nothing remembered about it, and
// deleting app.json as well would answer the next / with a library card.
//
// The answer is a redirect rather than a partial set, and the round trip is
// the point twice over: after a reset every region is stale, and a reader who
// pressed the refresh key is owed the thing refreshing does. It is spelled as
// the header htmx acts on itself -- a 303 would be followed by fetch and
// swapped into a page that asked for no swap, which is a request that appears
// to do nothing at all.
func (a *App) resetSession(w http.ResponseWriter, volume hostenv.Volume, slug string) {
	// Where to land is read before the record goes, because the record is
	// what knows which world the reader is standing in. A record that names a
	// world this build no longer serves -- or no record at all, which is a
	// reset pressed twice -- falls back to the volume's first world, and a
	// volume with no worlds to fall back to falls all the way back to /,
	// which sends the reader to the volume they were last in.
	manifest := volume.Manifest()
	held := a.session(slug)
	world := held.World
	if _, serving := worldEntry(manifest, world); !serving {
		world = ""
		if len(manifest.Worlds) > 0 {
			world = manifest.Worlds[0].Slug
		}
	}
	where := "/"
	if world != "" {
		where = "/v/" + slug + "/" + world
	}
	if err := a.env.Sessions().Delete(sessionRecord(slug)); err != nil {
		slog.Error("clearing a session record", logging.Op("session"),
			slog.String("volume", slug), slog.Any("error", err))
		http.Error(w, "the session could not be cleared", http.StatusInternalServerError)
		return
	}
	// The one thing a reset keeps is the reader's place: the world and the
	// lens they were looking at. Everything about HOW they were looking at it
	// goes; WHERE they stood does not -- a refresh that also teleported would
	// be answering a question nobody pressed the key to ask.
	if world != "" && held.Lens != "" {
		if err := a.saveSession(&Session{Volume: slug, World: world, Lens: held.Lens}); err != nil {
			slog.Warn("keeping the place through a reset", logging.Op("session"),
				slog.String("volume", slug), slog.Any("error", err))
		}
	}
	slog.Info("session reset", logging.Op("session"),
		slog.String("volume", slug), slog.String("back", where))
	w.Header().Set("HX-Redirect", where)
	w.WriteHeader(http.StatusNoContent)
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
		c.session.Focused = ""
		c.session.Detail.Open = false
		c.session.Highlighted = nil
		c.session.Search = ""
		c.session.Grid.Cell = ""
		c.session.Labels = nil
		c.session.Lens = ""
		c.session.Arranged = false
		// A map opens on the map. The panel is away until something gives it
		// a reason to come out, and a world nobody has arranged yet has not
		// been put away by hand either -- which is what lets it open itself
		// again on the first search of the new ground.
		c.session.Dock = Dock{Section: c.session.Dock.Section}
	}
	c.session.World = world
	return nil
}

func applyLens(c *concernContext, form formValues) error {
	name := form.get("lens")
	// A lens is usually a different picture of the same ground and the reader
	// keeps their place in it. A lens drawing another *layer* of a split world
	// is different ground, and the ground the reader last went to is not on it.
	if lensShard(c.world, name) != lensShard(c.world, c.session.Lens) {
		c.session.Focused = ""
	}
	c.session.Lens = name
	return nil
}

// lensShard is the layer a named lens draws, or the first lens's when the name
// is empty and nothing has been chosen yet.
func lensShard(model *worldModel, name string) int {
	if model == nil || len(model.Lenses) == 0 {
		return 0
	}
	for at := range model.Lenses {
		if model.Lenses[at].Name == name {
			return model.Lenses[at].Shard
		}
	}
	return model.Lenses[0].Shard
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
//
// THE EXCLUSIVE FORM (`only`) is the third way to ask, and it is a move on the
// highlight set rather than a state of its own -- the same shape as isolating
// a collection, and for the same reason. Highlights accumulate: every press
// adds ground, so a reader who wanted *this* district and nothing else had to
// clear the set and start again, or reach for the collection's isolate button,
// which answers a different question (it puts every other collection away).
// `only` names one feature and makes the set exactly that feature. Pressing it
// again on the feature that is already alone is the way back out, which is the
// isolate button's own toggle and means the control never traps a reader in a
// filter they cannot leave from the row they set it on.
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
	switch {
	case form.on("only"):
		on = !soleHighlight(*s, id)
		s.Highlighted = nil
		if on {
			s.Highlighted = []string{id}
		}
	default:
		if form.has("on") {
			on = form.on("on")
		}
		s.Highlighted = toggle(s.Highlighted, id, on)
	}
	// Asking to look at a piece of ground and keeping its collection put away
	// cannot both be meant, so a highlight brings the collection back.
	if on && c.world != nil {
		if shape, held := c.world.ShapeByID[id]; held {
			s.Hidden = toggle(s.Hidden, shape.Collection.ID, false)
		}
	}
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
	// A row reached for from a list says so, and going to a piece of ground
	// marks it in the index for as long as the reader is standing on it. A
	// pick off the canvas says nothing: the reader was already there.
	if form.on("focus") && c.world != nil {
		if _, ground := c.world.ShapeByID[s.Selected]; ground {
			s.Focused = s.Selected
		}
	}
	return nil
}

// defaultCellSystem is the system a grid opens on. Which systems exist and
// what they divide is the analysis lane's (issue #5 §5.4); which one is chosen
// is the session's, and this is the session's half saying "the first one".
// Geohash is the first one for the same reason it is first in the navigator:
// it divides anything, so a grid can always open.
const defaultCellSystem = cells.SystemGeohash

// worldAttrs is the world's own conventions, or none. A request that named no
// world the application could stand up still gets an answer out of the systems
// that ask the ground nothing.
func worldAttrs(world *worldModel) map[string]string {
	if world == nil {
		return nil
	}
	return world.Attrs
}

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
			// Opening it divides the cell again. A reader who put the subgrid
			// away and then closed the grid altogether is starting over rather
			// than resuming, which is the reference's own reading of the move
			// (`toggleGrid` -> `setSubgridVisible(true)` on enable). The
			// setting still outlives a *closed* grid -- the line above carries
			// it -- so a reader who only ever hides the subdivision keeps it
			// hidden for as long as the grid stays open.
			s.Grid = Grid{System: defaultCellSystem, Subgrid: 1}
		case "cycle":
			cycleGrid(c)
			return nil
		default:
			// A system named outright is the one value on this concern that
			// is not a move but a destination, and a destination has to
			// exist. Which systems divide a world is the world's answer, not
			// the session's: geohash divides anything, S2 wants a sphere with
			// an invertible flattening and refuses everything else
			// (internal/app/cells, ApplicableSystems -- the same two
			// questions the navigator asks before it offers a system at all).
			//
			// A value outside that set is refused rather than kept, and the
			// state stays exactly where it was. Keeping it would put the two
			// halves of the page in different worlds: the seam would narrow
			// the drawn set by a system the server cannot divide by, and on a
			// ground that cannot answer it the systems throw rather than
			// shrug. One request is not allowed to do that.
			attrs := worldAttrs(c.world)
			if !cells.Applicable(attrs, value) {
				slog.Warn("a grid system this world does not divide", logging.Op("session"),
					slog.String("system", value),
					slog.String("offered", strings.Join(cells.ApplicableSystems(attrs), ",")))
				return nil
			}
			setGridSystem(c, value)
		}
	}
	if form.on("ascend") {
		// Escape telescopes out: one level of the address at a time, and out
		// of the grid altogether once there is no address left. The two
		// presses the tours record are exactly these two answers.
		//
		// One level is not one character. What the parent of an address is, is
		// the address's own system's business (grid.go, parentCell), and the
		// only reason to say so twice is that the shape of a geohash makes the
		// wrong answer look right.
		if s.Grid.System == "" {
			return nil
		}
		if s.Grid.Cell == "" {
			s.Grid = Grid{Subgrid: s.Grid.Subgrid}
			return nil
		}
		s.Grid.Cell = parentCell(s.Grid.System, s.Grid.Cell)
		return nil
	}
	if cell, sent := form.values["cell"]; sent {
		selectGridCell(s, first(cell))
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

// selectGridCell is the navigator's field arriving: whatever the reader has
// typed, made into an address by the system holding the grid.
//
// Nothing here refuses a keystroke and nothing here reports one. A draft that
// is not yet a place -- half an S2 token -- leaves the record exactly as it
// was, which is the field holding the text while the map stays put; the next
// keystroke either completes the address or does not. And a cell arriving at a
// grid nobody has opened opens it, on the system that divides anything,
// *without* restoring the subdivision: the reader went somewhere rather than
// starting over, which is the one thing that separates this from the G key
// (`selectGridCell` in the reference, which sets `gridEnabled` by hand for
// exactly this reason).
func selectGridCell(s *Session, raw string) {
	system := s.Grid.System
	if system == "" {
		system = defaultCellSystem
	}
	cell, place := parseCell(system, normalizeCell(system, raw))
	if !place {
		return
	}
	s.Grid.Cell, s.Grid.System = cell, system
}

// setGridSystem changes which system divides the map, and carries the held
// place across.
//
// The two hierarchies share no boundaries, so the address cannot survive; the
// ground under the reader can, and does (internal/app/cells, Equivalent). The
// view is left alone on purpose -- the carried cell covers roughly the ground
// already on screen, and a camera move here would be the map jumping under a
// reader who asked for nothing of the kind.
func setGridSystem(c *concernContext, system string) {
	s := c.session
	if system == s.Grid.System {
		return
	}
	from, held := s.Grid.System, s.Grid.Cell
	s.Grid.System = system
	if from == "" || held == "" {
		return
	}
	s.Grid.Cell = cells.Equivalent(gridGround(c), worldAttrs(c.world), from, system, held)
}

// cycleGrid is the navigator's one button and the ⌘G it answers to: the next
// system this world divides by, carrying the held place.
//
// It is a move rather than a destination, which is why it can never be
// refused: the step is taken through the world's own list of systems, so a
// world offering one has nowhere to step and a world offering two steps
// between them. A grid nobody has opened has no system to cycle, and the
// button that would have said so is not on the page.
func cycleGrid(c *concernContext) {
	s := c.session
	if s.Grid.System == "" {
		return
	}
	next := nextSystem(cells.ApplicableSystems(worldAttrs(c.world)), s.Grid.System)
	if next == "" {
		return
	}
	setGridSystem(c, next)
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

// soleHighlight answers whether one feature is the whole of what is
// highlighted. It is the exclusive control's own state, and it is derived
// rather than stored for the same reason the isolate chip is: the set can be
// arrived at by any route -- one press of `only`, or highlights added and
// taken away one at a time -- and a reader looking at a lone highlighted zone
// is looking at an exclusive one however they got there. Both the move
// (applyHighlight) and the mark the control wears (legend.go) ask this, so
// they cannot disagree about what pressing it would do.
func soleHighlight(s Session, id string) bool {
	return len(s.Highlighted) == 1 && s.Highlighted[0] == id
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
