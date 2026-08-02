// Command bundles extracts the golden bundle fixtures: for each volume in the
// fixture set, the manifest and world payloads canonicalized, the packed
// ATLASLOC locations unpacked to JSON, the icons hashed, and a per-lens tile
// inventory naming every tile with the hash of its bytes and the digest of
// its decoded pixels.
//
// The pixel digest is the point of the inventory. A tile re-encoded by a
// later pipeline -- a different JPEG library, a different PNG compression
// level -- has different bytes and the same picture, and only the digest of
// the decoded pixels can tell that apart from a tile that actually changed.
//
//	go run ./golden/capture/bundles -out golden/fixtures tunic mars
//
// It reads the installed registry exactly as the application does, so the
// bundle it captures for a slug is the bundle the application would serve.
// It writes nothing outside -out.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/FelineStateMachine/atlas/golden/capture/canon"
	"github.com/FelineStateMachine/atlas/internal/bundle"

	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

func main() {
	dir := flag.String("dir", "", "registry directory (default: the application's own library)")
	out := flag.String("out", "golden/fixtures", "fixtures directory to write into")
	private := flag.String("private", "", "comma-separated slugs whose fixtures go under <out>/private")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "bundles: name at least one volume slug")
		os.Exit(2)
	}

	root := *dir
	if root == "" {
		resolved, err := bundle.DefaultDir()
		if err != nil {
			fail(err)
		}
		root = resolved
	}
	registry := bundle.NewRegistry(root)
	if err := registry.Rescan(); err != nil {
		fail(err)
	}
	winners := registry.Snapshot().Volumes

	held := map[string]bool{}
	for _, slug := range strings.Split(*private, ",") {
		if slug = strings.TrimSpace(slug); slug != "" {
			held[slug] = true
		}
	}

	for _, slug := range flag.Args() {
		winner, ok := winners[slug]
		if !ok {
			fail(fmt.Errorf("%s is not installed in %s", slug, root))
		}
		base := filepath.Join(*out, "bundles", slug)
		if held[slug] {
			base = filepath.Join(*out, "private", "bundles", slug)
		}
		if err := capture(winner.Path, winner.Manifest, base); err != nil {
			fail(fmt.Errorf("%s: %w", slug, err))
		}
		fmt.Printf("bundles: %s -> %s\n", slug, base)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bundles:", err)
	os.Exit(1)
}

// volumeFixture is the fixture's own header: which file was captured, which
// build of which volume it is, and the hash of every part the rest of the
// fixture is derived from. A capture that disagrees with this file disagrees
// about the input, not about the pipeline.
type volumeFixture struct {
	Slug          string            `json:"slug"`
	Title         string            `json:"title"`
	File          string            `json:"file"`
	FileBytes     int64             `json:"fileBytes"`
	FileSHA256    string            `json:"fileSha256"`
	FormatVersion int               `json:"formatVersion"`
	Conventions   int               `json:"conventions"`
	Stamp         string            `json:"stamp"`
	Stamp12       string            `json:"stamp12"`
	CreatedAt     string            `json:"createdAt"`
	Revision      int               `json:"revision"`
	Traits        []string          `json:"traits"`
	TileGrid      bundle.TileGrid   `json:"tileGrid"`
	Counts        counts            `json:"counts"`
	Worlds        []worldFixture    `json:"worlds"`
	Pyramids      []pyramidFixture  `json:"pyramids"`
	PartHashes    map[string]string `json:"partHashes"`
	EntryOrder    entryOrder        `json:"entryOrder"`
}

type counts struct {
	Entries int `json:"entries"`
	Worlds  int `json:"worlds"`
	Tiles   int `json:"tiles"`
	Icons   int `json:"icons"`
	Points  int `json:"points"`
	Paths   int `json:"paths"`
	Areas   int `json:"areas"`
}

