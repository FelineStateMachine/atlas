package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/workbench/oprunner"
)

// Pipeline operations.
//
// The workbench does not link the pipeline: an operation is the `atlas` binary
// invoked exactly as a person at a terminal would invoke it, and the run's own
// event stream comes back as rows (issue #5 §3.1 -- the workbench shells out to
// the lane CLIs). That is what keeps a lane's work inside its lane while the
// page that starts it imports nothing but the format and the score.
//
// Everything an operation may touch is configured when the workbench is
// mounted, never taken from a request: a request chooses among the operations
// below and may name a source, a target and a volume, and each of those three
// is validated before an argv exists. Nothing here composes a shell command;
// argv is a slice, and the operating system gets it as one.

// Targets is where operations run: the binary, the directories, and the working
// directory a run starts in. A target that is empty is a target the workbench
// was not given, and every operation that needs it refuses by saying so.
type Targets struct {
	// Atlas is the pipeline binary. `atlas workbench` fills it with its own
	// path, so the workbench runs the same build of the pipeline that is
	// serving the page.
	Atlas string
	// Dir is the working directory operations run in. Empty is the workbench's
	// own, which is what absolute paths want.
	Dir string
	// Registry is the library of .atlas files: what the measurement pages read
	// and what compose, enrich and measure are pointed at.
	Registry string
	// Archive is the capture archive root -- the directory holding archive.json.
	Archive string
	// TileSet is the derived tile set directory, which `atlas tiles` writes.
	TileSet string
	// TileIndex is that tile set's register, which compose and enrich read. It
	// is stated rather than derived because the file's name is the generate
	// lane's to know, and the wiring that knows the lane fills it in.
	TileIndex string
}

// An operation is one pipeline subcommand the page may run.
type operation struct {
	// Name is the operation as the form submits it and the CLI spells it.
	Name string
	// Summary is the one line the page prints beside the button.
	Summary string
	// Source says the operation is aimed at one capture source, so the form
	// asks for a source and a target.
	Source bool
	// Volumes says the operation accepts an optional volume slug narrowing it
	// to one volume.
	Volumes bool
	// needs are the configured targets without which it cannot run.
	needs []string
}

// operations is the table, in the order the page lists them: the pipeline read
// left to right, capture first and measurement last.
var operations = []operation{
	{
		Name:    "crawl",
		Summary: "fetch what a publisher serves into the capture archive",
		Source:  true,
		needs:   []string{"archive"},
	},
	{
		Name:    "tiles",
		Summary: "derive raster pyramids from archived captures",
		needs:   []string{"archive", "tile set"},
	},
	{
		Name:    "compose",
		Summary: "build volumes from archived captures and derived pyramids",
		Volumes: true,
		needs:   []string{"archive", "tile index", "registry"},
	},
	{
		Name:    "enrich",
		Summary: "fold every reading of a volume together and build the richer volume",
		Volumes: true,
		needs:   []string{"archive", "tile index", "registry"},
	},
	{
		Name:    "measure",
		Summary: "score every build in the registry",
		Volumes: true,
		needs:   []string{"registry"},
	},
}

func operationByName(name string) (operation, bool) {
	for _, held := range operations {
		if held.Name == name {
			return held, true
		}
	}
	return operation{}, false
}

// Ready reports whether this operation's targets are all configured, and what
// is missing when they are not. The page prints the answer beside the form
// rather than letting a person discover it by pressing the button.
func (o operation) Ready(t Targets) (bool, string) {
	var missing []string
	for _, need := range o.needs {
		if t.of(need) == "" {
			missing = append(missing, need)
		}
	}
	if t.Atlas == "" {
		missing = append(missing, "atlas binary")
	}
	if len(missing) == 0 {
		return true, ""
	}
	return false, "no " + strings.Join(missing, ", no ") + " configured"
}

func (t Targets) of(need string) string {
	switch need {
	case "archive":
		return t.Archive
	case "tile set":
		return t.TileSet
	case "tile index":
		return t.TileIndex
	case "registry":
		return t.Registry
	}
	return ""
}

// request is what a submission asks for, before anything is trusted.
type request struct {
	Operation string
	Source    string
	Target    string
	Volume    string
}

// plan turns a submission into the operation to run, or into the refusal a
// person should read. Every refusal is a 400: it is the request that is wrong,
// and none of them starts a subprocess.
func plan(targets Targets, sources []Source, asked request) (oprunner.Operation, error) {
	held, known := operationByName(asked.Operation)
	if !known {
		return oprunner.Operation{}, fmt.Errorf("unknown operation %q", asked.Operation)
	}
	if ready, why := held.Ready(targets); !ready {
		return oprunner.Operation{}, fmt.Errorf("%s cannot run here: %s", held.Name, why)
	}

	argv := []string{targets.Atlas, held.Name, "--log-json"}
	switch held.Name {
	case "crawl":
		source, found := sourceByName(sources, asked.Source)
		if !found {
			return oprunner.Operation{}, fmt.Errorf("unknown source %q", asked.Source)
		}
		if !source.Crawlable {
			return oprunner.Operation{}, fmt.Errorf(
				"%s has no crawler registered: its captures are archived and its endpoints are not reached from here",
				source.Name)
		}
		if err := oprunner.ValidTarget(asked.Target, source.Pair); err != nil {
			return oprunner.Operation{}, fmt.Errorf("%s target %q: %w (expected %s)",
				source.Label, asked.Target, err, source.TargetHint)
		}
		argv = append(argv, "-archive", targets.Archive, "-source", source.Name, asked.Target)
	case "tiles":
		argv = append(argv, "-archive", targets.Archive, "-output", targets.TileSet)
	case "compose", "enrich":
		argv = append(argv, "-archive", targets.Archive,
			"-tiles", targets.TileIndex, "-bundles", targets.Registry)
	case "measure":
		argv = append(argv, "-bundles", targets.Registry)
	}

	if asked.Volume != "" {
		if !held.Volumes {
			return oprunner.Operation{}, fmt.Errorf("%s does not take a volume", held.Name)
		}
		// A volume slug is a target like any other: it becomes a bare
		// argument, so it is held to the same rule that keeps an argument
		// from being read as a flag.
		if err := oprunner.ValidTarget(asked.Volume, false); err != nil {
			return oprunner.Operation{}, fmt.Errorf("volume %q: %w", asked.Volume, err)
		}
		argv = append(argv, asked.Volume)
	}

	return oprunner.Operation{Name: held.Name, Dir: targets.Dir, Argv: argv}, nil
}

// errNoOperation is what an empty submission gets. It is separated only so the
// handler's switch reads as a list of refusals.
var errNoOperation = errors.New("no operation was named")
