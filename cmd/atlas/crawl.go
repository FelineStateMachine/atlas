package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/internal/generate/crawl"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

func crawlCommand() command {
	return command{
		name:    "crawl",
		summary: "fetch what a publisher serves into the capture archive",
		run:     runCrawl,
	}
}

func runCrawl(args []string) error {
	fs := flags("crawl", "-archive DIR -source NAME TARGET [-n]")
	archiveDir := fs.String("archive", "", "capture archive root (the directory holding archive.json)")
	source := fs.String("source", "", "which crawler to run; -source list says what there is")
	interval := fs.Duration("interval", crawl.DefaultInterval, "minimum spacing between requests")
	concurrency := fs.Int("concurrency", crawl.DefaultConcurrency, "requests in flight at once")
	maxZoom := fs.Int("max-zoom", 0, "deepest zoom to capture; 0 takes what the publisher offers")
	on := fs.String("on", "", "the day a dated capture answers to, YYYY-MM-DD; default today")
	dry := fs.Bool("n", false, "report what would be fetched and write nothing")
	var options logging.Options
	options.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *source == "" || *source == "list" {
		listCrawlers()
		if *source == "" {
			return errors.New("-source is required")
		}
		return nil
	}
	crawler, err := crawl.For(*source)
	if err != nil {
		listCrawlers()
		return err
	}
	target := strings.Join(fs.Args(), " ")
	if target == "" {
		return fmt.Errorf("%s crawls %s", crawler.Name(), crawler.Usage())
	}
	if *archiveDir == "" {
		fs.Usage()
		return errors.New("-archive is required")
	}
	log, err := logging.Setup(options)
	if err != nil {
		return err
	}
	store, err := crawl.Open(*archiveDir)
	if err != nil {
		return err
	}

	// A crawl is hand-run and is meant to be interruptible: every write is
	// idempotent and every fetched thing is recorded before the next is asked
	// for, so stopping one is a normal way to end it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := time.Now()
	err = crawler.Crawl(ctx, crawl.Run{
		Target:      target,
		Archive:     store,
		Fetch:       crawl.NewFetcher(*interval, *concurrency),
		Concurrency: *concurrency,
		MaxZoom:     *maxZoom,
		On:          *on,
		DryRun:      *dry,
		Log:         log,
	})
	if errors.Is(err, crawl.ErrNotRunnable) {
		return fmt.Errorf("%s: %w", crawler.Name(), err)
	}
	if err != nil {
		return err
	}
	log.Info("crawl finished", logging.Op("crawl"), logging.Source(crawler.Name()),
		logging.Dur(time.Since(started)))
	return nil
}

// listCrawlers prints what there is to crawl and what each one's target means,
// because a target is the one argument no flag can describe for you.
func listCrawlers() {
	fmt.Fprintln(os.Stderr, "crawlers:")
	for _, crawler := range crawl.Registry() {
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", crawler.Name(), crawler.Usage())
	}
}