type worldFixture struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Parent      string   `json:"parent,omitempty"`
	Points      int      `json:"points"`
	Paths       int      `json:"paths"`
	Areas       int      `json:"areas"`
	Collections int      `json:"collections"`
	Lenses      []string `json:"lenses"`
	Shards      []int64  `json:"shards,omitempty"`
	MergedFrom  []string `json:"mergedFrom,omitempty"`
	TextEntries int      `json:"textEntries"`
}

type pyramidFixture struct {
	Pyramid   string   `json:"pyramid"`
	Worlds    []string `json:"worlds"`
	Lenses    []string `json:"lenses"`
	Tiles     int      `json:"tiles"`
	Bytes     int64    `json:"bytes"`
	Content   string   `json:"contentRollup"`
	Pixels    string   `json:"pixelRollup"`
	Inventory string   `json:"inventory"`
}

// entryOrder pins the order the writer laid the archive out in without
// committing seventeen thousand file names: the first entries as they read,
// and one hash over the whole ordered list.
type entryOrder struct {
	First  []string `json:"first"`
	SHA256 string   `json:"sha256"`
}

// worldPeek is the sliver of a world payload this capture reads for its
// header. The payload itself is committed whole and canonical; this is only
// what the header summarizes.
type worldPeek struct {
	Lenses []struct {
		Name  string `json:"name"`
		Tiles string `json:"tiles"`
		Shard int64  `json:"shard,omitempty"`
	} `json:"lenses"`
	Collections []json.RawMessage `json:"collections"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Merged      []struct {
		Source string `json:"source"`
		Origin bool   `json:"origin,omitempty"`
	} `json:"merged,omitempty"`
}

func capture(bundlePath string, manifest bundle.Manifest, out string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	archive, err := zip.OpenReader(bundlePath)
	if err != nil {
		return err
	}
	defer archive.Close()

	entries := make(map[string]*zip.File, len(archive.File))
	names := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		entries[file.Name] = file
		names = append(names, file.Name)
	}

	info, err := os.Stat(bundlePath)
	if err != nil {
		return err
	}
	fileHash, err := hashFile(bundlePath)
	if err != nil {
		return err
	}

	fixture := volumeFixture{
		Slug:          manifest.Volume.Slug,
		Title:         manifest.Volume.Title,
		File:          filepath.Base(bundlePath),
		FileBytes:     info.Size(),
		FileSHA256:    fileHash,
		FormatVersion: manifest.FormatVersion,
		Conventions:   manifest.Conventions,
		Stamp:         manifest.Version.Stamp,
		Stamp12:       bundle.ShortStamp(manifest.Version.Stamp),
		CreatedAt:     manifest.Version.CreatedAt,
		Revision:      manifest.Version.Revision,
		TileGrid:      manifest.TileGrid,
		PartHashes:    map[string]string{},
		EntryOrder:    orderOf(names),
	}
	fixture.Counts.Entries = len(names)
	fixture.Counts.Worlds = len(manifest.Worlds)

	raw, err := readEntry(entries, bundle.ManifestName)
	if err != nil {
		return err
	}
	fixture.PartHashes[bundle.ManifestName] = bundle.HashBytes(raw)
	if err := writeCanon(filepath.Join(out, "manifest.json"), raw); err != nil {
		return err
	}

	// Which lens of which world draws from which pyramid, so the tile
	// inventories below can say what they are an inventory of.
	lensesByPyramid := map[string][]string{}
	worldsByPyramid := map[string][]string{}

	var sharded, merged, split, sphere bool
	for _, entry := range manifest.Worlds {
		world := worldFixture{
			Slug:   entry.Slug,
			Title:  entry.Title,
			Parent: entry.Parent,
			Points: entry.Points,
			Paths:  entry.Paths,
			Areas:  entry.Areas,
		}
		fixture.Counts.Points += entry.Points
		fixture.Counts.Paths += entry.Paths
		fixture.Counts.Areas += entry.Areas
		if entry.Parent != "" {
			split = true
		}

		payloadName := "worlds/" + entry.Slug + ".json"
		payload, err := readEntry(entries, payloadName)
		if err != nil {
			return err
		}
		fixture.PartHashes[payloadName] = bundle.HashBytes(payload)
		if err := writeCanon(filepath.Join(out, "worlds", entry.Slug+".payload.json"), payload); err != nil {
			return err
		}
		var peek worldPeek
		if err := json.Unmarshal(payload, &peek); err != nil {
			return fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		world.Collections = len(peek.Collections)
		for _, lens := range peek.Lenses {
			world.Lenses = append(world.Lenses, lens.Name+"="+lens.Tiles)
			lensesByPyramid[lens.Tiles] = append(lensesByPyramid[lens.Tiles], entry.Slug+"/"+lens.Name)
			worldsByPyramid[lens.Tiles] = appendOnce(worldsByPyramid[lens.Tiles], entry.Slug)
			if lens.Shard != 0 {
				world.Shards = append(world.Shards, lens.Shard)
				sharded = true
			}
		}
		for _, source := range peek.Merged {
			if source.Origin {
				world.MergedFrom = append(world.MergedFrom, source.Source+" (origin)")
				continue
			}
			world.MergedFrom = append(world.MergedFrom, source.Source)
			merged = true
		}
		if peek.Attrs["atlas.geometry.surface"] == "sphere" {
			sphere = true
		}

		textName := "worlds/" + entry.Slug + ".text"
		text, err := readEntry(entries, textName)
		if err != nil {
			return err
		}
		fixture.PartHashes[textName] = bundle.HashBytes(text)
		if err := writeCanon(filepath.Join(out, "worlds", entry.Slug+".text.json"), text); err != nil {
			return err
		}
		var textEntries map[string]json.RawMessage
		if err := json.Unmarshal(text, &textEntries); err != nil {
			return fmt.Errorf("world %s text: %w", entry.Slug, err)
		}
		world.TextEntries = len(textEntries)

		packedName := "worlds/" + entry.Slug + ".bin"
		packed, err := readEntry(entries, packedName)
		if err != nil {
			return err
		}
		fixture.PartHashes[packedName] = bundle.HashBytes(packed)
		if err := writeLocations(filepath.Join(out, "worlds", entry.Slug+".locations.json"),
			entry.Slug, packed); err != nil {
			return err
		}

		fixture.Worlds = append(fixture.Worlds, world)
	}

	if err := writeIcons(entries, names, filepath.Join(out, "icons.json"), &fixture); err != nil {
		return err
	}

	pyramids := map[string][]*zip.File{}
	for _, name := range names {
		if !strings.HasPrefix(name, "tiles/") {
			continue
		}
		rest := strings.TrimPrefix(name, "tiles/")
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			continue
		}
		pyramids[rest[:slash]] = append(pyramids[rest[:slash]], entries[name])
	}
	pyramidNames := make([]string, 0, len(pyramids))
	for name := range pyramids {
		pyramidNames = append(pyramidNames, name)
	}
	sort.Strings(pyramidNames)
	for _, name := range pyramidNames {
		summary, err := writeTiles(name, pyramids[name], out,
			worldsByPyramid[name], lensesByPyramid[name])
		if err != nil {
			return err
		}
		fixture.Counts.Tiles += summary.Tiles
		fixture.Pyramids = append(fixture.Pyramids, summary)
	}

	if sharded {
		fixture.Traits = append(fixture.Traits, "lens-sharded")
	}
	if merged {
		fixture.Traits = append(fixture.Traits, "multi-source-merge")
	}
	if split {
		fixture.Traits = append(fixture.Traits, "split-sheet")
	}
	if sphere {
		fixture.Traits = append(fixture.Traits, "sphere")
	}
	if len(fixture.Traits) == 0 {
		fixture.Traits = append(fixture.Traits, "plain")
	}
	return canon.WriteValue(filepath.Join(out, "volume.json"), fixture)
}

func appendOnce(list []string, value string) []string {
	for _, held := range list {
		if held == value {
			return list
		}
	}
	return append(list, value)
}

func orderOf(names []string) entryOrder {
	digest := sha256.New()
	for _, name := range names {
		digest.Write([]byte(name))
		digest.Write([]byte{'\n'})
	}
	first := names
	if len(first) > 12 {
		first = first[:12]
	}
	return entryOrder{First: append([]string{}, first...), SHA256: hex.EncodeToString(digest.Sum(nil))}
}

func readEntry(entries map[string]*zip.File, name string) ([]byte, error) {
	file, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("archive holds no %s", name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func writeCanon(path string, raw []byte) error {
	data, err := canon.Bytes(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return canon.WriteFile(path, data)
}

// locationRow is one packed location as the fixture spells it, in the order
// the packed payload's parallel arrays carry the columns.
type locationRow struct {
	ID     int64   `json:"id"`
	Owner  uint16  `json:"owner"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Member int64   `json:"member"`
	Shard  int64   `json:"shard"`
	Title  string  `json:"title"`
}

