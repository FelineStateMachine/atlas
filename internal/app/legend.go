package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// The legend, decided.
//
// One tree holds every kind of collection: shape collections, text labels and
// pin groups are sections of the same list, each holding rows of the same
// shape. Everything the reference implementation worked out in the browser --
// which sections exist, what each row counts, which rows wear a label toggle
// and which way it reads, what the solo chip says -- is worked out here, and
// the template does no arithmetic and asks no questions (issue #5 §4.5).

// The palette a collection without a colour of its own draws from. It is the
// identity's own wheel, carried with the CSS system: mid-tone hues bright
// enough to read over world tiles and muted enough not to shout over the
// neutral chrome.
var palette = [...]string{
	"#4fb3d5", "#c9924b", "#82b56a", "#c96a6a", "#9581cc",
	"#4bc9a9", "#d4b04a", "#6a92c9", "#b08a5a", "#8fb3a2",
}

// colorFor is the one colour a feature wears wherever it is drawn, so the
// legend, the index and the map cannot disagree about what it looks like. The
// hash is the reference implementation's, kept exactly, because a swatch that
// changed colour between builds would read as a data change.
func colorFor(id string) string {
	value, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return palette[0]
	}
	if value < 0 {
		value = -value
	}
	mixed := uint32(uint32(value) * 2654435761)
	return palette[mixed%uint32(len(palette))]
}

// initials is the two-letter stand-in a collection with no icon wears.
func initials(title string) string {
	out := make([]string, 0, 2)
	for _, word := range strings.Fields(title) {
		out = append(out, string([]rune(word)[:1]))
		if len(out) == 2 {
			break
		}
	}
	return strings.Join(out, "")
}

// LegendView is the whole sidebar: the tree, the counts under it, and the
// words the toolbar says about the state it is in.
type LegendView struct {
	Sections []LegendSection

	// Search is the reader's query, echoed back so the field survives a
	// swap holding what they typed.
	Search string

	// Solo is what the isolate chip says, or empty when nothing is
	// isolated. Derived rather than remembered, so it is right however the
	// state was reached -- including by switching rows off one at a time.
	Solo string

	// Count is the sidebar footer's readout: what the map is drawing.
	Count string
}

// LegendSection is one folded group of rows.
type LegendSection struct {
	Key       string
	Title     string
	Count     int
	Collapsed bool

	// Shown is whether the section's switch reads on, which it does while
	// any one of its rows is visible.
	Shown bool
	Rows  []LegendRow
}

// LegendRow is one collection.
type LegendRow struct {
	ID    string
	Title string
	Kind  string
	Count int
	Color string

	// Glyph is the two-letter stand-in a point row wears when its
	// collection carries no icon asset.
	Glyph string
	Icon  string

	Visible bool

	// Labels is the label-policy toggle, present only where a collection
	// draws names on the ground: an area, or a point collection whose
	// curation drew it as text. A row without the capability has no button
	// to press, and the column stands empty so every row still lines up.
	Labels *LabelToggle

	// Index is the feature index under a shape row: present, and rendered
	// hidden, whether or not the row is unfolded, so anything reaching for
	// a feature by name finds it without unfolding first.
	Index    []IndexEntry
	Expanded bool
	Shapes   bool
}

// LabelToggle is one row's label-policy control. The word it says is what
// pressing it would do, not what is true now.
type LabelToggle struct {
	Collection string
	Speaking   bool
	Action     string // "Hide labels" or "Show labels"
	Label      string // the spoken label, which carries the collection's name
	Policy     string // the policy pressing it would set

	// Glyph is which mark the button wears, because the two kinds of row
	// that wear one are not asking the same question. An area's names are
	// drawn *on* the ground and the toggle is whether they speak unasked,
	// which the reference drew as a bar and a stem. A point collection
	// curated as text has no marker to fall back to -- its names are the
	// whole of how it draws -- so the toggle is whether the collection is a
	// row of tags at all, and the reference drew a luggage tag. It is a word
	// rather than a boolean because the template renders a glyph per name
	// and a third kind would be a third name, not a nested `if`.
	Glyph string // glyphLabelBar or glyphLabelTag
}

// The two marks a label toggle wears. They are the reference's own
// (frontend/src/legend.js), spelled here so the template names them rather
// than deciding between them.
const (
	glyphLabelBar = "bar"
	glyphLabelTag = "tag"
)

// IndexEntry is one feature under an unfolded shape row.
type IndexEntry struct {
	ID          string
	Title       string
	Color       string
	Depth       int
	Highlighted bool
	Current     bool
}

