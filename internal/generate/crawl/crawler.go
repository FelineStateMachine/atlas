package crawl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// The crawler seam.
//
// A crawler is the outward half of a source: it knows one publisher's endpoints
// the way that source's reader knows its bytes. The two are deliberately
// separate packages, because they have opposite properties -- one is a pure
// function of an archive and the other is the only thing in Atlas that can fail
// because somebody else's server is having a bad afternoon -- and because the
// import rule that keeps the network out of the rest of the lane is a rule about
// this package, not about a habit.
//
// A crawler is handed a run and hands back nothing. What it produces lands in
// the archive through this package's write path, which is the only way anything
// reaches an archive.

// Run is one crawl: what it is aimed at, what it may do, and where it writes.
type Run struct {
	// Target is the crawler's own spelling of what to fetch -- a game slug, an
	// "object/map" pair, a planetary body, a city. Each crawler documents its
	// own, because only it can say what one means.
	Target string
	// Archive is where captures land.
	Archive *Archive
	// Fetch is the run's shared schedule. Every request goes through it.
	Fetch *Fetcher
	// Concurrency is how many requests may be in flight. The schedule still
	// spaces them, so this hides latency rather than raising the rate.
	Concurrency int
	// MaxZoom clamps how deep a pyramid is captured; zero takes what the
	// publisher offers.
	MaxZoom int
	// On is the day a dated capture answers to, empty for today. Only a source
	// whose worlds are dated captures of one ground reads it.
	On string
	// DryRun reports what would be fetched and writes nothing. The discovery
	// requests still happen: what a crawl would do cannot be known without
	// asking what is there.
	DryRun bool
	// Log is the run's event stream; nil is a logger that discards.
	Log *slog.Logger
}

// Logger is the run's stream, never nil.
func (r Run) Logger() *slog.Logger {
	if r.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return r.Log
}

// A Crawler fetches one publisher's captures into the archive.
type Crawler interface {
	// Name is the crawler's registry name, and it is the source name the
	// archive records for what it writes, so a reader knows which translator to
	// read it back with.
	Name() string
	// Usage is one line about what Target means for this crawler, printed by
	// the subcommand's help.
	Usage() string
	// Crawl runs. It is expected to be interrupted: a run that stops halfway
	// leaves an archive a later run resumes from, because every write is
	// idempotent and every fetched thing is recorded before the next is asked
	// for.
	Crawl(ctx context.Context, run Run) error
}

// ErrNotRunnable marks a crawler whose endpoints this checkout will not reach:
// the game sources' captures are archived and their endpoints are somebody
// else's editorial work, so their crawlers are kept complete and are not run.
// It is a refusal a caller can recognise rather than a message it has to read.
var ErrNotRunnable = errors.New("this crawler is not run against live endpoints from here")

// Registry is the assembly point, and -- exactly as with the source registry --
// the one file outside a crawler's own scope permitted to name one.
func Registry() []Crawler {
	return []Crawler{
		ignCrawler{},
		blueMarbleCrawler{},
	}
}

// For finds the crawler registered under a name.
func For(name string) (Crawler, error) {
	for _, crawler := range Registry() {
		if crawler.Name() == name {
			return crawler, nil
		}
	}
	return nil, fmt.Errorf("no crawler is registered as %q", name)
}

// Names lists the registered crawlers, in registration order.
func Names() []string {
	crawlers := Registry()
	out := make([]string, 0, len(crawlers))
	for _, crawler := range crawlers {
		out = append(out, crawler.Name())
	}
	return out
}

// Day is the date a dated capture answers to: the one a run named, or today in
// UTC. A crawl of a city is a crawl of a day, and the day is what its world is
// called, so it is a run's decision rather than a clock's.
func Day(on string) (string, error) {
	if on == "" {
		return time.Now().UTC().Format(time.DateOnly), nil
	}
	if _, err := time.Parse(time.DateOnly, on); err != nil {
		return "", fmt.Errorf("capture day %q is not a YYYY-MM-DD date", on)
	}
	return on, nil
}
