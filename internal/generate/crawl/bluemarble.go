package crawl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources/nasabluemarble"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The Blue Marble crawler: the one networked step behind the included Earth
// volume.
//
// Every other crawler follows a publisher wherever its endpoints lead. This one
// is aimed at a single pinned publication -- NASA Earth Observatory's Blue
// Marble Next Generation base map, July 2004, one image at one URL with one
// digest -- and it refuses anything else. The digest is the whole contract:
// bytes that do not hash to the pin are not the pinned publication, whatever
// the URL served, and accepting them silently would put an unaccounted picture
// into every library that ships the included volume.
//
// What lands in the archive is the ordinary shape every capture takes: a
// capture body naming the product, and the reference-level tiles the deriver
// folds down -- the source image cut to the shared whole-sphere window through
// the deterministic resampler beside this file. The downloaded source itself is
// kept beside the capture, content-addressed, so a re-run derives without
// asking NASA again; it is a working file of the archive, never part of a
// bundle and never read at runtime.

// blueMarblePin is everything fixed about the capture: where the publication
// is, what its bytes must hash to, when it was first captured for this
// artifact, and the derivation policy the tiles are cut under. It is a struct
// rather than constants so the write path can be exercised against a stated
// pin; the shipped pin is blueMarble below.
type blueMarblePin struct {
	// Page is the product's own page, and Asset the exact image fetched.
	Page  string
	Asset string
	// SHA256 is the digest the fetched bytes must carry. A mismatch is a
	// refusal, never a warning.
	SHA256 string
	// CapturedAt is the moment the pinned publication was first captured for
	// this artifact. It is part of the pin so the archive -- and everything
	// stamped downstream of it -- reproduces on any machine.
	CapturedAt string
	// Width and Height are the source image's declared pixels.
	Width, Height int
	// Quality is the JPEG setting the reference tiles are encoded at. It is
	// spelled here, once, because two runs of one pipeline may not disagree.
	Quality int
}

// blueMarble is the shipped pin: Blue Marble Next Generation with topography
// and bathymetry, July 2004, as published by NASA Earth Observatory -- the
// variant whose shaded relief and ocean floor give the planet its texture,
// which is also the rendering NASA's own maps lead with. included/README.md is
// the recipe that carries these same facts for a person.
var blueMarble = blueMarblePin{
	Page:       "https://science.nasa.gov/earth/earth-observatory/blue-marble-next-generation/base-topography-bathymetry/",
	Asset:      "https://assets.science.nasa.gov/content/dam/science/esd/eo/images/bmng/bmng-topography-bathymetry/july/world.topo.bathy.200407.3x21600x10800.jpg",
	SHA256:     "d225f1f35a6448a4d1d8f6de6e48f3433e470085b70a35800e64f384f269a7b0",
	CapturedAt: "2026-08-03T15:27:26Z",
	Width:      21600,
	Height:     10800,
	Quality:    85,
}

// blueMarbleResampler names the cut for the capture body: the resampler beside
// this file, at its fixed-point precision. A change to how the reference level
// is cut renames this, and the renamed policy reads as a new capture.
const blueMarbleResampler = "catmull-rom-fixed15"

// blueMarbleZoom is the level the source is cut to: one deeper than the
// reference level whose pixels are the world square's own, so the pyramid
// holds a 16384×8192 picture the 21600-wide source still fills with real
// detail. The deriver folds the reference level and everything shallower down
// from this cut.
const blueMarbleZoom = doc.SyntheticZoom + 1

// blueMarbleIDBit marks Blue Marble identities in the archive register, beside
// the bits the other sources' identities carry.
const blueMarbleIDBit = int64(1) << 36

type blueMarbleCrawler struct{}

func (blueMarbleCrawler) Name() string { return nasabluemarble.SourceName }

func (blueMarbleCrawler) Usage() string {
	return "the pinned Blue Marble base map; the target is \"earth\""
}

