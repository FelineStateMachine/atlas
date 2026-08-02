package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// One answer to what the reader is looking at.
//
// Three surfaces used to work this out for themselves: the canvas drew what
// survived every filter, the footer counted what the legend allowed, and the
// dock listed what the search left. So highlighting one district could cull
// sixty features off the map while the panel beside it went on offering all
// sixty. Every surface asks here now, once, on the server, and a filter that
// lands on one of them lands on all of them.

// dockListLimit is how many rows the dock lists. Every feature still draws on
// the canvas; the list is a shortlist, and it says when it is one.
const dockListLimit = 100

// visibility is what one session leaves standing.
type visibility struct {
	// Points and Shapes are what the map is drawing, by kind.
	Points []*pointModel
	Shapes []*shapeModel

	// Drawn is the two counted together: one number, every kind.
	Drawn int

	// Eligible is the point features the filters admit, which is the
	// count the golden diagnostics call eligibleLocations.
	Eligible int

	// Listable is the part a list can name, alphabetically.
	Listable []ListEntry

	// Filtered says whether anything but the reader's own search is
	// narrowing the list -- a collection put away, ground highlighted, a
	// cell holding the view -- so a count that drops always has its reason
	// on screen beside it.
	Filtered bool

	FooterText string
	DockText   string
	DockFlag   string
	DockNote   string
	Empty      string
}

// ListEntry is one row of the dock: a point or a shape, told apart by which
// identity it carries, because they are jumped to by different routes.
type ListEntry struct {
	ID         string
	Title      string
	Kind       string
	Collection string
	Group      string
	Color      string
	Glyph      string
	Icon       string
	Selected   bool
}

// visible works out what the world's filters leave standing.
//
// The order of the questions is the reference implementation's, and each one
// is a different kind of narrowing:
//
//   - the collection is put away, which takes its features off every surface;
//   - the search does not match, which takes a point off the map and out of
//     the list but never takes ground out from under it;
//   - ground is highlighted, which asks a conjunctive question of every point;
//   - a shard other than the lens's holds the feature, which means it is
//     elsewhere in the world rather than filtered out.
func visible(model *worldModel, session Session, lens *payloadLens) visibility {
	out := visibility{}
	if model == nil {
		out.FooterText, out.DockText = "No features shown", "0 features"
		out.Empty = "Nothing is shown. Bring a collection back to list its features."
		return out
	}
	hidden := setOf(session.Hidden)
	query := strings.ToLower(strings.TrimSpace(session.Search))
	highlights := highlightGroups(model, session)
	shard := int64(0)
	if lens != nil {
		shard = int64(lens.Shard)
	}
	// The held cell narrows exactly the way a highlight does, and it spares
	// the same two answers: the feature the reader has open, and one they are
	// searching for by name.
	cell := heldCell(session, lens, float64(model.Grid.Size))

	for _, pin := range model.Points {
		if !onShard(pin.Shard, shard) || hidden[pin.Collection.ID] {
			continue
		}
		searched := query != "" && strings.Contains(strings.ToLower(pin.Title), query)
		if query != "" && !searched {
			continue
		}
		// A highlight asks a conjunctive question of every point. Two
		// answers are exempt from it: the point the reader has open, and
		// a point they are searching for by name -- both were asked for
		// rather than merely drawn.
		if len(highlights) > 0 && !passesHighlights(highlights, pin.X, pin.Y) &&
			pin.ID != session.Selected && !searched {
			continue
		}
		if cell != nil && !cell.Holds(pin.X, pin.Y) &&
			pin.ID != session.Selected && !searched {
			continue
		}
		out.Points = append(out.Points, pin)
	}
	out.Eligible = len(out.Points)

	// Ground is drawn while its collection is shown. Highlighting narrows
	// which points stand, not which ground is drawn, and so does a held cell:
	// descending into a cell is a question about what is standing in it, and
	// the ground it is standing on goes on being the ground. The shard is the
	// one question ground answers the same way a point does -- a shape on
	// another lens's layer is elsewhere in the world, not filtered out, so it
	// is neither drawn nor counted nor listed.
	for _, shape := range model.Shapes {
		if !onShard(shape.Shard, shard) || hidden[shape.Collection.ID] || !shape.Drawn {
			continue
		}
		out.Shapes = append(out.Shapes, shape)
	}
	out.Drawn = len(out.Points) + len(out.Shapes)

	for _, pin := range out.Points {
		out.Listable = append(out.Listable, ListEntry{
			ID:         pin.ID,
			Title:      pin.Title,
			Kind:       semconv.GeometryPoint,
			Collection: pin.Collection.Title,
			Group:      pin.Collection.Group,
			Color:      colorFor(pin.Collection.ID),
			Glyph:      initials(pin.Collection.Title),
			Icon:       pin.Collection.Icon,
			Selected:   pin.ID == session.Selected,
		})
	}
	// A shape answers the search here rather than on the canvas -- searching
	// for a place has never taken the ground out from under it -- and one the
	// archive left untitled is on the map without being in any index of it.
	for _, shape := range out.Shapes {
		if shape.Title == "" {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(shape.Title), query) {
			continue
		}
		out.Listable = append(out.Listable, ListEntry{
			ID:         shape.ID,
			Title:      shape.Title,
			Kind:       shape.Collection.Kind,
			Collection: shape.Collection.Title,
			Color:      colorFor(shape.ID),
			Selected:   shape.ID == session.Selected,
		})
	}
	sort.SliceStable(out.Listable, func(i, j int) bool {
		return compareTitles(out.Listable[i].Title, out.Listable[j].Title) < 0
	})

	out.Filtered = len(session.Hidden) > 0 || len(session.Highlighted) > 0 ||
		(session.Grid.System != "" && session.Grid.Cell != "")
	out.FooterText = countText(out.Drawn)
	out.DockText = countText(len(out.Listable))
	switch {
	case session.Search != "":
		out.DockFlag = "“" + session.Search + "”"
	case out.Filtered:
		out.DockFlag = "filtered"
	}
	if held := len(out.Listable); held > dockListLimit {
		out.DockNote = "First " + strconv.Itoa(dockListLimit) + " of " +
			strconv.Itoa(held) + " — search or filter to narrow the list."
	}
	switch {
	case len(out.Listable) > 0:
	case session.Search != "":
		out.Empty = "No visible features match."
	case len(session.Highlighted) > 0:
		out.Empty = "Nothing stands inside the highlighted ground."
	default:
		out.Empty = "Nothing is shown. Bring a collection back to list its features."
	}
	return out
}

