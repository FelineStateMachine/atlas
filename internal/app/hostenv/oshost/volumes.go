package oshost

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// closeGrace is how long a retired bundle stays open after a rescan replaces
// it. A request served out of the previous snapshot finishes in milliseconds;
// the grace period outlives any of them without keeping descriptors around.
const closeGrace = 5 * time.Second

// Volumes is a library kept as a directory of .atlas files.
//
// It scans at construction and after installation, and never watches (issue #5
// §2, decision 15): a file copied directly into the library outside Atlas
// appears at the next launch. Desktop open and drop events install immediately.
// The fold that decides which build serves is format/vnext's and is pure; this
// type is the walk, the open files, and the swap.
//
// Every rescan builds a whole snapshot and swaps it in at once, so a request
// sees one consistent library however the directory is churning.
type Volumes struct {
	dir  string
	snap atomic.Pointer[snapshot]

	// scanning runs alone: two concurrent scans of one directory would race
	// to swap snapshots and could close each other's bundles.
	scanning sync.Mutex
}

// snapshot is the library at one moment: the winning build per slug, the open
// readers behind them, and the order the handler lists them in.
type snapshot struct {
	serving map[string]vnext.Descriptor
	open    map[string]*volume
	order   []string
}

// volume is one serving build with its archive held open, so answering for an
// entry is a seek and a read rather than a fresh open of a file that may be
// hundreds of megabytes.
type volume struct {
	reader  *vnext.File
	locator string
	info    hostenv.VolumeInfo
	outline vnext.Volume
}

func (v *volume) Info() hostenv.VolumeInfo { return v.info }
func (v *volume) Outline() vnext.Volume    { return v.outline }
func (v *volume) FeaturePage(request vnext.FeaturePageRequest) (vnext.FeaturePageResult, error) {
	return v.reader.FeaturePage(request)
}
func (v *volume) FeatureSetSummaries() ([]vnext.FeatureSetSummary, error) {
	return v.reader.FeatureSetSummaries()
}
func (v *volume) DemandAddressedFeatures() bool { return v.reader.DemandAddressedFeatures() }
func (v *volume) RasterTile(entry string) (vnext.RasterTile, error) {
	return v.reader.ReadRasterTile(entry)
}
func (v *volume) Blob(entry string) ([]byte, error) {
	return v.reader.Blob(entry)
}
func (v *volume) Schema() ([]byte, error)                 { return v.reader.FileSchema.Canonical() }
func (v *volume) TableBlock(entry string) ([]byte, error) { return v.reader.TableBlock(entry) }

// NewVolumes answers for dir, which need not exist yet: a directory that is
// missing or empty is a library with no volumes, not an error. The first scan
// happens here, so a caller that gets a store gets a library.
func NewVolumes(dir string) (*Volumes, error) {
	v := &Volumes{dir: dir}
	v.snap.Store(&snapshot{serving: map[string]vnext.Descriptor{}, open: map[string]*volume{}})
	if _, err := v.Rescan(); err != nil {
		return nil, err
	}
	return v, nil
}

// Location is the directory the library is kept in.
func (v *Volumes) Location() string { return v.dir }

// Volumes lists the serving build of every installed volume, sorted by slug.
func (v *Volumes) Volumes() []hostenv.Volume {
	held := v.snap.Load()
	out := make([]hostenv.Volume, 0, len(held.order))
	for _, slug := range held.order {
		out = append(out, held.open[slug])
	}
	return out
}

