// Command http records the application's data plane as it is served today:
// the catalog, a sampled response per volume for each kind of entry a bundle
// holds, the answers the router gives to requests it refuses, and the headers
// on all of them.
//
//	go run ./golden/capture/http -base http://127.0.0.1:PORT -out golden/fixtures
//
// The server is expected to be already running -- golden/capture/capture.sh
// starts the headless build against the fixture registry and stops it again.
// What is sampled is derived from what is served: the catalog names the
// volumes, each volume's first world payload names its lenses and its icons,
// so the transcript follows the same road a client does and needs no table of
// paths kept in step by hand.
//
// Two values are machine-specific and are replaced where they appear: the
// bundles directory the catalog reports, and the Date header. Both are
// recorded as placeholders, and nothing else is touched.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/golden/capture/canon"
	"github.com/FelineStateMachine/atlas/internal/bundle"
)

const (
	bundlesDirPlaceholder = "<bundles-dir>"
	datePlaceholder       = "<date>"
)

// exchange is one recorded request and its answer. The body is pinned by
// hash and by length always -- after normalization, so the figures do not
// move with the length of a directory name -- and quoted only when it is
// small enough to read.
type exchange struct {
	Note           string            `json:"note"`
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	RequestHeaders map[string]string `json:"requestHeaders,omitempty"`
	Status         int               `json:"status"`
	Headers        map[string]string `json:"headers"`
	BodyBytes      int               `json:"bodyBytes"`
	BodySHA256     string            `json:"bodySha256"`
	BodyFile       string            `json:"bodyFile,omitempty"`
	BodyText       string            `json:"bodyText,omitempty"`
}

type catalogDoc struct {
	Volumes []struct {
		Slug   string `json:"slug"`
		Title  string `json:"title"`
		Stamp  string `json:"stamp"`
		Base   string `json:"base"`
		Worlds []struct {
			Slug string `json:"slug"`
		} `json:"worlds"`
	} `json:"volumes"`
	BundlesDir string `json:"bundlesDir"`
}

type payloadDoc struct {
	Lenses []struct {
		Name    string   `json:"name"`
		Tiles   string   `json:"tiles"`
		MinZoom int      `json:"minZoom"`
		Formats []string `json:"formats"`
	} `json:"lenses"`
	Collections []struct {
		IconAsset string `json:"iconAsset"`
	} `json:"collections"`
}

func main() {
	base := flag.String("base", "", "address the headless application is serving on")
	out := flag.String("out", "golden/fixtures", "fixtures directory to write into")
	flag.Parse()
	if *base == "" {
		fmt.Fprintln(os.Stderr, "http: -base is required")
		os.Exit(2)
	}
	if err := record(strings.TrimSuffix(*base, "/"), filepath.Join(*out, "http")); err != nil {
		fmt.Fprintln(os.Stderr, "http:", err)
		os.Exit(1)
	}
}

type recorder struct {
	base       string
	out        string
	bundlesDir string
	exchanges  []any
}

