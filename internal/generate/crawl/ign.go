package crawl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/FelineStateMachine/atlas/internal/generate/sources/ign"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The IGN wikimap crawler.
//
// It is complete and it is not run. IGN's wikimaps are already archived, their
// endpoints are somebody else's editorial work, and a clean-room rewrite has
// nothing to learn from asking for bytes it already has. What it is for is the
// shape: this is what a game-source crawler is, so that the archive layout, the
// politeness, the resume behaviour and the source gate are written down as code
// rather than as a description of code that used to exist.
//
// # The gate
//
// Some IGN wikimap pages are MapGenie maps in an IGN frame: the page declares a
// MapGenie game id and serves MapGenie's tiles. Archiving one here would put a
// second, worse copy of data Atlas already reads properly into the archive,
// where a later merge would fold a source into itself and report a beautiful
// agreement between a thing and itself. The page's own declaration is the
// evidence, and it exists only while the crawler is looking at the page -- which
// is why the refusal lives here and the reader says it cannot check.

const (
	ignPage    = "https://www.ign.com/maps/"
	ignGraphQL = "https://mollusk.apis.ign.com/graphql"
	// ignIDBit separates IGN's identities from every other source's in the
	// archive register. The low bits are a hash of a stable name and the high
	// bit says whose space it is, so a game two publishers both describe never
	// collides on a number and every id stays under 2^53, where a JSON round
	// trip is exact.
	ignIDBit = int64(1) << 32
)

type ignCrawler struct{}

func (ignCrawler) Name() string { return "ign" }

func (ignCrawler) Usage() string {
	return "an IGN wikimap as <objectSlug>/<mapSlug>, e.g. cyberpunk-2077/night-city"
}

func (c ignCrawler) Crawl(ctx context.Context, run Run) error {
	object, mapSlug, ok := strings.Cut(run.Target, "/")
	if !ok || object == "" || mapSlug == "" {
		return fmt.Errorf("%s names a wikimap as <objectSlug>/<mapSlug>, not %q", c.Name(), run.Target)
	}
	log := run.Logger().With(logging.Source(c.Name()), logging.Op("crawl"))

	page, err := c.fetchPage(ctx, run, object, mapSlug)
	if err != nil {
		return err
	}
	capture, err := c.fetchMarkers(ctx, run, object, mapSlug, page)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return err
	}
	volume := Volume{
		ID:      ignArchiveID("ign:" + object),
		Title:   page.ObjectName,
		Locator: ignPage + object + "/" + mapSlug,
		Source:  c.Name(),
		// The same-slug policy: two publishers describe Night City, and each
		// registers the plain title while naming its directory after itself.
		DirectoryTitle: "IGN " + page.ObjectName,
	}
	world := World{
		ID:    ignArchiveID("ign:" + object + "/" + mapSlug),
		Slug:  mapSlug,
		Title: page.MapName,
	}
	if run.DryRun {
		log.Info("crawl would archive", logging.Volume(object), logging.World(mapSlug),
			"types", len(capture.Types), "markers", len(capture.Markers),
			"bytes", len(body))
		return nil
	}

	volumeDir, err := run.Archive.RegisterVolume(volume)
	if err != nil {
		return err
	}
	worldDir, err := run.Archive.RegisterWorld(volume, volumeDir, world)
	if err != nil {
		return err
	}
	hash, fresh, err := run.Archive.WriteCapture(worldDir, Capture{
		Kind:      "ign-map",
		SourceID:  world.ID,
		SourceURL: volume.Locator,
		Body:      body,
	})
	if err != nil {
		return err
	}
	log.Info("capture archived", logging.Volume(object), logging.World(mapSlug),
		logging.Stamp(hash[:12]), "fresh", fresh)

	if err := c.fetchArtwork(ctx, run, volumeDir, capture.Types, log); err != nil {
		return err
	}
	return c.fetchTiles(ctx, run, worldDir, world.ID, object, mapSlug, page, log)
}