func writeLocations(path, world string, packed []byte) error {
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		return fmt.Errorf("world %s: %w", world, err)
	}
	rows := make([]any, 0, len(locations))
	for _, held := range locations {
		rows = append(rows, locationRow{
			ID:     held.ID,
			Owner:  held.Owner,
			Lat:    held.Lat,
			Lng:    held.Lng,
			Member: held.Member,
			Shard:  held.Shard,
			Title:  held.Title,
		})
	}
	header := map[string]any{
		"world":        world,
		"packed":       "ATLASLOC v3",
		"packedSha256": bundle.HashBytes(packed),
		"packedBytes":  len(packed),
		"count":        len(locations),
		"note": "the packed payload's own order, which is the order the viewer " +
			"reads it in; owner indexes the payload's collections array",
	}
	data, err := canon.Rows(header, "locations", rows)
	if err != nil {
		return err
	}
	return canon.WriteFile(path, data)
}

type iconRow struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func writeIcons(entries map[string]*zip.File, names []string, path string, fixture *volumeFixture) error {
	var rows []any
	digest := sha256.New()
	for _, name := range names {
		if !strings.HasPrefix(name, "icons/") {
			continue
		}
		data, err := readEntry(entries, name)
		if err != nil {
			return err
		}
		hash := bundle.HashBytes(data)
		rows = append(rows, iconRow{Name: name, Bytes: int64(len(data)), SHA256: hash})
		digest.Write([]byte(name + " " + hash + "\n"))
		fixture.Counts.Icons++
	}
	header := map[string]any{
		"volume": fixture.Slug,
		"count":  fixture.Counts.Icons,
		"rollup": hex.EncodeToString(digest.Sum(nil)),
	}
	data, err := canon.Rows(header, "icons", rows)
	if err != nil {
		return err
	}
	return canon.WriteFile(path, data)
}

