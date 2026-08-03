package crawl

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// TestBlueMarblePinsTheSpecifiedPublication holds the shipped pin to the facts
// the included volume is specified against. A typo here is not a style problem:
// the digest decides which bytes are accepted, the capture time decides the
// bundle's name, and the credit is what NASA asks a republisher to keep.
func TestBlueMarblePinsTheSpecifiedPublication(t *testing.T) {
	if blueMarble.Asset != "https://assets.science.nasa.gov/content/dam/science/esd/eo/images/bmng/bmng-topography-bathymetry/july/world.topo.bathy.200407.3x21600x10800.jpg" {
		t.Errorf("asset URL %q is not the pinned publication", blueMarble.Asset)
	}
	if blueMarble.SHA256 != "d225f1f35a6448a4d1d8f6de6e48f3433e470085b70a35800e64f384f269a7b0" {
		t.Errorf("digest %q is not the pinned digest", blueMarble.SHA256)
	}
	if blueMarble.CapturedAt != "2026-08-03T16:21:07Z" {
		t.Errorf("capture time %q is not the recorded first capture", blueMarble.CapturedAt)
	}
	if blueMarble.Width != 21600 || blueMarble.Height != 10800 {
		t.Errorf("source size %d×%d is not the published image's", blueMarble.Width, blueMarble.Height)
	}
	if blueMarble.Borders.SHA256 != "6866c877d39cba9c357620878839b336d569f8c662d3cfab4cb1dbe2d39c977f" ||
		blueMarble.Places.SHA256 != "0dbd25c9ad8bd797ddf164b067f563be5c16be2c002254eb594862377963f9dc" {
		t.Error("the Natural Earth digests are not the pinned ones")
	}
	if !strings.Contains(blueMarble.Borders.Asset, "/v5.1.2/") ||
		!strings.Contains(blueMarble.Places.Asset, "/v5.1.2/") ||
		!strings.Contains(blueMarble.FeaturesEdition, "v5.1.2") {
		t.Error("the feature pins do not agree on the tagged edition")
	}
}

// TestBlueMarbleDistillsTheFeatureFiles: the distillation keeps exactly what
// the capture needs -- names, codes, continents, label points, rings
// normalized to multipolygons, and only the primary capitals -- with every
// coordinate verbatim.
func TestBlueMarbleDistillsTheFeatureFiles(t *testing.T) {
	borders := []byte(`{"features":[
		{"properties":{"NAME":"Vale","ADM0_A3":"VAL","CONTINENT":"Europe","LABEL_X":10.5,"LABEL_Y":50.25},
		 "geometry":{"type":"Polygon","coordinates":[[[5,45],[15,45],[15,55],[5,45]]]}},
		{"properties":{"NAME":"Mar","ADM0_A3":"MAR2","CONTINENT":"Oceania","LABEL_X":150,"LABEL_Y":-20},
		 "geometry":{"type":"MultiPolygon","coordinates":[[[[145,-25],[155,-25],[155,-15],[145,-25]]]]}}]}`)
	places := []byte(`{"features":[
		{"properties":{"featurecla":"Admin-0 capital","name":"Vale City","adm0name":"Vale","adm0_a3":"VAL","latitude":50.1,"longitude":10.2}},
		{"properties":{"featurecla":"Admin-0 capital alt","name":"Old Seat","adm0name":"Vale","adm0_a3":"VAL","latitude":49,"longitude":9}},
		{"properties":{"featurecla":"Populated place","name":"Just A Town","adm0name":"Vale","adm0_a3":"VAL","latitude":48,"longitude":8}}]}`)
	pin := blueMarblePin{FeaturesEdition: "stated",
		Borders: blueMarbleAsset{SHA256: "bb"}, Places: blueMarbleAsset{SHA256: "pp"}}
	out, err := distillBlueMarbleFeatures(pin, borders, places)
	if err != nil {
		t.Fatal(err)
	}
	if out.Edition != "stated" || out.BordersSHA256 != "bb" || out.PlacesSHA256 != "pp" {
		t.Errorf("the distillation does not carry its provenance: %+v", out)
	}
	if len(out.Countries) != 2 {
		t.Fatalf("%d countries", len(out.Countries))
	}
	vale := out.Countries[0]
	if vale.Name != "Vale" || vale.A3 != "VAL" || vale.Continent != "Europe" ||
		vale.LabelLon != 10.5 || vale.LabelLat != 50.25 {
		t.Errorf("country did not travel verbatim: %+v", vale)
	}
	if len(vale.Polygons) != 1 || len(vale.Polygons[0]) != 1 || vale.Polygons[0][0][0] != [2]float64{5, 45} {
		t.Errorf("a Polygon did not normalize to one multipolygon part: %+v", vale.Polygons)
	}
	if len(out.Countries[1].Polygons) != 1 {
		t.Errorf("a MultiPolygon did not pass through: %+v", out.Countries[1].Polygons)
	}
	if len(out.Capitals) != 1 || out.Capitals[0].Name != "Vale City" {
		t.Fatalf("the distillation kept %+v; only primary capitals ride", out.Capitals)
	}
	if out.Capitals[0].Lat != 50.1 || out.Capitals[0].Lon != 10.2 {
		t.Errorf("a capital's coordinates did not travel verbatim: %+v", out.Capitals[0])
	}

	if _, err := distillBlueMarbleFeatures(pin, []byte(`{"features":[
		{"properties":{"NAME":"Line","ADM0_A3":"LIN","CONTINENT":"Europe"},
		 "geometry":{"type":"LineString","coordinates":[[0,0],[1,1]]}}]}`), places); err == nil {
		t.Error("a line among the borders was distilled anyway")
	}
}

