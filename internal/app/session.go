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
const SessionSchema = 1

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

	// Hidden collections and Collapsed sections are named by their ids in
	// the world payload; both are kept sorted so a record is stable to diff.
	Hidden    []string `json:"hidden,omitempty"`
	Collapsed []string `json:"collapsed,omitempty"`

	// Labels is the per-collection label-policy override: collection id to
	// policy name. An absent entry means the policy the conventions imply.
	Labels map[string]string `json:"labels,omitempty"`

	// Solo names the domains soloed. Empty is the ordinary case: everything
	// visible that is not hidden.
	Solo []string `json:"solo,omitempty"`

	Search   string  `json:"search,omitempty"`
	Dock     Dock    `json:"dock"`
	Detail   Detail  `json:"detail"`
	Grid     Grid    `json:"grid"`
	Sidebar  Sidebar `json:"sidebar"`
	Selected string  `json:"selected,omitempty"`

	// Cameras is the last settled camera per world, reported by the seam.
	// It is the one piece of continuous state the server keeps, and it is
	// kept because a reader expects a volume to open where they left it.
	Cameras map[string]Camera `json:"cameras,omitempty"`

	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Dock is the readout beside the map: whether it is open and which section of
// it is showing.
type Dock struct {
	Open    bool   `json:"open"`
	Section string `json:"section,omitempty"`
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

// Sidebar is the legend column.
type Sidebar struct {
	Open bool `json:"open"`
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
	sort.Strings(s.Solo)
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
type concern struct {
	apply   func(s *Session, form formValues) error
	regions []string

	// quiet marks a concern that answers with no content at all: the camera
	// report is a debounced upward whisper, not an interaction, and swapping
	// anything in response to it would fight the reader's own hand.
	quiet bool
}

// concerns is the table of session routes. One line per concern; adding an
// interaction is adding a line and a function.
var concerns = map[string]concern{
	"world":       {apply: applyWorld, regions: []string{"topbar", "legend", "dock", "overview", "viewport"}},
	"lens":        {apply: applyLens, regions: []string{"topbar", "viewport"}},
	"collections": {apply: applyCollections, regions: []string{"legend", "dock", "viewport"}},
	"sections":    {apply: applySections, regions: []string{"legend"}},
	"labels":      {apply: applyLabels, regions: []string{"legend", "viewport"}},
	"solo":        {apply: applySolo, regions: []string{"topbar", "legend", "viewport"}},
	"search":      {apply: applySearch, regions: []string{"legend", "dock", "viewport"}},
	"dock":        {apply: applyDock, regions: []string{"dock"}},
	"select":      {apply: applySelect, regions: []string{"detail", "dock", "viewport"}},
	"grid":        {apply: applyGrid, regions: []string{"grid-navigator", "dock", "viewport"}},
	"view":        {apply: applyView, quiet: true},
	"sidebar":     {apply: applySidebar, regions: []string{"shell"}},
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

func (f formValues) all(name string) []string { return f.values[name] }

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
	if err := held.apply(&session, form); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.saveSession(&session); err != nil {
		http.Error(w, "the session could not be written", http.StatusInternalServerError)
		return
	}

	if held.quiet {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	a.writePartials(w, held.regions, a.view(library, volume, session))
}

func applyWorld(s *Session, form formValues) error {
	world := form.get("world")
	if err := bundle.ValidSlug(world); err != nil {
		return fmt.Errorf("world: %w", err)
	}
	if world != s.World {
		// A selection belongs to the ground it was made on.
		s.Selected = ""
		s.Detail.Open = false
	}
	s.World = world
	return nil
}

func applyLens(s *Session, form formValues) error {
	s.Lens = form.get("lens")
	return nil
}

// applyCollections carries the legend's one operation: a collection is hidden
// or it is not. The whole hidden set may also be sent at once, which is what a
// "hide everything else" affordance sends.
func applyCollections(s *Session, form formValues) error {
	if replacement, sent := form.values["hidden"]; sent {
		s.Hidden = trimmed(replacement)
		return nil
	}
	id := form.get("collection")
	if id == "" {
		return errors.New("collections: no collection named")
	}
	s.Hidden = toggle(s.Hidden, id, !form.on("visible"))
	return nil
}

func applySections(s *Session, form formValues) error {
	id := form.get("section")
	if id == "" {
		return errors.New("sections: no section named")
	}
	s.Collapsed = toggle(s.Collapsed, id, !form.on("open"))
	return nil
}

func applyLabels(s *Session, form formValues) error {
	collection := form.get("collection")
	if collection == "" {
		return errors.New("labels: no collection named")
	}
	policy := form.get("policy")
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	if policy == "" {
		delete(s.Labels, collection)
		return nil
	}
	s.Labels[collection] = policy
	return nil
}

func applySolo(s *Session, form formValues) error {
	if replacement, sent := form.values["solo"]; sent {
		s.Solo = trimmed(replacement)
		return nil
	}
	domain := form.get("domain")
	if domain == "" {
		return errors.New("solo: no domain named")
	}
	s.Solo = toggle(s.Solo, domain, form.on("on"))
	return nil
}

func applySearch(s *Session, form formValues) error {
	s.Search = form.get("q")
	return nil
}

func applyDock(s *Session, form formValues) error {
	s.Dock.Open = form.on("open")
	if section := form.get("section"); section != "" {
		s.Dock.Section = section
	}
	return nil
}

// applySelect is the hybrid interaction: the seam resolved a canvas pick and
// submits the identity it found through an ordinary request.
func applySelect(s *Session, form formValues) error {
	s.Selected = form.get("feature")
	s.Detail.Open = s.Selected != ""
	return nil
}

func applyGrid(s *Session, form formValues) error {
	if system, sent := form.values["system"]; sent {
		s.Grid.System = strings.TrimSpace(first(system))
		if s.Grid.System == "" {
			s.Grid = Grid{}
			return nil
		}
	}
	if cell, sent := form.values["cell"]; sent {
		s.Grid.Cell = strings.TrimSpace(first(cell))
	}
	if depth, ok := form.number("subgrid"); ok {
		s.Grid.Subgrid = int(depth)
	}
	return nil
}

// applyView takes the camera report. It is the only upward flow from the seam
// besides a pick, it is debounced to once per settle, and it answers with no
// content.
func applyView(s *Session, form formValues) error {
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

func applySidebar(s *Session, form formValues) error {
	s.Sidebar.Open = form.on("open")
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
