# The golden harness and the guardrails (issue #5 §6, §9).
#
# These targets are the clean-room rewrite's enforcement surface; the existing
# build recipes are untouched and still live where they always did.

.PHONY: golden golden-all depcheck lint-lanes analysis-lane

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

# The TypeScript half of the same boundaries. Needs the workspace install at
# the repository root (`npm ci`). `render` is linted once it exists; the
# wildcard is what keeps this target honest before the seam lands.
lint-lanes:
	npx eslint --config eslint.config.mjs analysis $(wildcard render)

# The analysis lane's own gate: the boundary rules, the type checker at its
# strictest, and the conformance suite over every registered system. `make
# golden` runs this as the `analysis-lane` suite; this target is the same run,
# reachable on its own.
analysis-lane:
	npm run --silent lane
