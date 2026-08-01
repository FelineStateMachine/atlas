package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fabricated report in the export's shape: BOM, attribute/value rows,
// district rules above, the queried parcel's own facts below.
const sampleReport = "\ufeff\"Attributes\",\"Value\"\n" +
	"\"zone code\",\"X-1\"\n" +
	"\"zone name\",\"Example District\"\n" +
	"\"zone guide\",\"A district for examples.\"\n" +
	"\"link\",\"https://example.com/code\"\n" +
	"\"building height control\",\"max_building_height_ft-25\"\n" +
	"\"assorted\",\"minimum_floor_area_sf_per_du_unit-850\"\n" +
	"\"prohibited\",\"Rock quarries, Foundries\"\n" +
	"\"as of right\",\"Parks, Libraries\"\n" +
	"\"short term rental permitted\",\"True\"\n" +
	"\"Parcel Identification\",\"APN: R1234567\"\n" +
	"\"Property Location (Situs / Geospatial)\",\"Address: 1 Example Way\"\n" +
	"\"Ownership Information\",\"Owner Full Name: SOME PERSON\"\n" +
	"\"Valuation (Assessed & Market)\",\"Assd Total Value: 12345\"\n"

func writeReport(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadZoneReportKeepsRulesAndDropsTheParcel(t *testing.T) {
	note, err := readZoneReport(writeReport(t, "x1.csv", sampleReport))
	if err != nil {
		t.Fatal(err)
	}
	if note.Code != "X-1" {
		t.Fatalf("code = %q", note.Code)
	}
	want := map[string]string{
		"zoning.zone_code":                 "X-1",
		"zoning.zone_name":                 "Example District",
		"zoning.zone_guide":                "A district for examples.",
		"controls.building_height_control": "max_building_height_ft-25",
		"controls.assorted":                "minimum_floor_area_sf_per_du_unit-850",
		"plu.prohibited":                   "Rock quarries, Foundries",
		"plu.as_of_right":                  "Parks, Libraries",
		"plu.short_term_rental_permitted":  "True",
	}
	if len(note.Fields) != len(want) {
		t.Fatalf("fields = %v", note.Fields)
	}
	for key, value := range want {
		if note.Fields[key] != value {
			t.Fatalf("fields[%q] = %q, want %q", key, note.Fields[key], value)
		}
	}
	// The parcel's facts -- and any owner's name -- must never survive.
	flat := strings.ToLower(spellFields(note.Fields))
	for _, leaked := range []string{"some person", "r1234567", "example way", "12345", "example.com"} {
		if strings.Contains(flat, leaked) {
			t.Fatalf("parcel fact %q leaked into the note", leaked)
		}
	}
}

func spellFields(fields map[string]string) string {
	var parts []string
	for key, value := range fields {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func TestLoadZoneomicsReportsRefusals(t *testing.T) {
	// A report without a zone code is refused.
	if _, err := readZoneReport(writeReport(t, "no-code.csv",
		"\"Attributes\",\"Value\"\n\"zone name\",\"Nameless\"\n")); err == nil {
		t.Fatal("a codeless report was accepted")
	}

	// Two reports naming the same code are refused together.
	dir := t.TempDir()
	for _, name := range []string{"a.csv", "b.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sampleReport), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadZoneomicsReports(dir); err == nil {
		t.Fatal("doubled zone codes were accepted")
	}

	// An empty directory is a mistake, not a quiet no-op.
	if _, err := loadZoneomicsReports(t.TempDir()); err == nil {
		t.Fatal("an empty directory was accepted")
	}
}

func TestLoadZoneomicsReportsReadsAWholeDirectory(t *testing.T) {
	dir := t.TempDir()
	second := strings.Replace(sampleReport, "X-1", "X-2", 1)
	if err := os.WriteFile(filepath.Join(dir, "x1.csv"), []byte(sampleReport), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x2.csv"), []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	notes, err := loadZoneomicsReports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].Code != "X-1" || notes[1].Code != "X-2" {
		t.Fatalf("notes = %+v", notes)
	}
}