// legend builds the tree for one world under one session, seen through one
// lens: a split world offers one layer at a time, and the feature index lists
// the layer the reader is standing on.
func legend(model *worldModel, session Session, shown visibility, lens *payloadLens) LegendView {
	out := LegendView{Search: session.Search, Count: shown.FooterText}
	if model == nil {
		return out
	}
	shard := int64(0)
	if lens != nil {
		shard = int64(lens.Shard)
	}
	hidden := setOf(session.Hidden)
	collapsed := setOf(session.Collapsed)
	expanded := setOf(session.Expanded)
	highlighted := setOf(session.Highlighted)

	for _, section := range sections(model) {
		listed := LegendSection{
			Key:       section.key,
			Title:     section.title,
			Collapsed: collapsed[section.key],
		}
		for _, collection := range section.members {
			visible := !hidden[collection.ID]
			listed.Count += collection.Count
			listed.Shown = listed.Shown || visible
			row := LegendRow{
				ID:       collection.ID,
				Title:    collection.Title,
				Kind:     collection.Kind,
				Count:    collection.Count,
				Color:    colorFor(collection.ID),
				Glyph:    initials(collection.Title),
				Icon:     collection.Icon,
				Visible:  visible,
				Shapes:   collection.Kind != semconv.GeometryPoint,
				Expanded: expanded[collection.ID],
			}
			if toggle, offered := labelToggle(collection, session); offered {
				row.Labels = &toggle
			}
			if row.Shapes {
				for _, entry := range orderedShapes(collection, shard) {
					row.Index = append(row.Index, IndexEntry{
						ID:          entry.shape.ID,
						Title:       entry.shape.Title,
						Color:       colorFor(entry.shape.ID),
						Depth:       entry.depth,
						Highlighted: highlighted[entry.shape.ID],
						Current:     session.Focused == entry.shape.ID,
					})
				}
			}
			listed.Rows = append(listed.Rows, row)
		}
		out.Sections = append(out.Sections, listed)
	}
	out.Solo = soloChip(model, hidden)
	return out
}

// labelPolicy is the ladder: the reader's override wins, then the producer's
// curated word, which format/semconv alone decides.
func labelPolicy(collection *collectionModel, session Session) string {
	if override, set := session.Labels[collection.ID]; set && override != "" {
		return override
	}
	return collection.Curated
}

// labelToggle answers whether a row wears the control at all, and how it
// reads. Only a collection whose names can draw on the ground has one: an
// area, or a point collection whose curation granted it text. A plain pin row
// simply has no button to press.
func labelToggle(collection *collectionModel, session Session) (LabelToggle, bool) {
	glyph := ""
	switch {
	case collection.Kind == semconv.GeometryArea:
		glyph = glyphLabelBar
	case collection.Kind == semconv.GeometryPoint && collection.RenderAs == semconv.RenderAsText:
		glyph = glyphLabelTag
	default:
		return LabelToggle{}, false
	}
	speaking := labelPolicy(collection, session) == semconv.LabelAlways
	toggle := LabelToggle{
		Collection: collection.ID, Speaking: speaking,
		Policy: semconv.LabelAlways, Glyph: glyph,
	}
	if speaking {
		toggle.Action, toggle.Policy = "Hide labels", semconv.LabelQuiet
		toggle.Label = "Hide " + collection.Title + " labels"
	} else {
		toggle.Action = "Show labels"
		toggle.Label = "Show " + collection.Title + " labels"
	}
	return toggle, true
}

// flipLabel is what pressing the toggle does: the policy flips to the other
// word, and if that word is what the producer curated anyway the override has
// nothing left to say and is dropped rather than stored.
func flipLabel(collection *collectionModel, session Session) string {
	flipped := semconv.LabelQuiet
	if labelPolicy(collection, session) != semconv.LabelAlways {
		flipped = semconv.LabelAlways
	}
	if flipped == collection.Curated {
		return ""
	}
	return flipped
}

// ladder is the label ladder as the golden baselines read it off the buttons:
// which collections are speaking and which are silent, sorted, over exactly
// the rows that wear a toggle.
func ladder(model *worldModel, session Session) (speaking, silent []string) {
	if model == nil {
		return nil, nil
	}
	for _, collection := range model.Members {
		toggle, offered := labelToggle(collection, session)
		if !offered {
			continue
		}
		if toggle.Speaking {
			speaking = append(speaking, collection.ID)
		} else {
			silent = append(silent, collection.ID)
		}
	}
	sort.Strings(speaking)
	sort.Strings(silent)
	return speaking, silent
}

