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
	// FeaturesEdition names the vector publication the capture marries to the
	// raster, and Borders and Places pin its two files: country polygons and
	// populated places, each held to its own digest exactly as the image is.
	FeaturesEdition string
	Borders, Places blueMarbleAsset
}

// blueMarbleAsset is one pinned file: where it is, and what its bytes must
// hash to.
type blueMarbleAsset struct {
	Asset  string
	SHA256 string
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
	CapturedAt: "2026-08-03T16:21:07Z",
	Width:      21600,
	Height:     10800,
	Quality:    85,
	// The feature data is Natural Earth, public domain, at a tagged release:
	// country polygons at 1:110m -- the light cut, made for whole-planet maps
	// -- and the populated places its capitals are read from. Boundaries and
	// capital designations are carried as Natural Earth draws them, de facto.
	FeaturesEdition: "Natural Earth 1:110m cultural vectors v5.1.2",
	Borders: blueMarbleAsset{
		Asset:  "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_110m_admin_0_countries.geojson",
		SHA256: "6866c877d39cba9c357620878839b336d569f8c662d3cfab4cb1dbe2d39c977f",
	},
	Places: blueMarbleAsset{
		Asset:  "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_110m_populated_places_simple.geojson",
		SHA256: "0dbd25c9ad8bd797ddf164b067f563be5c16be2c002254eb594862377963f9dc",
	},
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
	source, err := c.asset(ctx, run, worldDir, blueMarble.Asset, blueMarble.SHA256, ".jpg", log)
	if err != nil {
		return err
	}
	borders, err := c.asset(ctx, run, worldDir, blueMarble.Borders.Asset, blueMarble.Borders.SHA256, ".geojson", log)
	if err != nil {
		return err
	}
	places, err := c.asset(ctx, run, worldDir, blueMarble.Places.Asset, blueMarble.Places.SHA256, ".geojson", log)
	if err != nil {
		return err
	}
	features, err := distillBlueMarbleFeatures(blueMarble, borders, places)
	if err != nil {
		return err
	}
	if err := writeBlueMarble(run.Archive, worldDir, world.ID, blueMarble, source, features, log); err != nil {
		return err
	}
	return nil
}

