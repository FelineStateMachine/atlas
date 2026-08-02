// Package crawl fetches what publishers serve and writes it into the capture
// archive. It is the one package in Atlas permitted to reach the network, and
// `depcheck`'s netconfine rule is what makes that true rather than customary.
//
// # Why the boundary is here
//
// A bundle serves offline, forever. That invariant is kept at two ends: a
// payload carries no runtime URL, and only one package can produce one. Every
// other part of the pipeline is a pure function of bytes already on disk, which
// is what lets an editorial change replay over years of captures instead of
// re-crawling them — and what lets a translator be tested without a network,
// a fixture, or a mock.
//
// Fetching is crawling, wherever it happens. The enrich lane's national
// hydrography evidence is captured here too, and travels in the archive, so the
// join re-runs against the archive rather than against a live endpoint.
//
// # What a crawl promises
//
//   - **Unchanged bytes record nothing.** A capture is deduplicated by content
//     hash alone. A re-crawl that fetched the same thing leaves the archive byte
//     for byte as it was: the payload file is rewritten to the same path with
//     the same bytes, and the index is not touched at all. `capturedAt` is
//     first-seen, never last-verified, so nothing downstream moves and a rebuild
//     computes the same stamp.
//   - **Politeness is global.** One schedule spaces every request of a run,
//     whatever its concurrency and whatever host it is aimed at. A 429 or a 5xx
//     pushes the whole schedule back, not just the worker that saw it.
//   - **Absence is a result.** A tile origin answers 403 or 404 for an object it
//     never published, and that is an ordinary outcome recorded as such, so a
//     re-crawl does not ask again.
//   - **Nothing is renamed.** A volume or world the archive already holds a
//     directory for keeps it forever, found by identity rather than by name.
//
// # The archive this writes
//
// The layout is the historical one, kept verbatim because years of captured
// history are data rather than code. `internal/generate/archive` reads it and
// docs/generate.md §3 is normative for it. What this package adds is the write
// path: where a capture's bytes go, how the registers are updated in place, and
// what is recorded about a tile that was fetched, one that was not published,
// and one that failed.
package crawl

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Hash is the archive's content address: SHA-256 of the bytes, lowercase hex.
// It addresses a capture body, and it is what a tile record carries so a
// derivation stamp can be taken without reading a raster.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// The statuses a tile record carries. Only Cached means the bytes are on disk.
const (
	// StatusCached: the bytes are here, under the path the record names.
	StatusCached = "cached"
	// StatusAbsent: the origin says it never published this tile. A pyramid is
	// a rectangle and its corners are often empty, so this is the ordinary
	// answer at the edges and it is recorded so a re-crawl does not ask again.
	StatusAbsent = "absent"
	// StatusFailed: something went wrong that is not absence. The error is
	// recorded beside it, and a later run will try again.
	StatusFailed = "failed"
)

// DirectoryName is how a volume or a world's directory is named on first sight:
// its title, folded to something a filesystem is comfortable with, with its
// identity appended so two publishers describing the same game never collide.
//
// It is only ever consulted on first sight. A directory the archive already
// holds is found by identity and kept forever, whatever it was called, because
// renaming one would orphan every capture and every tile inside it.
func DirectoryName(title string, id int64) string {
	out := make([]rune, 0, len(title)+8)
	for _, r := range strings.ToLower(title) {
		if folded, known := foldedLatin[r]; known {
			r = folded
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		default:
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out) + "-" + strconv.FormatInt(id, 10)
}

// foldedLatin is the diacritics the corpus actually contains. It is a table
// rather than a normalization library because the answer has to be stable
// forever: a directory named once is named for good, and a Unicode table that
// improved would rename somebody's archive.
var foldedLatin = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ñ': 'n', 'ç': 'c', 'ß': 's', 'ý': 'y',
}