// ignPageState is the slice of the page's embedded state a capture needs.
type ignPageState struct {
	MapType        string   `json:"mapType"`
	MapGenieGameID string   `json:"mapGenieGameId"`
	ObjectName     string   `json:"objectName"`
	MapName        string   `json:"mapName"`
	Width          float64  `json:"width"`
	Height         float64  `json:"height"`
	MinZoom        int      `json:"minZoom"`
	MaxZoom        int      `json:"maxZoom"`
	InitialLat     float64  `json:"initialLat"`
	InitialLng     float64  `json:"initialLng"`
	Background     string   `json:"backgroundColor"`
	Tilesets       []string `json:"tilesets"`
	Types          []struct {
		TypeSlug   string `json:"typeSlug"`
		TypeName   string `json:"typeName"`
		ParentSlug string `json:"parentTypeSlug"`
		MarkerIcon struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"markerIcon"`
	} `json:"types"`
}

// fetchPage reads the wikimap's page and holds it to every gate before a single
// tile is asked for.
func (c ignCrawler) fetchPage(ctx context.Context, run Run, object, mapSlug string) (ignPageState, error) {
	url := ignPage + object + "/" + mapSlug
	body, _, err := run.Fetch.Get(ctx, url, nil)
	if err != nil {
		return ignPageState{}, fmt.Errorf("read %s: %w", url, err)
	}
	const open = `<script id="__NEXT_DATA__" type="application/json">`
	_, rest, found := strings.Cut(string(body), open)
	if !found {
		return ignPageState{}, fmt.Errorf("%s carries no embedded state", url)
	}
	state, _, found := strings.Cut(rest, "</script>")
	if !found {
		return ignPageState{}, fmt.Errorf("%s carries no embedded state", url)
	}
	var envelope struct {
		Props struct {
			PageProps struct {
				Page struct {
					Map ignPageState `json:"map"`
				} `json:"page"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(state), &envelope); err != nil {
		return ignPageState{}, fmt.Errorf("decode the state of %s: %w", url, err)
	}
	page := envelope.Props.PageProps.Page.Map

	switch {
	case page.MapType != "ign":
		return ignPageState{}, fmt.Errorf(
			"%s is not a native IGN wikimap: the page calls itself %q", url, page.MapType)
	case page.MapGenieGameID != "" && page.MapGenieGameID != "null":
		// The gate. Archiving this would put a second, worse copy of data Atlas
		// already reads properly into the archive.
		return ignPageState{}, fmt.Errorf(
			"%s is a MapGenie map in an IGN frame (it declares MapGenie game %s); "+
				"crawl it through the MapGenie source instead", url, page.MapGenieGameID)
	case len(page.Tilesets) == 0:
		return ignPageState{}, fmt.Errorf("%s offers no tile set", url)
	case page.Width <= 0 || page.Height <= 0:
		return ignPageState{}, fmt.Errorf("%s draws a map with no size", url)
	case len(page.Types) == 0:
		return ignPageState{}, fmt.Errorf("%s declares no marker type", url)
	}
	template := page.Tilesets[0]
	if want := "/wikimaps/" + object + "/" + mapSlug + "/"; !strings.Contains(template, want) {
		// The archive groups tiles by the path in their URL, and the reader
		// looks them up under the same path. A template that does not carry it
		// would archive tiles nothing could find again.
		return ignPageState{}, fmt.Errorf(
			"the tile template %q does not carry %s, so its tiles could not be grouped",
			template, want)
	}
	return page, nil
}

// ignCapture is the canonical document archived for a wikimap. Its shape is the
// source reader's, because the reader is what has to read it back.
type ignCapture struct {
	Source     string            `json:"source"`
	ObjectSlug string            `json:"objectSlug"`
	MapSlug    string            `json:"mapSlug"`
	GameTitle  string            `json:"gameTitle"`
	MapTitle   string            `json:"mapTitle"`
	Map        ignCaptureMap     `json:"map"`
	Types      []ignType         `json:"types"`
	Markers    []json.RawMessage `json:"markers"`
}

type ignCaptureMap struct {
	Width           float64 `json:"width"`
	Height          float64 `json:"height"`
	MinZoom         int     `json:"minZoom"`
	MaxZoom         int     `json:"maxZoom"`
	InitialLat      float64 `json:"initialLat"`
	InitialLng      float64 `json:"initialLng"`
	BackgroundColor string  `json:"backgroundColor"`
	Tileset         string  `json:"tileset"`
}

