package semconv

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// This file is the hand-written half of the registry: the value checkers, and
// the reading and validating surface over them. The vocabulary itself --
// namespace, version, entities, stability tiers, key constants and the
// registry map -- is generated into registry_gen.go from spec/registry.yaml.
//
// The split is deliberate. A checker is code and stays code; the spec
// references it by name, so adding a checker is a Go edit and using one is a
// spec edit, and a spec naming a checker nobody wrote fails codegen rather
// than admitting a key that checks nothing.

// definition is one registered key: where it attaches, how settled it is, and
// what values it admits.
type definition struct {
	entity    Entity
	stability Stability
	check     func(value string) error
}

// Keys lists every registered key in a stable order, for the tools that
// report on adoption and for the tests that hold the registry to its prose.
func Keys() []string {
	out := make([]string, 0, len(registry))
	for key := range registry {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// EntityOf reports where a key attaches. The false result is a reader's cue
// to ignore the attribute: an unregistered key is data from a vocabulary this
// build does not speak, never a reason to refuse a bundle.
func EntityOf(key string) (Entity, bool) {
	definition, known := registry[key]
	return definition.entity, known
}

// StabilityOf reports how settled a key is.
func StabilityOf(key string) (Stability, bool) {
	definition, known := registry[key]
	return definition.stability, known
}

// Check holds one attribute to the registry: registered, attached to this
// entity, and carrying a value its vocabulary admits. It is the single-key
// face of [Validate], for a producer building an attribute set one key at a
// time.
func Check(entity Entity, key, value string) error {
	definition, known := registry[key]
	if !known {
		return fmt.Errorf("attribute %q is not registered", key)
	}
	if definition.entity != entity {
		return fmt.Errorf("attribute %q attaches to a %s, not a %s", key, definition.entity, entity)
	}
	if err := definition.check(value); err != nil {
		return fmt.Errorf("attribute %q: %w", key, err)
	}
	return nil
}

// Validate is the producer-strict gate: every key of attrs in the atlas
// namespace must pass [Check] against entity. Keys outside the namespace are
// not this registry's business and pass through.
//
// Attributes are visited in sorted order, so a set with several faults always
// reports the same one.
func Validate(entity Entity, attrs map[string]string) error {
	for _, key := range sortedKeys(attrs) {
		if !strings.HasPrefix(key, Namespace) {
			continue
		}
		if err := Check(entity, key, attrs[key]); err != nil {
			return err
		}
	}
	return nil
}

func enum(values ...string) func(string) error {
	return func(value string) error {
		if slices.Contains(values, value) {
			return nil
		}
		return fmt.Errorf("%q is not one of %s", value, strings.Join(values, "|"))
	}
}

func slug(value string) error {
	if value == "" {
		return fmt.Errorf("empty")
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("%q is not a slug", value)
	}
	return nil
}

func decimal(value string) error {
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return fmt.Errorf("%q is not a number", value)
	}
	return nil
}

func positiveDecimal(value string) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%q is not a positive number", value)
	}
	return nil
}

func huc12(value string) error {
	if len(value) != 12 {
		return fmt.Errorf("%q is not twelve digits", value)
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return fmt.Errorf("%q is not twelve digits", value)
		}
	}
	return nil
}

func setName(value string) error {
	set, name, found := strings.Cut(value, "/")
	if !found || set == "" || name == "" {
		return fmt.Errorf("%q is not set/name", value)
	}
	if err := slug(set); err != nil {
		return err
	}
	return slug(name)
}

func numbers(count int) func(string) error {
	return func(value string) error {
		parts := strings.Split(value, ",")
		if len(parts) != count {
			return fmt.Errorf("%q wants %d comma-separated numbers", value, count)
		}
		for _, part := range parts {
			if _, err := strconv.ParseFloat(part, 64); err != nil {
				return fmt.Errorf("%q is not a number", part)
			}
		}
		return nil
	}
}

func sortedKeys(attrs map[string]string) []string {
	out := make([]string, 0, len(attrs))
	for key := range attrs {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
