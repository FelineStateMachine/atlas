// Package canon writes the one JSON shape every golden fixture is committed
// in. A fixture is a comparison instrument, so two captures of the same
// content must produce the same bytes on any machine: keys sorted, numbers
// carried as the source wrote them, no HTML escaping, one trailing newline.
//
// Numbers travel through json.Number rather than float64 on purpose. A
// payload that says 0.30000001192092896 keeps saying it, and a payload that
// says 1e3 keeps saying that; re-formatting a number is a change to the
// fixture that no one asked for.
package canon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Bytes canonicalizes raw JSON. It is the form every payload extracted from a
// bundle is committed in.
func Bytes(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return Value(value)
}

// Value canonicalizes a Go value. Maps sort by key, which is what makes the
// output stable; structs keep their declared field order, which is why the
// fixture types below are structs rather than maps.
func Value(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Compact canonicalizes a value onto one line, for the rows of an inventory
// that is read a line at a time and diffed a line at a time.
func Compact(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// Rows writes a document whose one long array holds a row per line: a header
// object, then the named array, each element compact on its own line. A tile
// inventory of seventeen thousand entries is unreadable pretty-printed and
// undiffable compacted; this is the shape that survives both.
func Rows(header map[string]any, arrayKey string, rows []any) ([]byte, error) {
	head, err := Value(header)
	if err != nil {
		return nil, err
	}
	// Value wrote a complete object; reopen it to append the array.
	trimmed := bytes.TrimRight(head, "\n")
	if len(trimmed) < 2 || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("canon: header is not an object")
	}
	var out bytes.Buffer
	out.Write(bytes.TrimRight(trimmed[:len(trimmed)-1], " \n"))
	if len(header) > 0 {
		out.WriteString(",")
	}
	out.WriteString("\n  ")
	key, err := Compact(arrayKey)
	if err != nil {
		return nil, err
	}
	out.Write(key)
	out.WriteString(": [")
	for index, row := range rows {
		line, err := Compact(row)
		if err != nil {
			return nil, err
		}
		if index > 0 {
			out.WriteString(",")
		}
		out.WriteString("\n    ")
		out.Write(line)
	}
	if len(rows) > 0 {
		out.WriteString("\n  ")
	}
	out.WriteString("]\n}\n")
	return out.Bytes(), nil
}

// WriteFile writes a fixture, creating the directories above it.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(append([]byte{}, data...), '\n')
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteValue canonicalizes and writes in one step.
func WriteValue(path string, value any) error {
	data, err := Value(value)
	if err != nil {
		return err
	}
	return WriteFile(path, data)
}
