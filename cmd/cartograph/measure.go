package main

// The workbench judges a build along the axes internal/measure defines --
// one yardstick shared with tools/maturity, aligned with the semantic
// conventions the payloads speak, so "how good is this build" means the
// same thing in every room. The workbench keeps the ledgers whole where the
// report prints counts: every matched pair, every held pin with its reason,
// so a page can lay two builds' accounts beside each other and show exactly
// what a policy change moved.

import "github.com/FelineStateMachine/atlas/internal/measure"

type build = measure.Build
type mergeAccount = measure.MergeAccount

func measureBundle(path string) (*build, error) { return measure.MeasureBundle(path) }

func loadPins(path string, mapSlugs []string) (map[string]map[int64]string, error) {
	return measure.LoadPins(path, mapSlugs)
}

func percent(part, whole int) string { return measure.Percent(part, whole) }
