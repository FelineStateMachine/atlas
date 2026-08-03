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
	if blueMarble.Asset != "https://assets.science.nasa.gov/content/dam/science/esd/eo/images/bmng/bmng-base/july/world.200407.3x21600x10800.jpg" {
		t.Errorf("asset URL %q is not the pinned publication", blueMarble.Asset)
	}
	if blueMarble.SHA256 != "dea8b4dc8a4f93f5f8bce0c8c85a508a178e7901e9ed8e6bf86e6ce7ef6d61e2" {
		t.Errorf("digest %q is not the pinned digest", blueMarble.SHA256)
	}
	if blueMarble.CapturedAt != "2026-08-03T14:30:39Z" {
		t.Errorf("capture time %q is not the recorded first capture", blueMarble.CapturedAt)
	}
	if blueMarble.Width != 21600 || blueMarble.Height != 10800 {
		t.Errorf("source size %d×%d is not the published image's", blueMarble.Width, blueMarble.Height)
	}
}

// TestBlueMarbleRefusesAWrongDigest is the gate the whole capture stands
// behind: bytes that do not hash to the pin are refused by name, and bytes that
// do pass.
func TestBlueMarbleRefusesAWrongDigest(t *testing.T) {
	err := verifyBlueMarble([]byte("not the publication"), blueMarble)
	if err == nil {
		t.Fatal("changed upstream bytes were accepted")
	}
	if !strings.Contains(err.Error(), blueMarble.SHA256) || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("the refusal does not name the pin: %v", err)
	}

	stated := []byte("a stated source")
	pin := blueMarblePin{SHA256: Hash(stated)}
	if err := verifyBlueMarble(stated, pin); err != nil {
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
	if err := writeBlueMarble(store, worldDir, 43, pin, source, slog.New(slog.DiscardHandler)); err != nil {
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
