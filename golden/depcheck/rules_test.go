package main

import (
	"strings"
	"testing"
)

const mod = modulePath + "/"

// The rules are pure functions of two package paths, which is what lets the
// whole import matrix of issue #5 §3.2 be read as a table.
func TestLaneImportEdge(t *testing.T) {
	tests := []struct {
		name    string
		from    string // module-relative path of the importing package
		imports string // full import path
		want    string // substring of the expected message; "" means allowed
	}{
		{"format may use the stdlib", "format/bundle", "archive/zip", ""},
		{"format may use itself", "format/bundle", mod + "format/semconv", ""},
		{"format may not use third parties", "format/bundle", "github.com/fsnotify/fsnotify", "standard library only"},
		{"format may not use a lane", "format/bundle", mod + "internal/generate/doc", "standard library only"},

		{"generate may use format", "internal/generate/compose", mod + "format/bundle", ""},
		{"generate may use itself", "internal/generate/compose", mod + "internal/generate/doc", ""},
		{"generate may use third parties", "internal/generate/tiles", "golang.org/x/image/draw", ""},
		{"generate may not use enrich", "internal/generate/compose", mod + "internal/enrich/merge", "generate must not import enrich"},
		{"enrich may not use generate", "internal/enrich/merge", mod + "internal/generate/doc", "enrich must not import generate"},
		{"generate may not use app", "internal/generate/doc", mod + "internal/app/session", "generate must not import app"},
		{"enrich may not use the harness", "internal/enrich/maturity", mod + "golden/depcheck", "enrich must not import golden"},

		{"app may use format", "internal/app/handler", mod + "format/semconv", ""},
		{"app may use itself", "internal/app/handler", mod + "internal/app/hostenv", ""},
		{"app may not use generate", "internal/app/handler", mod + "internal/generate/compose", "app must not import generate"},
		{"app may not use enrich", "internal/app/handler", mod + "internal/enrich/merge", "app must not import enrich"},

		{"workbench may use enrich/maturity", "internal/workbench", mod + "internal/enrich/maturity", ""},
		{"workbench may not use other enrichers", "internal/workbench", mod + "internal/enrich/merge", "only enrich/maturity"},
		{"workbench may not use generate", "internal/workbench", mod + "internal/generate/tiles", "workbench must not import generate"},

		{"nothing imports render", "internal/app/handler", mod + "render/scene", "nothing imports render"},
		{"nothing imports render, not even the CLI", "cmd/atlas", mod + "render/scene", "nothing imports render"},
		{"analysis is TypeScript", "internal/app/handler", mod + "analysis/cellsystems", "not importable from Go"},

		{"every lane may narrate itself", "internal/app/hostenv/oshost", mod + "internal/logging", ""},
		{"including the CLI that sets the stream up", "cmd/atlas", mod + "internal/logging", ""},
		{"format narrates nothing", "format/bundle", mod + "internal/logging", "standard library only"},
		{"logging depends on nothing of ours", "internal/logging", mod + "internal/app/hostenv", "logging must not import app"},
		{"every lane may speak the event stream", "internal/generate/compose", mod + "internal/logging", ""},
		{"logging may use the center", "internal/logging", mod + "format/bundle", ""},
		{"logging may not use a lane", "internal/logging", mod + "internal/generate/doc", "logging must not import generate"},

		{"the CLI wires every lane", "cmd/atlas", mod + "internal/enrich/merge", ""},
		{"the harness may read a lane", "golden/depcheck", mod + "internal/generate/doc", ""},
		{"a path the clean room never heard of is not judged", "internal/measure", mod + "internal/bundle", ""},

		{"the shell mounts the app", "", mod + "internal/app", ""},
		{"and the host it mounts it over", "", mod + "internal/app/hostenv/wailshost", ""},
		{"and narrates itself", "", mod + "internal/logging", ""},
		{"but does not import the seam either", "", mod + "render/scene", "nothing imports render"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := laneImportEdge(laneOf(tt.from), tt.from, tt.imports)
			assertMessage(t, got, tt.want)
		})
	}
}

// The old tree left for the golden-reference tag at close-out, so no import of
// it can be written today by accident. The rule stays, and so does its table:
// what it forbids is any module-local package the lane matrix has never heard
// of, and the archived names below are the clearest examples anyone will
// recognise. A path that is merely misspelled is caught by the same line.
func TestCleanRoomEdge(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		imports string
		want    string
	}{
		{"a lane may not reach the old tree", "internal/generate/doc", mod + "internal/mgdoc", "golden-reference tree"},
		{"nor the desktop shell's helpers", "internal/app/handler", mod + "internal/icons", "golden-reference tree"},
		{"the harness may read whatever it measures", "golden/pipeline", mod + "internal/bundle", ""},
		{"format has a stricter rule of its own", "format/bundle", mod + "internal/bundle", ""},
		{"a lane package is fine", "internal/generate/doc", mod + "format/bundle", ""},
		{"the shared event stream is clean room, not old tree", "internal/app/hostenv/oshost", mod + "internal/logging", ""},
		{"the stdlib is fine", "internal/generate/doc", "archive/zip", ""},
		{"a package outside the clean room is not judged", "internal/measure", mod + "internal/bundle", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanRoomEdge(laneOf(tt.from), tt.from, tt.imports)
			assertMessage(t, got, tt.want)
		})
	}
}

