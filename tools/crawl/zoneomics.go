// The Zoneomics leg of an ArcGIS crawl reads zone reports the user exported
// from their own Zoneomics account: one CSV per zoning district, each an
// attribute/value table of the district's identity, its permitted and
// prohibited uses, and its dimensional controls. The reports join the
// captured zoning zones by zone code and ride the same content-addressed
// capture as the boundaries they explain.
//
// Files are the only door used. The API prices single-point reports and the
// public code pages bar automated access, so neither fits correlating a
// whole town; a subscriber exports each district's report once and drops
// the files beside the archive, and a snapshot needs no key and no wire.
//
// A report is a *point* report, and below the district's rules it carries
// the queried parcel's own facts -- owner name, address, valuation. Those
// rows are dropped unconditionally: a person's name and home value have no
// place in a distributed bundle, and no curation may re-admit them.
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/arcgismap"
)

// loadZoneomicsReports reads every .csv under path (or path itself when it
// is one file) into one ZoneNote per district. Any unreadable file, any
// report without a zone code, and any two reports naming the same code are
// the caller's failure: a capture must never silently lose or double its
// enrichment.
func loadZoneomicsReports(path string) ([]arcgismap.ZoneNote, error) {
	files, err := zoneomicsFiles(path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no zone report CSVs under %s", path)
	}
	seen := make(map[string]string, len(files))
	notes := make([]arcgismap.ZoneNote, 0, len(files))
	for _, file := range files {
		note, err := readZoneReport(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		if prior, doubled := seen[note.Code]; doubled {
			return nil, fmt.Errorf("%s and %s both report zone %s",
				prior, filepath.Base(file), note.Code)
		}
		seen[note.Code] = filepath.Base(file)
		notes = append(notes, note)
	}
	return notes, nil
}

func zoneomicsFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".csv") {
			files = append(files, filepath.Join(path, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// readZoneReport reads one exported report into a note: attribute rows
// mapped into the capture's sectioned field names, parcel rows dropped.
func readZoneReport(path string) (arcgismap.ZoneNote, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return arcgismap.ZoneNote{}, err
	}
	// Exports lead with a byte-order mark.
	text := strings.TrimPrefix(string(raw), "\ufeff")
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return arcgismap.ZoneNote{}, err
	}

	note := arcgismap.ZoneNote{Fields: make(arcgismap.Fields)}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		attribute := strings.TrimSpace(row[0])
		value := strings.TrimSpace(row[1])
		if attribute == "" || attribute == "Attributes" || value == "" {
			continue
		}
		if attribute == "zone code" {
			note.Code = value
		}
		name, kept := zoneReportField(attribute)
		if !kept {
			continue
		}
		note.Fields[name] = value
	}
	if note.Code == "" {
		return arcgismap.ZoneNote{}, fmt.Errorf("report names no zone code")
	}
	if len(note.Fields) == 0 {
		return arcgismap.ZoneNote{}, fmt.Errorf("report for zone %s keeps no fields", note.Code)
	}
	return note, nil
}

// parcelReportRow marks the rows of a point report that describe the queried
// parcel rather than the district: identity, ownership, valuation. They are
// dropped before the capture exists, unconditionally -- this list is a
// floor, not a curation surface.
func parcelReportRow(attribute string) bool {
	lowered := strings.ToLower(attribute)
	for _, banned := range []string{
		"parcel identification", "property location", "ownership", "owner",
		"valuation", "lot / land characteristics", "land use",
	} {
		if strings.Contains(lowered, banned) {
			return true
		}
	}
	return false
}

// zoneReportField maps one report attribute onto the capture's sectioned
// spelling: the district's identity under zoning., its use rules under
// plu., its dimensional standards under controls. Rows that are links or
// parcel facts are dropped; anything else unrecognized keeps its own name,
// unsectioned, so a new report vocabulary loses nothing.
func zoneReportField(attribute string) (string, bool) {
	if parcelReportRow(attribute) {
		return "", false
	}
	lowered := strings.ToLower(attribute)
	if droppedZoneomicsKey(lowered) {
		return "", false
	}
	slug := strings.ReplaceAll(lowered, " ", "_")
	switch {
	case strings.HasPrefix(lowered, "zone "):
		return "zoning." + slug, true
	case lowered == "prohibited" || lowered == "as of right" ||
		strings.Contains(lowered, "use") || strings.Contains(lowered, "permitted") ||
		strings.HasPrefix(lowered, "short term rental") || lowered == "definitions section":
		return "plu." + slug, true
	case strings.HasSuffix(lowered, "control") || lowered == "assorted" ||
		strings.Contains(attribute, ">"):
		return "controls." + slug, true
	}
	return slug, true
}

// droppedZoneomicsKey names the report parts a capture leaves behind:
// links and identifiers, matched by whole token so "guide" and
// "residential" survive "guid" and "id".
func droppedZoneomicsKey(key string) bool {
	banned := map[string]bool{
		"url": true, "link": true, "links": true, "href": true, "guid": true,
		"id": true, "ids": true,
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if banned[token] {
			return true
		}
	}
	return false
}