// asset answers one pinned file's bytes: the copy already kept beside the
// capture when there is one, the origin exactly once when there is not. Either
// way the bytes are held to the pinned digest before anything is derived from
// them.
func (c blueMarbleCrawler) asset(
	ctx context.Context, run Run, worldDir, url, sha256, extension string, log *slog.Logger,
) ([]byte, error) {
	cached := filepath.Join(worldDir, "source", sha256+extension)
	if data, err := os.ReadFile(cached); err == nil {
		if err := verifyBlueMarble(data, sha256); err != nil {
			return nil, fmt.Errorf("cached source %s: %w", cached, err)
		}
		log.Info("source already captured", logging.Path(cached), "bytes", len(data))
		return data, nil
	}
	data, _, err := run.Fetch.Get(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if err := verifyBlueMarble(data, sha256); err != nil {
		return nil, err
	}
	if err := writeFile(cached, data); err != nil {
		return nil, err
	}
	log.Info("source captured", logging.Path(cached), "bytes", len(data))
	return data, nil
}

// verifyBlueMarble holds bytes to a pinned digest. It runs before any decode:
// bytes that are not the pinned publication are not decoded, not derived from,
// and not archived.
func verifyBlueMarble(data []byte, sha256 string) error {
	if sum := Hash(data); sum != sha256 {
		return fmt.Errorf(
			"source digest mismatch: fetched bytes hash to %s where the pin requires %s; "+
				"refusing changed upstream bytes", sum, sha256)
	}
	return nil
}

// distillBlueMarbleFeatures reads the two verified Natural Earth files down to
// what the capture keeps: each country's name, code, continent, label point
// and rings, and each primary capital's name, country and place. Coordinates
// travel verbatim, as published; putting them on the picture is the reader's
// work. Order is the publication's own.
func distillBlueMarbleFeatures(pin blueMarblePin, borders, places []byte) (blueMarbleFeatures, error) {
	out := blueMarbleFeatures{
		Edition:       pin.FeaturesEdition,
		BordersSHA256: pin.Borders.SHA256,
		PlacesSHA256:  pin.Places.SHA256,
	}

	var countries struct {
		Features []struct {
			Properties struct {
				Name      string  `json:"NAME"`
				A3        string  `json:"ADM0_A3"`
				Continent string  `json:"CONTINENT"`
				LabelLon  float64 `json:"LABEL_X"`
				LabelLat  float64 `json:"LABEL_Y"`
			} `json:"properties"`
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(borders, &countries); err != nil {
		return out, fmt.Errorf("decode country borders: %w", err)
	}
	for _, feature := range countries.Features {
		country := blueMarbleCountry{
			Name:      feature.Properties.Name,
			A3:        feature.Properties.A3,
			Continent: feature.Properties.Continent,
			LabelLon:  feature.Properties.LabelLon,
			LabelLat:  feature.Properties.LabelLat,
		}
		switch feature.Geometry.Type {
		case "Polygon":
			var rings [][][2]float64
			if err := json.Unmarshal(feature.Geometry.Coordinates, &rings); err != nil {
				return out, fmt.Errorf("country %q: %w", country.Name, err)
			}
			country.Polygons = [][][][2]float64{rings}
		case "MultiPolygon":
			if err := json.Unmarshal(feature.Geometry.Coordinates, &country.Polygons); err != nil {
				return out, fmt.Errorf("country %q: %w", country.Name, err)
			}
		default:
			return out, fmt.Errorf("country %q draws a %s; borders are ground",
				country.Name, feature.Geometry.Type)
		}
		out.Countries = append(out.Countries, country)
	}

	var placed struct {
		Features []struct {
			Properties struct {
				Class   string  `json:"featurecla"`
				Name    string  `json:"name"`
				Country string  `json:"adm0name"`
				A3      string  `json:"adm0_a3"`
				Lat     float64 `json:"latitude"`
				Lon     float64 `json:"longitude"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(places, &placed); err != nil {
		return out, fmt.Errorf("decode populated places: %w", err)
	}
	for _, feature := range placed.Features {
		// Only the primary designations: the alternates are second seats and
		// former capitals, which is more editorial weight than a demo carries.
		if feature.Properties.Class != "Admin-0 capital" {
			continue
		}
		out.Capitals = append(out.Capitals, blueMarbleCapital{
			Name:    feature.Properties.Name,
			Country: feature.Properties.Country,
			A3:      feature.Properties.A3,
			Lat:     feature.Properties.Lat,
			Lon:     feature.Properties.Lon,
		})
	}
	return out, nil
}

// writeBlueMarble derives the capture from verified source bytes: the image
// decoded, held to its declared size, cut to the capture level through the
// deterministic resampler, tiled, and recorded -- and the capture body written
// last, carrying the product's identity and the policy the cut was made under.
func writeBlueMarble(
	store *Archive, worldDir string, worldID int64,
	pin blueMarblePin, source []byte, features blueMarbleFeatures, log *slog.Logger,
) error {
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
		Features: features,
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
	Features    blueMarbleFeatures   `json:"features"`
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

// blueMarbleFeatures is the vector half of the capture: the publication it was
// distilled from, the digests its files carried, and the distillation itself.
type blueMarbleFeatures struct {
	Edition       string              `json:"edition"`
	BordersSHA256 string              `json:"bordersSha256"`
	PlacesSHA256  string              `json:"placesSha256"`
	Countries     []blueMarbleCountry `json:"countries"`
	Capitals      []blueMarbleCapital `json:"capitals"`
}

// blueMarbleCountry is one country as the capture keeps it: identity, the
// continent the publication files it under, its label point, and its rings --
// polygons, then rings, then positions, longitude first, all verbatim.
type blueMarbleCountry struct {
	Name      string           `json:"name"`
	A3        string           `json:"a3"`
	Continent string           `json:"continent"`
	LabelLon  float64          `json:"labelLon"`
	LabelLat  float64          `json:"labelLat"`
	Polygons  [][][][2]float64 `json:"polygons"`
}

// blueMarbleCapital is one primary capital as the capture keeps it.
type blueMarbleCapital struct {
	Name    string  `json:"name"`
	Country string  `json:"country"`
	A3      string  `json:"a3"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

func blueMarbleArchiveID(name string) int64 {
	return int64(fnv32a(name)&0x7fffffff) | blueMarbleIDBit
}
