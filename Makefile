# The golden harness and the guardrails (issue #5 §6, §9).
#
# These targets are the clean-room rewrite's enforcement surface; the existing
# build recipes are untouched and still live where they always did.

.PHONY: golden golden-all depcheck lint-lanes analysis-lane render-lane seam seam-watch static serve-static

# The one entrypoint. Runs every gate of §6 in order; gates whose lane does not
# exist yet report SKIP with the milestone they wait on, so a green run doubles
# as a list of what is not yet proven.
golden:
	go run ./golden/harness

# Attempt every gate, including the ones that are not ready. Fails loudly.
# Useful when you have just landed a lane and want to see its gate go red.
golden-all:
	go run ./golden/harness -suites=all

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

# The tree an `atlas serve -static` mount wants: the seam's bundle as
# `app.js`, which is the one file the shell's script tag asks for. The
# stylesheet system is NOT here -- it is the application's own asset, served
# from /assets by the application itself, which is what makes deleting this
# lane cost a page one script tag rather than its chrome.
static: seam
	mkdir -p dist/static
	cp render/dist/app.js dist/static/app.js

# The smoke run: the real application, the real registry, and the seam mounted
# where the page looks for it. Read-only against the library.
serve-static: static
	go run ./cmd/atlas serve -static dist/static