// Rows is the shortlist the dock actually renders.
func (v visibility) Rows() []ListEntry {
	if len(v.Listable) > dockListLimit {
		return v.Listable[:dockListLimit]
	}
	return v.Listable
}

func countText(n int) string {
	word := " features"
	if n == 1 {
		word = " feature"
	}
	return grouped(n) + word
}

// grouped writes a count the way the chrome has always written one: thousands
// separated by commas, so a reader takes in "2,048" at a glance rather than
// counting digits. Every count on the page goes through here.
func grouped(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var out []byte
	for i, digit := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digit)
	}
	return sign + string(out)
}

// onShard answers whether a feature belongs to the shard the lens is showing.
// A map split into layers offers one at a time, and anything belonging to
// another layer is elsewhere in the world rather than merely filtered out.
func onShard(feature, lens int64) bool {
	return lens == 0 || feature == 0 || feature == lens
}

// ---------------------------------------------------------------------------
// AND across, OR within
// ---------------------------------------------------------------------------

// highlightGroups buckets the highlighted features under their collections,
// which is the shape the filter thinks in: each bucket a question, its members
// the acceptable answers.
//
// Two districts highlighted widens the question; a district and a subwatershed
// narrows it to the ground they share.
func highlightGroups(model *worldModel, session Session) [][]*shapeModel {
	if len(session.Highlighted) == 0 {
		return nil
	}
	at := map[string]int{}
	var groups [][]*shapeModel
	for _, id := range sortedIDs(session.Highlighted) {
		shape, held := model.ShapeByID[id]
		if !held {
			continue
		}
		key := shape.Collection.ID
		if _, seen := at[key]; !seen {
			at[key] = len(groups)
			groups = append(groups, nil)
		}
		groups[at[key]] = append(groups[at[key]], shape)
	}
	return groups
}

// passesHighlights answers whether a coordinate survives the highlights: it
// must lie inside at least one highlighted feature of every collection holding
// any. Within a collection the features are alternatives; across collections
// they are conditions.
func passesHighlights(groups [][]*shapeModel, x, y float64) bool {
	for _, group := range groups {
		inside := false
		for _, shape := range group {
			if shape.contains(x, y) {
				inside = true
				break
			}
		}
		if !inside {
			return false
		}
	}
	return true
}