func record(base, out string) error {
	r := &recorder{base: base, out: out}

	// One unrecorded fetch first, only to learn the directory this machine
	// keeps its library in. Every body is normalized against it before it is
	// weighed or hashed, so the transcript reads the same whatever the
	// capture's working directory was called.
	if err := r.learnBundlesDir(); err != nil {
		return err
	}

	catalogStatus, catalogBody, err := r.get("the catalog, composed at scan time and never cached",
		"/data/catalog.json", nil, "catalog.json")
	if err != nil {
		return err
	}
	if catalogStatus != http.StatusOK {
		return fmt.Errorf("catalog answered %d", catalogStatus)
	}
	var catalog catalogDoc
	if err := json.Unmarshal(catalogBody, &catalog); err != nil {
		return err
	}
	if err := r.writeBody("catalog.json", catalogBody); err != nil {
		return err
	}

	for _, volume := range catalog.Volumes {
		if len(volume.Worlds) == 0 {
			continue
		}
		world := volume.Worlds[0].Slug
		_, payload, err := r.get("a world payload, the first thing a volume is opened by",
			volume.Base+"/worlds/"+world+".json", nil, "")
		if err != nil {
			return err
		}
		if _, _, err := r.get("the packed point locations beside it",
			volume.Base+"/worlds/"+world+".bin", nil, ""); err != nil {
			return err
		}
		if _, _, err := r.get("the deferred prose, fetched when a feature is opened",
			volume.Base+"/worlds/"+world+".text", nil, ""); err != nil {
			return err
		}

		var read payloadDoc
		if err := json.Unmarshal(payload, &read); err != nil {
			return fmt.Errorf("%s payload: %w", volume.Slug, err)
		}
		for _, collection := range read.Collections {
			if collection.IconAsset == "" {
				continue
			}
			if _, _, err := r.get("a category icon",
				volume.Base+"/icons/"+collection.IconAsset, nil, ""); err != nil {
				return err
			}
			break
		}
		if len(read.Lenses) == 0 {
			continue
		}
		lens := read.Lenses[0]
		extension := "jpg"
		if len(lens.Formats) > 0 {
			extension = lens.Formats[0]
		}
		tile := fmt.Sprintf("%s/tiles/%s/%d/0/0.%s", volume.Base, lens.Tiles, lens.MinZoom, extension)
		if _, _, err := r.get("a tile at the shallowest level of the first lens", tile, nil, ""); err != nil {
			return err
		}
		if volume.Slug == catalog.Volumes[0].Slug {
			// Tiles are stored uncompressed so that a range could serve them.
			// The router does not offer ranges: it sets a length and copies.
			// The fixture records that, so the rewrite either keeps the
			// behavior or changes it deliberately.
			if _, _, err := r.get("the same tile asked for by range",
				tile, map[string]string{"Range": "bytes=0-99"}, ""); err != nil {
				return err
			}
		}
	}

	if len(catalog.Volumes) > 0 {
		volume := catalog.Volumes[0]
		world := volume.Worlds[0].Slug
		stamp := bundle.ShortStamp(volume.Stamp)
		refusals := []struct {
			note string
			path string
		}{
			{"a stamp that is not the serving build: gone, and the client refetches the catalog",
				"/data/v/" + volume.Slug + "/000000000000/worlds/" + world + ".json"},
			{"a volume that is not installed",
				"/data/v/not-a-volume/" + stamp + "/worlds/" + world + ".json"},
			{"an entry outside worlds, tiles, and icons",
				volume.Base + "/atlas.json"},
			{"an extension the data plane does not name a type for",
				volume.Base + "/worlds/" + world + ".txt"},
			{"a world the bundle does not hold",
				volume.Base + "/worlds/not-a-world.json"},
			{"a path with no extension at all",
				volume.Base + "/worlds/" + world},
			{"a climb out of the volume, which the router's own path cleaning answers first",
				volume.Base + "/worlds/../../../../etc/passwd"},
			{"a path under the shell that is not the shell",
				"/not-a-page"},
		}
		for _, refusal := range refusals {
			if _, _, err := r.get(refusal.note, refusal.path, nil, ""); err != nil {
				return err
			}
		}
	}

	if _, _, err := r.get("the application shell", "/", nil, ""); err != nil {
		return err
	}
	for _, asset := range []string{"/static/app.css", "/static/app.js"} {
		if _, _, err := r.get("a shell asset, which the headless host answers for itself",
			asset, nil, ""); err != nil {
			return err
		}
	}

	header := map[string]any{
		"captured": "the headless application (ATLAS_HEADLESS=1) serving the fixture registry",
		"note": "Date is replaced by " + datePlaceholder + " and the catalog's own bundlesDir by " +
			bundlesDirPlaceholder + "; bodyBytes and bodySha256 are of the body after " +
			"that replacement, and nothing else is normalized",
		"exchanges": len(r.exchanges),
	}
	data, err := canon.Rows(header, "transcript", r.exchanges)
	if err != nil {
		return err
	}
	return canon.WriteFile(filepath.Join(out, "transcript.json"), data)
}

func (r *recorder) learnBundlesDir() error {
	response, err := http.Get(r.base + "/data/catalog.json")
	if err != nil {
		return err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return err
	}
	var catalog catalogDoc
	if err := json.Unmarshal(body, &catalog); err != nil {
		return err
	}
	r.bundlesDir = catalog.BundlesDir
	return nil
}

func (r *recorder) get(note, path string, headers map[string]string, bodyFile string) (int, []byte, error) {
	request, err := http.NewRequest(http.MethodGet, r.base+path, nil)
	if err != nil {
		return 0, nil, err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return 0, nil, err
	}

	scrubbed := r.scrub(body)
	recorded := exchange{
		Note:           note,
		Method:         http.MethodGet,
		Path:           path,
		RequestHeaders: headers,
		Status:         response.StatusCode,
		Headers:        map[string]string{},
		BodyBytes:      len(scrubbed),
		BodySHA256:     bundle.HashBytes(scrubbed),
		BodyFile:       bodyFile,
	}
	names := make([]string, 0, len(response.Header))
	for name := range response.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := strings.Join(response.Header.Values(name), ", ")
		if name == "Date" {
			value = datePlaceholder
		}
		recorded.Headers[name] = value
	}
	// Small text answers -- the refusals, mostly -- read better quoted than
	// hashed, and are short enough that quoting them costs nothing.
	if len(scrubbed) <= 512 && isText(response.Header.Get("Content-Type")) {
		recorded.BodyText = string(scrubbed)
	}
	r.exchanges = append(r.exchanges, recorded)
	return response.StatusCode, body, nil
}

func (r *recorder) writeBody(name string, body []byte) error {
	return canon.WriteFile(filepath.Join(r.out, name), r.scrub(body))
}

// scrub replaces the one machine-specific value the served bodies carry.
func (r *recorder) scrub(body []byte) []byte {
	if r.bundlesDir == "" {
		return body
	}
	return bytes.ReplaceAll(body, []byte(r.bundlesDir), []byte(bundlesDirPlaceholder))
}

func isText(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "application/json")
}