func TestHostenvEdge(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		imports string
		want    string
	}{
		{"the handler may not read the environment", "internal/app/handler", "os", "must not import \"os\""},
		{"nor shell out", "internal/app/session", "os/exec", "must not import \"os/exec\""},
		{"nor build paths", "internal/app/handler", "path/filepath", "must not import \"path/filepath\""},
		{"nor know its window system", "internal/app/handler", "github.com/wailsapp/wails/v2", "host toolkit"},
		{"hostenv implementations may do all three", "internal/app/hostenv", "os", ""},
		{"including the Wails host", "internal/app/hostenv/wailshost", "github.com/wailsapp/wails/v2/pkg/runtime", ""},
		{"io/fs is the portable shape", "internal/app/handler", "io/fs", ""},
		{"the rule is about the app", "internal/generate/crawl", "os", ""},
		// A host entry is where the machine is reached: which directories the
		// library and the session records live in, and which window the
		// handler is drawn in, are exactly the decisions it exists to make.
		{"the desktop shell is a host entry", "", "os", ""},
		{"and opens the window itself", "", "github.com/wailsapp/wails/v2", ""},
		// The workbench is a developer's tool on a developer's machine, and it
		// exists to run the lane CLIs (issue #5 §3.1, §5.6): shelling out is
		// its contract, not a leak. The portability amendment is about the
		// application the three hosts mount, and only about that.
		{"the workbench shells out by contract", "internal/workbench/oprunner", "os/exec", ""},
		{"and reads the registry it measures", "internal/workbench", "os", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostenvEdge(laneOf(tt.from), tt.from, tt.imports)
			assertMessage(t, got, tt.want)
		})
	}
}

func TestAtlasKeyLiteral(t *testing.T) {
	tests := []struct {
		literal string
		want    bool
	}{
		{"atlas.world.slug", true},
		{"atlas.render.as", true},
		{"atlas.hydro.huc12", true},
		{"atlas", false},
		{"atlas.", false},
		{"atlas.json", false},
		{"atlasing.away", false},
		{"the atlas.world.slug key", false},
		{"Atlas.World", false},
	}

	for _, tt := range tests {
		t.Run(tt.literal, func(t *testing.T) {
			if got := atlasKey.MatchString(tt.literal); got != tt.want {
				t.Errorf("atlasKey.MatchString(%q) = %v, want %v", tt.literal, got, tt.want)
			}
		})
	}
}

// The server half of net/http must stay usable: the application *is* an
// http.Handler, so a rule that flagged every mention of the package would be a
// rule nobody could keep.
func TestOutboundSymbols(t *testing.T) {
	tests := []struct {
		pkg, name string
		want      bool
	}{
		{"net/http", "Get", true},
		{"net/http", "Client", true},
		{"net/http", "DefaultTransport", true},
		{"net", "Dialer", true},
		{"net/http", "Handler", false},
		{"net/http", "ServeMux", false},
		{"net/http", "StatusOK", false},
		{"net/http", "ResponseWriter", false},
		{"net/http/httptest", "NewRequest", false},
	}

	for _, tt := range tests {
		t.Run(tt.pkg+"."+tt.name, func(t *testing.T) {
			if got := outbound[tt.pkg][tt.name]; got != tt.want {
				t.Errorf("outbound[%q][%q] = %v, want %v", tt.pkg, tt.name, got, tt.want)
			}
		})
	}
}

func TestLaneOf(t *testing.T) {
	tests := []struct {
		path string
		want Lane
	}{
		{"", LaneShell},
		{".", LaneShell},
		{"format", LaneFormat},
		{"format/bundle", LaneFormat},
		{"formatting", LaneOutside},
		{"internal/generate/sources/mapgenie", LaneGenerate},
		{"internal/enrich/maturity", LaneEnrich},
		{"internal/app/hostenv", LaneApp},
		{"internal/workbench", LaneWorkbench},
		{"cmd/atlas", LaneCLI},
		{"golden/depcheck", LaneGolden},
		{"internal/logging", LaneLogging},
		// Archived names, kept as the recognisable examples of a path the
		// matrix does not own: they are on the golden-reference tag, and the
		// answer for any path like them has to be the same.
		{"cmd/cartograph", LaneOutside},
		{"internal/bundle", LaneOutside},
		{"tools/tiles", LaneOutside},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := laneOf(tt.path); got != tt.want {
				t.Errorf("laneOf(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// Every message names the contract and cites the issue section, because a
// violation should teach the boundary rather than merely block it (§9).
func assertMessage(t *testing.T, got, want string) {
	t.Helper()
	switch {
	case want == "" && got != "":
		t.Fatalf("edge rejected, want allowed: %s", got)
	case want == "":
		return
	case got == "":
		t.Fatalf("edge allowed, want a finding containing %q", want)
	case !strings.Contains(got, want):
		t.Fatalf("finding %q does not contain %q", got, want)
	case !strings.Contains(got, "issue #5 §"):
		t.Fatalf("finding %q does not cite an issue section", got)
	}
}
