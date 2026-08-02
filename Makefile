# The golden harness and the guardrails (issue #5 §6, §9).
#
# These targets are the clean-room rewrite's enforcement surface; the existing
# build recipes are untouched and still live where they always did.

.PHONY: golden golden-all spec depcheck lint-lanes analysis-lane render-lane seam seam-watch static serve-static desktop

# The one entrypoint. Runs every gate of §6 in order; gates whose lane does not
# exist yet report SKIP with the milestone they wait on, so a green run doubles
# as a list of what is not yet proven.
golden:
	go run ./golden/harness

# Attempt every gate, including the ones that are not ready. Fails loudly.
# Useful when you have just landed a lane and want to see its gate go red.
golden-all:
	go run ./golden/harness -suites=all

# The semantic conventions, from their one machine-readable source: the Go
# registry, the TypeScript lanes' key constants, and the document a reader
# learns the vocabulary from. `go generate ./format/semconv` is the same run.
# `make golden`'s semconv-codegen gate checks that what is committed is what
# this would write.
spec:
	go run ./spec/gen

# The guardrails alone: the lane import matrix, the clean-room rule, hostenv
# purity, network confinement, semconv discipline.
depcheck:
	go run ./golden/depcheck

# The TypeScript half of the same boundaries, over both lanes.
lint-lanes:
	npx eslint --config eslint.config.mjs analysis render

# The analysis lane's own gate: the boundary rules, the type checker at its
# strictest, and the conformance suite over every registered system. `make
# golden` runs this as the `analysis-lane` suite; this target is the same run,
# reachable on its own.
analysis-lane:
	npm run --silent lane

# The seam's own gate: the same boundary rules, the type checker, the seam's
# unit tests against the golden fixtures, and the authored-line budget as a
# warning. `make golden` runs this as the `render-lane` suite.
render-lane:
	npm run --silent seam-lane

# The seam's built output: one JavaScript file, from one entry point.
seam:
	npm run --silent seam-build

# The same, rebuilt on every save. `atlas dev -seam-watch` runs this.
seam-watch:
	npm run --silent watch --workspace @atlas/render

# The tree a host mounts at /static: the seam's bundle as `app.js`, which is
# the one file the shell's script tag asks for. The stylesheet system is NOT
# here -- it is the application's own asset, served from /assets by the
# application itself, which is what makes deleting this lane cost a page one
# script tag rather than its chrome.
#
# It is installed in two places because two hosts want it in two shapes.
# dist/static is the build output a `-static` mount is pointed at, and is what
# the parity harness looks for by default (golden/parity/run.mjs). static/ is
# the tree the desktop shell embeds (`//go:embed static` in main.go), so that
# the shipped application is one file; see static/README.md. Same bytes, one
# build.
static: seam
	mkdir -p dist/static
	cp render/dist/app.js dist/static/app.js
	cp render/dist/app.js static/app.js

# The smoke run: the real application, the real registry, and the seam mounted
# where the page looks for it. Read-only against the library.
serve-static: static
	go run ./cmd/atlas serve -static dist/static

# The desktop shell (issue #5 §3.4): the application in a window, one file,
# seam embedded. This is the macOS recipe -- the same `go build` .github/
# workflows/release.yml runs, plus the bundle directory macOS wants before it
# will treat a binary as an application. Linux and Windows need no bundle and
# only the `go build` line, with the tags the release workflow spells.
#
# It is `go build`, not `wails build`: the Wails CLI scaffolds and drives a
# Vite frontend, and this tree serves its own pages. There is no wails.json
# for the same reason.
desktop: static
	go build -tags "desktop,production" -ldflags "-s -w" -o Atlas .
	mkdir -p Atlas.app/Contents/MacOS
	cp Atlas Atlas.app/Contents/MacOS/Atlas
	printf '%s\n' \
	  '<?xml version="1.0" encoding="UTF-8"?>' \
	  '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
	  '<plist version="1.0">' \
	  '<dict>' \
	  '  <key>CFBundleExecutable</key><string>Atlas</string>' \
	  '  <key>CFBundleIdentifier</key><string>dev.felinestatemachine.atlas</string>' \
	  '  <key>CFBundleName</key><string>Atlas</string>' \
	  '  <key>CFBundlePackageType</key><string>APPL</string>' \
	  '  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>' \
	  '  <key>LSMinimumSystemVersion</key><string>12.0</string>' \
	  '  <key>NSHighResolutionCapable</key><true/>' \
	  '</dict>' \
	  '</plist>' > Atlas.app/Contents/Info.plist
