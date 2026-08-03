# The test surface and the guardrails.
#
# These targets are the enforcement surface; the existing build recipes are
# untouched and still live where they always did.

.PHONY: test test-e2e corpus-smoke spec depcheck lint-lanes analysis-lane render-lane seam seam-watch static serve-static desktop

# The one entrypoint: every required gate, and nothing that can silently
# decline to judge. Go tests run through tools/testgate, which fails the run
# if any test skipped; the lanes run their own boundary rules, type checks and
# suites; depcheck holds the import matrix. What CI runs is this, spelled out
# step by step in .github/workflows/ci.yml because the Windows runner has no
# make.
test:
	go vet ./...
	go run ./tools/testgate ./...
	npm run --silent lane
	npm run --silent seam-lane
	go run ./tools/depcheck

# The application in a real browser, over a registry packed from the committed
# corpus. Needs a Playwright Chromium (npx playwright install chromium); every
# other prerequisite is built by the target itself.
test-e2e: static
	go run ./tests/e2e/prep
	npx playwright test --config tests/e2e/playwright.config.ts

# The maintainer's deep check, and deliberately not a CI gate: walk a real
# installed library and hold every current-format bundle to the reader's
# invariants. Compares no stamps, no hashes, no content.
corpus-smoke:
	go run ./tools/corpussmoke

# The semantic conventions, from their one machine-readable source: the Go
# registry, the TypeScript lanes' key constants, and the document a reader
# learns the vocabulary from. `go generate ./format/semconv` is the same run.
# spec/gen's own test checks that what is committed is what this would write.
spec:
	go run ./spec/gen

# The guardrails alone: the lane import matrix, the clean-room rule, hostenv
# purity, network confinement, semconv discipline.
depcheck:
	go run ./tools/depcheck

# The TypeScript half of the same boundaries, over both lanes.
lint-lanes:
	npx eslint --config eslint.config.mjs analysis render

# The analysis lane's own gate: the boundary rules, the type checker at its
# strictest, and the conformance suite over every registered system. `make
# test` runs the same thing; this target is the one lane on its own.
analysis-lane:
	npm run --silent lane

# The seam's own gate: the same boundary rules, the type checker, the seam's
# unit tests against the corpus and its stated models, and the authored-line
# budget as a warning. `make test` runs the same thing.
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
# the e2e run serves (tests/e2e/playwright.config.ts). static/ is
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
