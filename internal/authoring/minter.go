package authoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/minting"
)

// NativeMinterOptions names the app-owned recipe, evidence, and library
// locations. They are supplied by the desktop host and never cross the HTTP
// application boundary.
type NativeMinterOptions struct {
	RecipesDir string
	CacheDir   string
	LibraryDir string
}

// NativeMinter turns one compact request into a durable single manifest and
// runs the same deterministic builder used by the CLI and Workbench.
type NativeMinter struct {
	options NativeMinterOptions
	build   func(context.Context, BuildOptions) (BuildResult, error)
	mu      sync.Mutex
}

var _ minting.Minter = (*NativeMinter)(nil)

// NewNativeMinter prepares the three app-owned locations.
func NewNativeMinter(options NativeMinterOptions) (*NativeMinter, error) {
	for name, directory := range map[string]string{
		"recipes": options.RecipesDir, "cache": options.CacheDir, "library": options.LibraryDir,
	} {
		if strings.TrimSpace(directory) == "" {
			return nil, fmt.Errorf("native minter %s directory is empty", name)
		}
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create native minter %s directory: %w", name, err)
		}
	}
	return &NativeMinter{options: options, build: Build}, nil
}

// Default is the neutral, immediately valid starting request.
func (m *NativeMinter) Default() minting.Request {
	profile := DefaultAreaProfile()
	return minting.Request{
		Title: "My Atlas", Bounds: profile.Bounds, DetailZoom: profile.DetailZoom,
		Topo: profile.IncludeTopo, Roads: profile.IncludeRoads,
		Hydro: profile.IncludeHydro, Counties: profile.IncludeCounties,
		RoadLabel: profile.Presentation.RoadLabel, HydroLabel: profile.Presentation.HydroLabel,
		CountyLabel: profile.Presentation.CountyLabel, RoadColor: profile.Presentation.RoadColor,
		HydroColor: profile.Presentation.HydroColor, HydroFill: profile.Presentation.HydroFill,
		CountyColor: profile.Presentation.CountyColor,
	}
}

// Preview plans the exact project Mint will persist without touching network
// evidence or the library.
func (m *NativeMinter) Preview(request minting.Request) (minting.Preview, error) {
	_, summary, err := mintProject(request)
	if err != nil {
		return minting.Preview{}, err
	}
	return minting.Preview{
		Pixels: summary.PixelSize, RasterTiles: summary.RasterTiles,
		Requests: summary.CaptureRequests, EstimatedBytes: summary.EstimatedBytes,
	}, nil
}

// Mint serializes builds that share one cache and library. It atomically
// replaces the app-managed manifest before invoking the ordinary builder, so
// a failed capture leaves an inspectable recipe and no partial Atlas.
func (m *NativeMinter) Mint(ctx context.Context, request minting.Request, emit func(minting.Event)) (minting.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	project, _, err := mintProject(request)
	if err != nil {
		return minting.Result{}, err
	}
	descriptors, _, err := vnext.Scan(m.options.LibraryDir, vnext.StandardSchema())
	if err != nil {
		return minting.Result{}, fmt.Errorf("scan Atlas library before mint: %w", err)
	}
	project.Release.Revision = nextMintRevision(descriptors, project.ID)
	manifest, err := MarshalProjectYAML(project)
	if err != nil {
		return minting.Result{}, err
	}
	projectPath := filepath.Join(m.options.RecipesDir, project.ID+ProjectExtension)
	if err := writeManagedManifest(projectPath, manifest); err != nil {
		return minting.Result{}, err
	}
	result, err := m.build(ctx, BuildOptions{
		ProjectPath: projectPath, CacheDir: m.options.CacheDir, LibraryDir: m.options.LibraryDir,
		Event: func(event Event) {
			if emit != nil {
				emit(minting.Event{
					Stage: event.Stage, Message: event.Message, Current: event.Current,
					Total: event.Total, Bytes: event.Bytes, Cached: event.Cached, Artifact: event.Artifact,
				})
			}
		},
	})
	if err != nil {
		return minting.Result{}, err
	}
	return minting.Result{
		Slug: result.Descriptor.Slug, World: project.Target.World,
		Title: result.Descriptor.Title, Stamp: result.Descriptor.Stamp,
	}, nil
}

func nextMintRevision(descriptors []vnext.Descriptor, slug string) int {
	revision := 1
	for _, descriptor := range descriptors {
		if descriptor.Slug == slug && descriptor.Revision >= revision {
			revision = descriptor.Revision + 1
		}
	}
	return revision
}

func mintProject(request minting.Request) (Project, AreaSummary, error) {
	id := mintSlug(request.Title)
	if err := vnext.ValidSlug(id); err != nil {
		return Project{}, AreaSummary{}, fmt.Errorf("Atlas name: %w", err)
	}
	profile := DefaultAreaProfile()
	profile.ID, profile.Title = id, strings.TrimSpace(request.Title)
	profile.Bounds, profile.DetailZoom = request.Bounds, request.DetailZoom
	profile.IncludeTopo, profile.IncludeRoads = request.Topo, request.Roads
	profile.IncludeHydro, profile.IncludeCounties = request.Hydro, request.Counties
	if request.RoadLabel != "" {
		profile.Presentation.RoadLabel = request.RoadLabel
	}
	if request.HydroLabel != "" {
		profile.Presentation.HydroLabel = request.HydroLabel
	}
	if request.CountyLabel != "" {
		profile.Presentation.CountyLabel = request.CountyLabel
	}
	if request.RoadColor != "" {
		profile.Presentation.RoadColor = request.RoadColor
	}
	if request.HydroColor != "" {
		profile.Presentation.HydroColor = request.HydroColor
	}
	if request.HydroFill != "" {
		profile.Presentation.HydroFill = request.HydroFill
	}
	if request.CountyColor != "" {
		profile.Presentation.CountyColor = request.CountyColor
	}
	return NewAreaProject(profile)
}

func mintSlug(title string) string {
	var out strings.Builder
	dash := false
	for _, character := range strings.ToLower(strings.TrimSpace(title)) {
		if character <= unicode.MaxASCII && (unicode.IsLetter(character) || unicode.IsDigit(character)) {
			if out.Len() >= 48 {
				break
			}
			out.WriteRune(character)
			dash = false
			continue
		}
		if out.Len() > 0 && !dash {
			out.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func writeManagedManifest(path string, data []byte) (err error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".atlas-project-*")
	if err != nil {
		return fmt.Errorf("create managed manifest: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0o644); err != nil {
		return fmt.Errorf("set managed manifest permissions: %w", err)
	}
	if _, err = temporary.Write(data); err != nil {
		return fmt.Errorf("write managed manifest: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync managed manifest: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close managed manifest: %w", err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish managed manifest: %w", err)
	}
	directoryFile, openErr := os.Open(directory)
	if openErr != nil {
		return fmt.Errorf("open managed manifest directory: %w", openErr)
	}
	defer directoryFile.Close()
	if syncErr := directoryFile.Sync(); syncErr != nil {
		return fmt.Errorf("sync managed manifest directory: %w", syncErr)
	}
	return nil
}