// ---------------------------------------------------------------------------
// Sections
// ---------------------------------------------------------------------------

type legendGroup struct {
	key     string
	title   string
	members []*collectionModel
}

// sectionsKeyZones is the key of the viewer's own section: the one it makes
// for shape collections the producer left ungrouped. It is a legend key, not
// a payload word, which is why it is spelled once here.
const sectionsKeyZones = "zones"

// sections buckets a world's collections the way the legend lists them:
// groups in order of first appearance, and an ungrouped shape collection
// filed under the viewer's own Zones section, which is unshifted to the front.
//
// Text is how a point collection draws, not what it is, so a collection of
// cities rendered as floating names sits in its own group beside the markers,
// wearing the toggle that flips it either way.
func sections(model *worldModel) []legendGroup {
	var groups []legendGroup
	at := map[string]int{}
	var zones []*collectionModel
	for _, collection := range model.Members {
		if collection.Kind != semconv.GeometryPoint && collection.Group == "" {
			zones = append(zones, collection)
			continue
		}
		key := "group-" + collection.Group
		if _, held := at[key]; !held {
			at[key] = len(groups)
			groups = append(groups, legendGroup{key: key, title: collection.Group})
		}
		groups[at[key]].members = append(groups[at[key]].members, collection)
	}
	if len(zones) > 0 {
		groups = append([]legendGroup{{key: sectionsKeyZones, title: "Zones", members: zones}}, groups...)
	}
	return groups
}

// defaultCollapsed is the section set a world opens with. Zones are a
// navigation aid, not the primary filter surface: keep the boundaries drawn
// but fold their section away so pin groups stay above the fold.
func defaultCollapsed() []string { return []string{sectionsKeyZones} }

// defaultExpanded is the collection set a world opens with unfolded: the
// ungrouped shape collections, so their feature indexes are there the moment
// the section is opened.
func defaultExpanded(model *worldModel) []string {
	var out []string
	if model == nil {
		return out
	}
	for _, collection := range model.Members {
		if collection.Kind != semconv.GeometryPoint && collection.Group == "" {
			out = append(out, collection.ID)
		}
	}
	return sortedIDs(out)
}

// defaultHidden is what the payload itself asks to start put away.
func defaultHidden(model *worldModel) []string {
	var out []string
	if model == nil {
		return out
	}
	for _, collection := range model.Members {
		if collection.Hidden {
			out = append(out, collection.ID)
		}
	}
	return sortedIDs(out)
}

// ---------------------------------------------------------------------------
// The feature index
// ---------------------------------------------------------------------------

type indexed struct {
	shape *shapeModel
	depth int
}

// orderedShapes is the parent-first order of the ground itself: children
// under their parent, siblings by title, and anything orphaned appended so
// nothing is lost to a broken parent link.
//
// The index holds the ground the reader is standing on and nothing else: a
// shape belonging to another layer of a split world is *elsewhere in the
// world* rather than filtered out, and a shape carrying no geometry the chart
// can draw was never on the map to be indexed. Both are the same two questions
// the map itself asks before drawing a shape (filter.go), asked here so the
// index and the map cannot list different ground.
func orderedShapes(collection *collectionModel, shard int64) []indexed {
	standing := make([]*shapeModel, 0, len(collection.Shapes))
	for _, shape := range collection.Shapes {
		if onShard(shape.Shard, shard) && shape.Drawn {
			standing = append(standing, shape)
		}
	}
	held := map[string]bool{}
	for _, shape := range standing {
		held[shape.ID] = true
	}
	children := map[string][]*shapeModel{}
	for _, shape := range standing {
		parent := shape.Parent
		if !held[parent] {
			parent = ""
		}
		children[parent] = append(children[parent], shape)
	}
	for _, group := range children {
		sort.SliceStable(group, func(i, j int) bool {
			return compareIndexTitles(group[i].Title, group[j].Title) < 0
		})
	}

	var out []indexed
	seen := map[string]bool{}
	var append_ func(*shapeModel, int)
	append_ = func(shape *shapeModel, depth int) {
		if seen[shape.ID] {
			return
		}
		seen[shape.ID] = true
		out = append(out, indexed{shape: shape, depth: depth})
		for _, child := range children[shape.ID] {
			append_(child, depth+1)
		}
	}
	for _, shape := range children[""] {
		append_(shape, 0)
	}
	for _, shape := range standing {
		append_(shape, 0)
	}
	return out
}

// ---------------------------------------------------------------------------
// Isolating
// ---------------------------------------------------------------------------