// tileRow is one tile: what it weighs, what its bytes hash to, and what its
// decoded picture hashes to. Name is relative to the pyramid, so the row
// reads as the z/x/y it is.
type tileRow struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	CRC32  string `json:"crc32"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"w"`
	Height int    `json:"h"`
	Pixels string `json:"px"`
}

func writeTiles(pyramid string, files []*zip.File, out string, worlds, lenses []string) (pyramidFixture, error) {
	summary := pyramidFixture{
		Pyramid:   pyramid,
		Worlds:    worlds,
		Lenses:    lenses,
		Tiles:     len(files),
		Inventory: path.Join("tiles", inventoryName(pyramid)+".json"),
	}
	sort.Strings(summary.Worlds)
	sort.Strings(summary.Lenses)

	prefix := "tiles/" + pyramid + "/"
	sort.Slice(files, func(a, b int) bool {
		return lessTile(strings.TrimPrefix(files[a].Name, prefix), strings.TrimPrefix(files[b].Name, prefix))
	})

	rows := make([]any, len(files))
	errs := make([]error, len(files))
	var group sync.WaitGroup
	work := make(chan int)
	workers := runtime.NumCPU()
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range work {
				row, err := measureTile(files[index], prefix)
				rows[index], errs[index] = row, err
			}
		}()
	}
	for index := range files {
		work <- index
	}
	close(work)
	group.Wait()
	for _, err := range errs {
		if err != nil {
			return summary, err
		}
	}

	content := sha256.New()
	pixels := sha256.New()
	for _, row := range rows {
		tile := row.(tileRow)
		summary.Bytes += tile.Bytes
		content.Write([]byte(tile.Name + " " + tile.SHA256 + "\n"))
		pixels.Write([]byte(tile.Name + " " + tile.Pixels + "\n"))
	}
	summary.Content = hex.EncodeToString(content.Sum(nil))
	summary.Pixels = hex.EncodeToString(pixels.Sum(nil))

	header := map[string]any{
		"pyramid":       pyramid,
		"worlds":        summary.Worlds,
		"lenses":        summary.Lenses,
		"count":         summary.Tiles,
		"bytes":         summary.Bytes,
		"contentRollup": summary.Content,
		"pixelRollup":   summary.Pixels,
		"order":         "z, then x, then y, numerically",
		"px": "sha256 over \"<width>x<height>\\n\" and the decoded image as " +
			"non-alpha-premultiplied RGBA, so a re-encode of the same picture digests equal",
	}
	data, err := canon.Rows(header, "tiles", rows)
	if err != nil {
		return summary, err
	}
	return summary, canon.WriteFile(filepath.Join(out, summary.Inventory), data)
}