// Close closes every reader the current snapshot holds and leaves the store
// empty. The desktop host never calls it — a process done with its library is
// a process that is exiting — but a caller that opened a library it must
// later delete has to let the files go first, which on Windows is a rule
// rather than a courtesy.
func (v *Volumes) Close() error {
	v.scanning.Lock()
	defer v.scanning.Unlock()
	previous := v.snap.Load()
	v.snap.Store(&snapshot{serving: map[string]vnext.Descriptor{}, open: map[string]*volume{}})
	var first error
	for _, held := range previous.open {
		if err := held.reader.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Rescan reads the directory whole and swaps in what it finds, reporting the
// volumes whose serving build moved. A bundle that will not open is logged
// and passed over rather than failing the scan: one bad download should not
// take a library down with it.
func (v *Volumes) Rescan() ([]string, error) {
	v.scanning.Lock()
	defer v.scanning.Unlock()

	descriptors, skipped, err := vnext.Scan(v.dir, vnext.StandardSchema())
	if err != nil {
		return nil, fmt.Errorf("scan library: %w", err)
	}
	for _, passed := range skipped {
		slog.Warn("bundle skipped", logging.Op("scan"),
			logging.Path(passed.Locator), slog.Any("error", passed.Err))
	}

	previous := v.snap.Load()
	winners := vnext.Fold(descriptors)
	next := &snapshot{
		serving: make(map[string]vnext.Descriptor, len(winners)),
		open:    make(map[string]*volume, len(winners)),
		order:   make([]string, 0, len(winners)),
	}
	for slug, winner := range winners {
		opened, err := carryOrOpen(previous, winner)
		if err != nil {
			slog.Warn("volume skipped", logging.Op("scan"),
				logging.Volume(slug), logging.Path(winner.Locator), slog.Any("error", err))
			continue
		}
		next.serving[slug] = winner
		next.open[slug] = opened
		next.order = append(next.order, slug)
	}
	sort.Strings(next.order)
	v.snap.Store(next)

	retire(previous, next)
	changed := vnext.Changed(previous.serving, next.serving)
	for _, slug := range changed {
		if winner, still := next.serving[slug]; still {
			slog.Info("volume serving", logging.Op("scan"),
				logging.Volume(slug), logging.Stamp(vnext.ShortStamp(winner.Stamp)))
		} else {
			slog.Info("volume gone", logging.Op("scan"), logging.Volume(slug))
		}
	}
	return changed, nil
}

// Install copies bundle content into the library under its versioned name and
// rescans. The content is staged to a temporary file first because the format
// validates a bundle before letting it in, and validation wants a file it can
// seek; the stage is removed either way.
func (v *Volumes) Install(name string, content io.Reader) (hostenv.Installed, error) {
	if err := os.MkdirAll(v.dir, 0o755); err != nil {
		return hostenv.Installed{}, fmt.Errorf("open library: %w", err)
	}
	// Staged inside the library so the install below is a rename within one
	// filesystem, and under a name that does not end in .atlas so a scan
	// running beside it passes the half-written file over.
	staged, err := os.CreateTemp(v.dir, ".importing-*")
	if err != nil {
		return hostenv.Installed{}, fmt.Errorf("stage %s: %w", name, err)
	}
	stage := staged.Name()
	defer os.Remove(stage)
	if _, err := io.Copy(staged, content); err != nil {
		staged.Close()
		return hostenv.Installed{}, fmt.Errorf("stage %s: %w", name, err)
	}
	if err := staged.Close(); err != nil {
		return hostenv.Installed{}, fmt.Errorf("stage %s: %w", name, err)
	}

	standing := v.snap.Load().serving
	descriptor, err := vnext.Install(v.dir, stage, vnext.StandardSchema())
	if err != nil {
		return hostenv.Installed{}, fmt.Errorf("%s: %w", name, err)
	}
	installed := hostenv.Installed{
		Slug:    descriptor.Slug,
		Title:   descriptor.Title,
		Stamp:   descriptor.Stamp,
		Bytes:   descriptor.Size,
		Already: standing[descriptor.Slug].Stamp == descriptor.Stamp,
	}
	slog.Info("bundle installed", logging.Op("install"),
		logging.Volume(descriptor.Slug), logging.Stamp(vnext.ShortStamp(descriptor.Stamp)),
		logging.Path(descriptor.Locator))

	changed, err := v.Rescan()
	if err != nil {
		return installed, err
	}
	installed.Changed = changed
	return installed, nil
}

// carryOrOpen reuses the reader already open for a build whose file has not
// moved, and opens the file fresh otherwise.
func carryOrOpen(previous *snapshot, winner vnext.Descriptor) (*volume, error) {
	if held, ok := previous.open[winner.Slug]; ok && held.locator == winner.Locator && held.info.Stamp == winner.Stamp {
		return held, nil
	}
	reader, err := vnext.OpenFile(winner.Locator, vnext.StandardSchema())
	if err != nil {
		return nil, err
	}
	outline, err := reader.Outline()
	if err != nil {
		reader.Close()
		return nil, err
	}
	info, err := volumeInfo(winner, outline, reader.Reader)
	if err != nil {
		reader.Close()
		return nil, err
	}
	return &volume{reader: reader, locator: winner.Locator, outline: outline, info: info}, nil
}

func volumeInfo(descriptor vnext.Descriptor, outline vnext.Volume, reader *vnext.Reader) (hostenv.VolumeInfo, error) {
	info := hostenv.VolumeInfo{
		Slug: descriptor.Slug, Title: descriptor.Title, Stamp: descriptor.Stamp,
		Release: descriptorRelease(descriptor), Worlds: make([]hostenv.WorldInfo, 0, len(outline.Worlds)),
	}
	summaries, err := reader.FeatureSetSummaries()
	if err != nil {
		return hostenv.VolumeInfo{}, fmt.Errorf("read feature summary: %w", err)
	}
	counts := make(map[string]int, len(summaries))
	for _, summary := range summaries {
		counts[summary.FeatureSet] = summary.Rows
	}
	for index, world := range outline.Worlds {
		listed := hostenv.WorldInfo{Slug: world.ID, Title: world.Title, UpdatedAt: descriptor.CreatedAt}
		for _, set := range world.FeatureSets {
			switch featureSetKind(set) {
			case semconv.GeometryPath:
				listed.Paths += counts[set.ID]
			case semconv.GeometryArea:
				listed.Areas += counts[set.ID]
			default:
				listed.Points += counts[set.ID]
			}
		}
		info.Worlds = append(info.Worlds, listed)
		if index == 0 {
			info.TileGrid = hostenv.TileGrid{
				SourceZoom: int(world.CoordinateSpace.SourceZoom),
				OriginX:    int(world.CoordinateSpace.OriginX), OriginY: int(world.CoordinateSpace.OriginY),
				FirstTile: int(world.CoordinateSpace.FirstTile),
				TileSize:  int(world.CoordinateSpace.TileSize), Size: int(world.CoordinateSpace.Size),
			}
		}
	}
	return info, nil
}

func featureSetKind(set vnext.FeatureSet) string {
	for _, claim := range set.Claims {
		if claim.Field.Name == semconv.KeyGeometryKind {
			return claim.Value.String
		}
	}
	return strings.TrimPrefix(set.SemanticType, "geometry.")
}

func descriptorRelease(descriptor vnext.Descriptor) vnext.Release {
	return vnext.Release{
		Title: descriptor.Title, CreatedAt: descriptor.CreatedAt, Revision: descriptor.Revision,
		Stamp: descriptor.Stamp, Worlds: descriptor.Worlds,
	}
}

// retire closes the readers the previous snapshot held that the new one did
// not carry across, after a grace period long enough for any request already
// streaming out of one to finish.
func retire(previous, next *snapshot) {
	var closing []*volume
	for slug, held := range previous.open {
		if carried, still := next.open[slug]; still && carried == held {
			continue
		}
		closing = append(closing, held)
	}
	if len(closing) == 0 {
		return
	}
	time.AfterFunc(closeGrace, func() {
		for _, old := range closing {
			if err := old.reader.Close(); err != nil {
				slog.Warn("closing a retired bundle", logging.Op("scan"),
					logging.Path(old.locator), slog.Any("error", err))
			}
		}
	})
}