// soloChip is the isolate readout, derived from the hide set rather than
// remembered, so it is right however the state was reached. Each domain speaks
// for itself and the chip reads them together: a region isolated among the
// shapes and a resource among the points is "Regions · Ore".
func soloChip(model *worldModel, hidden map[string]bool) string {
	var labels []string
	for _, domain := range []string{"zones", "features"} {
		var (
			onlyVisible     *collectionModel
			visibleCount    int
			hiddenCount     int
			soleSection     *legendGroup
			sectionsShowing int
		)
		groups := sections(model)
		for at := range groups {
			section := &groups[at]
			shown, held := 0, 0
			for _, collection := range section.members {
				if collection.Domain() != domain {
					continue
				}
				held++
				if hidden[collection.ID] {
					hiddenCount++
					continue
				}
				shown++
				visibleCount++
				onlyVisible = collection
			}
			if shown > 0 {
				sectionsShowing++
				soleSection = nil
				if shown == held {
					soleSection = section
				}
			}
		}
		if hiddenCount == 0 {
			continue
		}
		switch {
		case visibleCount == 1 && onlyVisible != nil:
			labels = append(labels, onlyVisible.Title)
		case visibleCount > 1 && sectionsShowing == 1 && soleSection != nil:
			labels = append(labels, soleSection.title)
		}
	}
	return strings.Join(labels, " · ")
}

// soloDomains names the ground an isolate target may touch: the target
// collection's own domain, or every domain the target section holds.
func soloDomains(model *worldModel, section, collection string) map[string]bool {
	domains := map[string]bool{}
	for _, group := range sections(model) {
		for _, member := range group.members {
			if group.key == section || member.ID == collection {
				domains[member.Domain()] = true
			}
		}
	}
	return domains
}

// isolate answers the hide set an isolate request produces. Asking to isolate
// what is already isolated means the reader is done with it, so the same
// control lets them back out -- of this domain alone. Everything else in the
// domain is hidden rather than remembered; showing everything is the single,
// obvious way back.
func isolate(model *worldModel, hidden map[string]bool, section, collection string) []string {
	domains := soloDomains(model, section, collection)
	out := map[string]bool{}
	for id, held := range hidden {
		out[id] = held
	}
	if isolated(model, hidden, section, collection) {
		for _, group := range sections(model) {
			for _, member := range group.members {
				if domains[member.Domain()] {
					delete(out, member.ID)
				}
			}
		}
	} else {
		for _, group := range sections(model) {
			wanted := group.key == section
			for _, member := range group.members {
				if !domains[member.Domain()] {
					continue
				}
				if wanted || member.ID == collection {
					delete(out, member.ID)
				} else {
					out[member.ID] = true
				}
			}
		}
	}
	return keysOf(out)
}

// isolated is true when the target's own domain already shows exactly what
// isolating it would show. Other domains are none of the target's business.
func isolated(model *worldModel, hidden map[string]bool, section, collection string) bool {
	domains := soloDomains(model, section, collection)
	for _, group := range sections(model) {
		for _, member := range group.members {
			if !domains[member.Domain()] {
				continue
			}
			wanted := group.key == section || member.ID == collection
			if wanted == hidden[member.ID] {
				return false
			}
		}
	}
	return true
}

// toggleSection is the section switch: one visible row anywhere in the section
// means pressing it puts the whole section away, and an empty section brings
// it all back.
func toggleSection(model *worldModel, hidden map[string]bool, key string) []string {
	out := map[string]bool{}
	for id, held := range hidden {
		out[id] = held
	}
	for _, group := range sections(model) {
		if group.key != key {
			continue
		}
		anyVisible := false
		for _, member := range group.members {
			if !hidden[member.ID] {
				anyVisible = true
				break
			}
		}
		for _, member := range group.members {
			if anyVisible {
				out[member.ID] = true
			} else {
				delete(out, member.ID)
			}
		}
	}
	return keysOf(out)
}

// everyCollection is the whole hide set, for "hide everything".
func everyCollection(model *worldModel) []string {
	var out []string
	if model == nil {
		return out
	}
	for _, collection := range model.Members {
		out = append(out, collection.ID)
	}
	return sortedIDs(out)
}

// everySection is every section key, for "fold everything".
func everySection(model *worldModel) []string {
	var out []string
	if model == nil {
		return out
	}
	for _, group := range sections(model) {
		out = append(out, group.key)
	}
	return sortedIDs(out)
}

func setOf(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key, held := range set {
		if held {
			out = append(out, key)
		}
	}
	return sortedIDs(out)
}