// inventoryName keeps a pyramid's inventory to one path segment: pyramid
// names carry slashes on a volume whose lenses are aligned resamples of
// another's, and a file name may not.
func inventoryName(pyramid string) string {
	return strings.ReplaceAll(pyramid, "/", "__")
}

func measureTile(file *zip.File, prefix string) (tileRow, error) {
	reader, err := file.Open()
	if err != nil {
		return tileRow{}, err
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return tileRow{}, err
	}
	row := tileRow{
		Name:   strings.TrimPrefix(file.Name, prefix),
		Bytes:  int64(len(data)),
		CRC32:  fmt.Sprintf("%08x", file.CRC32),
		SHA256: bundle.HashBytes(data),
	}
	width, height, digest, err := pixelDigest(data)
	if err != nil {
		return row, fmt.Errorf("%s: %w", file.Name, err)
	}
	row.Width, row.Height, row.Pixels = width, height, digest
	return row, nil
}

// pixelDigest decodes a tile and hashes the picture rather than the file.
// Every format lands in one representation -- unpremultiplied RGBA at the
// origin -- so a JPEG and a PNG of the same picture digest alike, and so
// does the same picture re-encoded by a different library.
func pixelDigest(data []byte) (int, int, string, error) {
	decoded, _, err := image.Decode(strings.NewReader(string(data)))
	if err != nil {
		return 0, 0, "", err
	}
	bounds := decoded.Bounds()
	flat := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(flat, flat.Bounds(), decoded, bounds.Min, draw.Src)
	digest := sha256.New()
	fmt.Fprintf(digest, "%dx%d\n", bounds.Dx(), bounds.Dy())
	digest.Write(flat.Pix)
	return bounds.Dx(), bounds.Dy(), hex.EncodeToString(digest.Sum(nil)), nil
}

// lessTile orders z/x/y.ext by its numbers, so level 10 follows level 9
// rather than level 1.
func lessTile(a, b string) bool {
	left, right := numbersIn(a), numbersIn(b)
	for index := range min(len(left), len(right)) {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	return a < b
}

func numbersIn(name string) []int {
	fields := strings.FieldsFunc(name, func(r rune) bool { return r < '0' || r > '9' })
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return out
		}
		out = append(out, value)
	}
	return out
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