// TestBlueMarbleRefusesAWrongDigest is the gate the whole capture stands
// behind: bytes that do not hash to the pin are refused by name, and bytes that
// do pass.
func TestBlueMarbleRefusesAWrongDigest(t *testing.T) {
	err := verifyBlueMarble([]byte("not the publication"), blueMarble.SHA256)
	if err == nil {
		t.Fatal("changed upstream bytes were accepted")
	}
	if !strings.Contains(err.Error(), blueMarble.SHA256) || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("the refusal does not name the pin: %v", err)
	}

	stated := []byte("a stated source")
	if err := verifyBlueMarble(stated, Hash(stated)); err != nil {
		t.Errorf("the pinned bytes themselves were refused: %v", err)
	}
}

// statedSource paints a small stated source image -- a horizontal gradient
// crossed by bands, so neighbouring tiles differ -- and encodes it the way the
// pin expects.
func statedSource(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(255 * x / width),
				G: uint8(255 * y / height),
				B: uint8((x / 8) % 256),
				A: 255,
			})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// statedFeatures is the smallest sound vector half: two countries on two
// continents, a capital in one of them, and a microstate capital no border
// draws.
func statedFeatures() blueMarbleFeatures {
	return blueMarbleFeatures{
		Edition:       "stated",
		BordersSHA256: "bb", PlacesSHA256: "pp",
		Countries: []blueMarbleCountry{
			{Name: "Vale", A3: "VAL", Continent: "Europe", LabelLon: 10, LabelLat: 50,
				Polygons: [][][][2]float64{{{{5, 45}, {15, 45}, {15, 55}, {5, 55}, {5, 45}}}}},
			{Name: "Mar", A3: "MAR2", Continent: "Oceania", LabelLon: 150, LabelLat: -20,
				Polygons: [][][][2]float64{{{{145, -25}, {155, -25}, {155, -15}, {145, -15}, {145, -25}}}}},
		},
		Capitals: []blueMarbleCapital{
			{Name: "Vale City", Country: "Vale", A3: "VAL", Lat: 50, Lon: 10},
			{Name: "Atoll", Country: "Atoll", A3: "ATL", Lat: -18, Lon: 152},
		},
	}
}

