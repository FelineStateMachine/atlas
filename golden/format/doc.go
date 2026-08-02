// Package format is the format-roundtrip gate of issue #5 §6: the committed
// bundle fixtures of golden/fixtures, read through format/bundle, held to
// what the golden-reference tree recorded.
//
// The gate runs in two modes, and the split is the whole design.
//
// Always-on, with no library present, it stands on the committed extractions
// alone: a canonicalized manifest parses into [bundle.Manifest] and re-encodes
// to the same canonical bytes, the identity derived from it names the file the
// fixture set says it names, the unpacked locations pack back to the byte
// count and digest the extraction recorded, and the extractions reassemble
// into an archive that [bundle.Reader.Validate] accepts. That is what CI runs,
// and it is a real gate: every byte it compares was written by the
// implementation this one replaces.
//
// Registry mode, with ATLAS_REGISTRY_DIR pointing at a bundles directory,
// opens the real .atlas of each fixture build and holds this package to the
// bytes on disk: the manifest re-encodes byte for byte, every part hashes to
// what the fixture recorded, every payload canonicalizes to its committed
// extraction, the icon and tile inventories match name for name, tiles are
// stored uncompressed, and the whole bundle validates.
//
// What the gate does not assert is stamp identity. The stamp sums a hash per
// named part, and one class of part -- a tile pyramid's derivation stamp --
// is not recoverable from a finished bundle. golden/format/STAMPS.md records
// the aspiration per fixture and what the enforceable proxies are.
//
// This package holds no code but its tests. It is named for the gate it runs,
// which is the file golden/harness/main.go names as the suite's entrypoint.
package format