func (c blueMarbleCrawler) Crawl(ctx context.Context, run Run) error {
	if run.Target != nasabluemarble.Body {
		return fmt.Errorf("%s captures the pinned Earth base map; the target is %q, not %q",
			c.Name(), nasabluemarble.Body, run.Target)
	}
	log := run.Logger().With(logging.Source(c.Name()), logging.Op("crawl"))

	volume := Volume{
		ID:      blueMarbleArchiveID("bluemarble:" + nasabluemarble.Body),
		Title:   doc.Title(nasabluemarble.Body),
		Locator: blueMarble.Page,
		Source:  c.Name(),
		// The same-slug policy: any source describing the Earth registers the
		// plain title while naming its directory after itself.
		DirectoryTitle: "NASA Blue Marble Earth",
	}
	world := World{
		ID:    blueMarbleArchiveID("bluemarble:" + nasabluemarble.Body + "/" + nasabluemarble.Body),
		Slug:  nasabluemarble.Body,
		Title: doc.Title(nasabluemarble.Body),
	}
	if run.DryRun {
		log.Info("crawl would archive", logging.Volume(volume.Title), logging.World(world.Slug),
			"asset", blueMarble.Asset, "sha256", blueMarble.SHA256)
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
	source, err := c.source(ctx, run, worldDir, log)
	if err != nil {
		return err
	}
	if err := writeBlueMarble(run.Archive, worldDir, world.ID, blueMarble, source, log); err != nil {
		return err
	}
	return nil
}

// source answers the pinned image's bytes: the copy already kept beside the
// capture when there is one, the origin exactly once when there is not. Either
// way the bytes are held to the pinned digest before anything is derived from
// them.
func (c blueMarbleCrawler) source(ctx context.Context, run Run, worldDir string, log *slog.Logger) ([]byte, error) {
	cached := blueMarbleSourcePath(worldDir, blueMarble)
	if data, err := os.ReadFile(cached); err == nil {
		if err := verifyBlueMarble(data, blueMarble); err != nil {
			return nil, fmt.Errorf("cached source %s: %w", cached, err)
		}
		log.Info("source already captured", logging.Path(cached), "bytes", len(data))
		return data, nil
	}
	data, _, err := run.Fetch.Get(ctx, blueMarble.Asset, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", blueMarble.Asset, err)
	}
	if err := verifyBlueMarble(data, blueMarble); err != nil {
		return nil, err
	}
	if err := writeFile(cached, data); err != nil {
		return nil, err
	}
	log.Info("source captured", logging.Path(cached), "bytes", len(data))
	return data, nil
}

// blueMarbleSourcePath is where the downloaded source sits: beside the world's
// captures, content-addressed by the pinned digest.
func blueMarbleSourcePath(worldDir string, pin blueMarblePin) string {
	return filepath.Join(worldDir, "source", pin.SHA256+".jpg")
}

// verifyBlueMarble holds bytes to the pin. It runs before any decode: bytes
// that are not the pinned publication are not decoded, not derived from, and
// not archived.
func verifyBlueMarble(data []byte, pin blueMarblePin) error {
	if sum := Hash(data); sum != pin.SHA256 {
		return fmt.Errorf(
			"source digest mismatch: fetched bytes hash to %s where the pin requires %s; "+
				"refusing changed upstream bytes", sum, pin.SHA256)
	}
	return nil
}

// writeBlueMarble derives the capture from verified source bytes: the image
// decoded, held to its declared size, cut to the capture level through the
// deterministic resampler, tiled, and recorded -- and the capture body written
// last, carrying the product's identity and the policy the cut was made under.
func writeBlueMarble(store *Archive, worldDir string, worldID int64, pin blueMarblePin, source []byte, log *slog.Logger) error {
	decoded, kind, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return fmt.Errorf("decode source: %w", err)
	}
	bounds := decoded.Bounds()
	if kind != "jpeg" || bounds.Dx() != pin.Width || bounds.Dy() != pin.Height {
		return fmt.Errorf("source is a %d×%d %s where the pin requires a %d×%d jpeg",
			bounds.Dx(), bounds.Dy(), kind, pin.Width, pin.Height)
	}

	maxX, maxY := doc.EquirectLevelExtent(blueMarbleZoom)
	ground := resample(toNRGBA(decoded), (maxX+1)*doc.SyntheticTileSize, (maxY+1)*doc.SyntheticTileSize)

	index, err := store.OpenTileIndex(worldDir, worldID)
	if err != nil {
		return err
	}
	setID := index.SetID(nasabluemarble.TileSet)
	for y := 0; y <= maxY; y++ {
		for x := 0; x <= maxX; x++ {
			tile := ground.SubImage(image.Rect(
				x*doc.SyntheticTileSize, y*doc.SyntheticTileSize,
				(x+1)*doc.SyntheticTileSize, (y+1)*doc.SyntheticTileSize))
			var encoded bytes.Buffer
			if err := jpeg.Encode(&encoded, tile, &jpeg.Options{Quality: pin.Quality}); err != nil {
				return fmt.Errorf("encode tile %d/%d: %w", x, y, err)
			}
			data := encoded.Bytes()
			if err := WriteTile(TilePath(worldDir, setID, blueMarbleZoom, x, y, "jpg"), data); err != nil {
				return err
			}
			index.Put(TileRecord{
				ByteLength:  len(data),
				ContentHash: Hash(data),
				ContentType: "image/jpeg",
				CoverageKey: index.CoverageKey(setID, blueMarbleZoom, x, y),
				Status:      StatusCached,
				TileSetID:   setID,
				URL:         blueMarbleTileURL(blueMarbleZoom, x, y),
				X:           x,
				Y:           y,
				Zoom:        blueMarbleZoom,
			})
		}
	}
	if err := index.Close(worldDir); err != nil {
		return err
	}

	body, err := json.MarshalIndent(blueMarbleCapture{
		Source:      nasabluemarble.SourceName,
		Body:        nasabluemarble.Body,
		Product:     "Blue Marble: Next Generation w/ Topography and Bathymetry, July 2004",
		Credit:      "NASA Earth Observatory",
		AssetSHA256: pin.SHA256,
		Width:       pin.Width,
		Height:      pin.Height,
		Map: blueMarbleMosaic{
			MaxZoom:    blueMarbleZoom,
			Extension:  "jpg",
			LayerTitle: "Blue Marble",
		},
		Derive: blueMarbleDerivation{
			Resampler:   blueMarbleResampler,
			JPEGQuality: pin.Quality,
		},
	}, "", "  ")
	if err != nil {
		return err
	}
	hash, fresh, err := store.WriteCapture(worldDir, Capture{
		Kind:       nasabluemarble.CaptureKind,
		SourceID:   worldID,
		SourceURL:  pin.Asset,
		CapturedAt: pin.CapturedAt,
		Body:       body,
	})
	if err != nil {
		return err
	}
	log.Info("capture archived", logging.World(nasabluemarble.Body),
		logging.Stamp(hash[:12]), "fresh", fresh, "tiles", (maxX+1)*(maxY+1))
	return nil
}

