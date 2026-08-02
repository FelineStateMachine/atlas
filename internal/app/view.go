package app

import (
	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// View is what every region is rendered with: the library, the volume and
// world in view, and the session that colours them. One type for every region
// is deliberate -- a region is a view of the same application state, not of a
// bespoke payload -- and it is the seam between "the handler decided" and "the
// template rendered".
//
// Everything a template needs must already be a field here. A template that
// has to compute is a decision that belongs in Go (issue #5 §4.5).
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
	if _, held := worldEntry(manifest, world); !held {
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
	out.Volume = &shown
	out.Title = manifest.Volume.Title
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