type ignType struct {
	TypeSlug       string `json:"typeSlug"`
	TypeName       string `json:"typeName"`
	ParentTypeSlug string `json:"parentTypeSlug,omitempty"`
	IconURL        string `json:"iconUrl,omitempty"`
	IconWidth      int    `json:"iconWidth,omitempty"`
	IconHeight     int    `json:"iconHeight,omitempty"`
}

// fetchMarkers asks for every marker of every declared type in one query, and
// assembles the capture.
func (c ignCrawler) fetchMarkers(
	ctx context.Context, run Run, object, mapSlug string, page ignPageState,
) (ignCapture, error) {
	slugs := make([]string, 0, len(page.Types))
	for _, t := range page.Types {
		slugs = append(slugs, t.TypeSlug)
	}
	query := map[string]any{
		"query": `query MapMarkers($objectSlug: String!, $mapSlug: String!, $typeSlugs: [String!]!) {
  markersMultiple(objectSlug: $objectSlug, mapSlug: $mapSlug, typeSlugs: $typeSlugs) {
    id lat lng markerName markerSlug typeSlug wikiPage iconSlug regionId checklistTaskId
  }
}`,
		"variables": map[string]any{
			"objectSlug": object,
			"mapSlug":    mapSlug,
			"typeSlugs":  slugs,
		},
	}
	body, err := json.Marshal(query)
	if err != nil {
		return ignCapture{}, err
	}
	answer, _, err := run.Fetch.Post(ctx, ignGraphQL, "application/json", body)
	if err != nil {
		return ignCapture{}, fmt.Errorf("ask for the markers of %s/%s: %w", object, mapSlug, err)
	}
	var envelope struct {
		Data struct {
			Markers []json.RawMessage `json:"markersMultiple"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(answer, &envelope); err != nil {
		return ignCapture{}, fmt.Errorf("decode the markers of %s/%s: %w", object, mapSlug, err)
	}
	if len(envelope.Errors) > 0 && envelope.Errors[0].Message != "" {
		return ignCapture{}, fmt.Errorf("the marker endpoint refused: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data.Markers) == 0 {
		return ignCapture{}, fmt.Errorf("%s/%s answered with no marker at all", object, mapSlug)
	}

	capture := ignCapture{
		Source:     "ign-wikimaps",
		ObjectSlug: object,
		MapSlug:    mapSlug,
		GameTitle:  page.ObjectName,
		MapTitle:   page.MapName,
		Map: ignCaptureMap{
			Width:           page.Width,
			Height:          page.Height,
			MinZoom:         page.MinZoom,
			MaxZoom:         page.MaxZoom,
			InitialLat:      page.InitialLat,
			InitialLng:      page.InitialLng,
			BackgroundColor: page.Background,
			Tileset:         page.Tilesets[0],
		},
		Markers: envelope.Data.Markers,
	}
	for _, t := range page.Types {
		capture.Types = append(capture.Types, ignType{
			TypeSlug:       t.TypeSlug,
			TypeName:       t.TypeName,
			ParentTypeSlug: t.ParentSlug,
			IconURL:        t.MarkerIcon.URL,
			IconWidth:      t.MarkerIcon.Width,
			IconHeight:     t.MarkerIcon.Height,
		})
	}
	return capture, nil
}

// fetchArtwork stores the sprite each marker type draws with. A sprite the
// origin never published is not a failure: the collection goes without and a
// reader falls back to its own glyph.
func (c ignCrawler) fetchArtwork(
	ctx context.Context, run Run, volumeDir string, types []ignType, log *slog.Logger,
) error {
	for _, t := range types {
		if t.IconURL == "" {
			continue
		}
		extension := ".png"
		if dot := strings.LastIndex(t.IconURL, "."); dot >= 0 && dot < len(t.IconURL)-1 {
			extension = "." + strings.ToLower(t.IconURL[dot+1:])
		}
		if run.DryRun {
			continue
		}
		data, _, err := run.Fetch.Get(ctx, t.IconURL, nil)
		if errors.Is(err, ErrAbsent) {
			log.Warn("artwork not published", "key", t.TypeSlug, logging.Path(t.IconURL))
			continue
		}
		if err != nil {
			return err
		}
		if err := WriteArtwork(volumeDir, t.TypeSlug, extension, data); err != nil {
			return err
		}
	}
	return nil
}

// fetchTiles walks every level of the pyramid the page declares. A wikimap is a
// flat image and its levels are rectangles, so there is nothing to prune: what
// is asked for is exactly what the source's own frame declaration says a
// complete level holds, which is what lets the deriver judge completeness later.
func (c ignCrawler) fetchTiles(
	ctx context.Context, run Run, worldDir string, worldID int64,
	object, mapSlug string, page ignPageState, log *slog.Logger,
) error {
	if run.DryRun {
		return nil
	}
	index, err := run.Archive.OpenTileIndex(worldDir, worldID)
	if err != nil {
		return err
	}
	template := page.Tilesets[0]
	scope := ign.TileSetPath(object, mapSlug)
	format := ign.TileExtension(template)
	setID := index.SetID(scope)

	deepest := page.MaxZoom
	if run.MaxZoom > 0 && run.MaxZoom < deepest {
		deepest = run.MaxZoom
	}
	concurrency := run.Concurrency
	if concurrency < 1 {
		concurrency = DefaultConcurrency
	}

	for zoom := 0; zoom <= deepest; zoom++ {
		maxX, maxY := ign.LevelExtent(page.Width, page.Height, zoom)
		var (
			mu                       sync.Mutex
			wait                     sync.WaitGroup
			gate                     = make(chan struct{}, concurrency)
			failure                  error
			fetched, absent, skipped int
		)
		for y := 0; y <= maxY; y++ {
			for x := 0; x <= maxX; x++ {
				url := strings.NewReplacer(
					"{z}", strconv.Itoa(zoom), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y),
				).Replace(template)
				// Resume: a tile already cached, or already known not to exist,
				// is not asked for again.
				if held, known := index.Held(url); known {
					switch held.Status {
					case StatusAbsent:
						absent++
						continue
					case StatusCached:
						skipped++
						continue
					}
				}
				wait.Add(1)
				gate <- struct{}{}
				go func(zoom, x, y int, url string) {
					defer wait.Done()
					defer func() { <-gate }()
					data, contentType, err := run.Fetch.Get(ctx, url, nil)
					record := TileRecord{
						URL: url, Zoom: zoom, X: x, Y: y,
						TileSetID:   setID,
						CoverageKey: index.CoverageKey(setID, zoom, x, y),
						ContentType: "application/octet-stream",
					}
					mu.Lock()
					defer mu.Unlock()
					switch {
					case errors.Is(err, ErrAbsent):
						record.Status = StatusAbsent
						absent++
					case err != nil:
						record.Status = StatusFailed
						record.Error = err.Error()
						if failure == nil {
							failure = err
						}
					default:
						record.Status = StatusCached
						record.ContentHash = Hash(data)
						record.ContentType = contentType
						record.ByteLength = len(data)
						if writeErr := WriteTile(
							TilePath(worldDir, setID, zoom, x, y, format), data); writeErr != nil {
							failure = writeErr
							return
						}
						fetched++
					}
					index.Put(record)
				}(zoom, x, y, url)
			}
		}
		wait.Wait()
		// The index is written after every level, so a run interrupted between
		// levels resumes rather than starting over.
		if err := index.Close(worldDir); err != nil {
			return err
		}
		log.Info("level captured", logging.World(mapSlug), "zoom", zoom,
			"fetched", fetched, "skipped", skipped, "absent", absent)
		if failure != nil {
			return failure
		}
	}
	return nil
}

// ignArchiveID mints an archive identity for an IGN thing: FNV-1a over a stable
// name in the low bits, IGN's own bit above them.
func ignArchiveID(name string) int64 {
	return int64(fnv32a(name)&0x7fffffff) | ignIDBit
}