// blueMarbleTileURL is the address a reference tile is recorded under. These
// tiles were derived here and never served anywhere, so the address is honest
// about that -- an .invalid host, like the rendered basemaps' -- while carrying
// the marker and pyramid path the archive's reader recovers the tile set from.
func blueMarbleTileURL(zoom, x, y int) string {
	return "https://blue-marble.invalid/tiles/" + nasabluemarble.TileSet +
		"/" + strconv.Itoa(zoom) + "/" + strconv.Itoa(x) + "/" + strconv.Itoa(y) + ".jpg"
}

// blueMarbleCapture is the capture body as this crawler writes it, mirrored by
// the reader's own declaration in sources/nasabluemarble.
type blueMarbleCapture struct {
	Source      string               `json:"source"`
	Body        string               `json:"body"`
	Product     string               `json:"product"`
	Credit      string               `json:"credit"`
	AssetSHA256 string               `json:"assetSha256"`
	Width       int                  `json:"width"`
	Height      int                  `json:"height"`
	Map         blueMarbleMosaic     `json:"map"`
	Derive      blueMarbleDerivation `json:"derive"`
}

type blueMarbleMosaic struct {
	MaxZoom    int    `json:"maxZoom"`
	Extension  string `json:"extension"`
	LayerTitle string `json:"layerTitle"`
}

type blueMarbleDerivation struct {
	Resampler   string `json:"resampler"`
	JPEGQuality int    `json:"jpegQuality"`
}

func blueMarbleArchiveID(name string) int64 {
	return int64(fnv32a(name)&0x7fffffff) | blueMarbleIDBit
}