// writeStated runs the capture's write path over a stated pin into a fresh
// archive, and answers the world directory it wrote.
func writeStated(t *testing.T, pin blueMarblePin, source []byte) (root, worldDir string) {
	t.Helper()
	root = t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	volumeDir, err := store.RegisterVolume(Volume{ID: 42, Title: "Earth", Source: "nasa-blue-marble"})
	if err != nil {
		t.Fatal(err)
	}
	worldDir, err = store.RegisterWorld(
		Volume{ID: 42, Title: "Earth", Source: "nasa-blue-marble"}, volumeDir,
		World{ID: 43, Slug: "earth", Title: "Earth"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBlueMarble(store, worldDir, 43, pin, source, statedFeatures(), slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return root, worldDir
}

// TestBlueMarbleCutsTheWholeSphereWindow: the write path cuts exactly the
// reference level the deriver expects -- every tile of the half-height window
// at the reference zoom, cached, recorded under the tile set the reader names,
// and readable back through the archive.
func TestBlueMarbleCutsTheWholeSphereWindow(t *testing.T) {
	source := statedSource(t, 432, 216)
	pin := blueMarblePin{
		SHA256: Hash(source), CapturedAt: "2026-08-03T14:30:39Z",
		Width: 432, Height: 216, Quality: 90,
	}
	root, _ := writeStated(t, pin, source)

	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	worlds, err := store.Worlds(store.Volumes()[0])
	if err != nil {
		t.Fatal(err)
	}
	captured, err := store.Tiles(worlds[0])
	if err != nil {
		t.Fatal(err)
	}
	levels, held := captured["earth/blue-marble"]
	if !held {
		t.Fatalf("no tiles recorded under the reader's tile set; recorded sets: %v", keysOf(captured))
	}
	maxX, maxY := doc.EquirectLevelExtent(blueMarbleZoom)
	if len(levels[blueMarbleZoom]) != (maxX+1)*(maxY+1) {
		t.Fatalf("%d tiles at the reference level, want the whole %d×%d window",
			len(levels[blueMarbleZoom]), maxX+1, maxY+1)
	}
	for _, ref := range levels[blueMarbleZoom] {
		path, format, err := store.Raster(worlds[0], ref)
		if err != nil || format != "jpg" {
			t.Fatalf("tile %d/%d is not readable as jpg: %v", ref.X, ref.Y, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if Hash(data) != ref.ContentHash {
			t.Fatalf("tile %d/%d does not carry the hash its record claims", ref.X, ref.Y)
		}
	}

	captures, err := store.Captures(worlds[0])
	if err != nil || len(captures) != 1 {
		t.Fatalf("%d captures recorded: %v", len(captures), err)
	}
	if captures[0].Kind != "blue-marble-map" || captures[0].CapturedAt != pin.CapturedAt {
		t.Errorf("capture records %s at %s; the pin decides both",
			captures[0].Kind, captures[0].CapturedAt)
	}
}

// TestBlueMarbleWritesTheSameCaptureTwice is the determinism the included
// volume stands on: the same pinned source, cut on two runs, is the same
// archive -- every tile byte for byte, and the capture body under the same
// content hash.
func TestBlueMarbleWritesTheSameCaptureTwice(t *testing.T) {
	source := statedSource(t, 432, 216)
	pin := blueMarblePin{
		SHA256: Hash(source), CapturedAt: "2026-08-03T14:30:39Z",
		Width: 432, Height: 216, Quality: 90,
	}
	firstRoot, firstWorld := writeStated(t, pin, source)
	secondRoot, secondWorld := writeStated(t, pin, source)

	first, err := os.ReadFile(filepath.Join(firstWorld, "tiles", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(secondWorld, "tiles", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("two runs over one pin recorded different tile indexes")
	}
	var records []TileRecord
	if err := json.Unmarshal(first, &records); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		a, err := os.ReadFile(filepath.Join(firstRoot, tileRelPath(firstWorld, firstRoot, record)))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(secondRoot, tileRelPath(secondWorld, secondRoot, record)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("tile %d/%d differs between two runs", record.X, record.Y)
		}
	}

	firstIndex, err := os.ReadFile(filepath.Join(firstWorld, "snapshots", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondIndex, err := os.ReadFile(filepath.Join(secondWorld, "snapshots", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstIndex, secondIndex) {
		t.Fatal("two runs over one pin recorded different captures")
	}
}

// TestResamplePreservesFlatGround: the frozen weights of every tap window sum
// exactly to one, so a flat picture resamples to exactly itself -- the property
// that keeps the cut from shifting the whole planet's tone.
func TestResamplePreservesFlatGround(t *testing.T) {
	flat := image.NewNRGBA(image.Rect(0, 0, 270, 135))
	for i := range flat.Pix {
		flat.Pix[i] = 137
	}
	out := resample(flat, 128, 64)
	for i, value := range out.Pix {
		if value != 137 {
			t.Fatalf("pixel byte %d resampled 137 to %d", i, value)
		}
	}
}

func tileRelPath(worldDir, root string, record TileRecord) string {
	rel, _ := filepath.Rel(root, TilePath(worldDir, record.TileSetID, record.Zoom, record.X, record.Y, "jpg"))
	return rel
}

func keysOf(m map[string]map[int][]archive.TileRef) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
