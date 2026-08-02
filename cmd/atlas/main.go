// Command atlas is the one command-line binary: every lane's operations under
// one name, each as a subcommand.
//
//	atlas compose     build a volume from archived captures and derived tiles
//	atlas crawl       fetch what a publisher serves into the capture archive
//	atlas tiles       derive raster pyramids from archived captures
//	atlas translate   read archived captures and print the interchange document
//
// More subcommands arrive with the lanes that own them -- enrich,
// measure, workbench, serve, dev. Each lives in its own file and appears here as
// one line of the table below, so two people adding two subcommands do not
// collide over this file.
//
// Every run writes a structured event stream to stderr, so piped stdout stays
// clean for whatever the subcommand's product is. --log-json makes the stream
// machine-readable and --log-level, or ATLAS_LOG, opens up debug. See
// docs/logging.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// A command is one subcommand: its name, one line about it, and how to run it.
// Run is handed the arguments after the subcommand name and owns its own flag
// set, which is what keeps the flags of one lane out of another's help text.
type command struct {
	name    string
	summary string
	run     func(args []string) error
}

// commands is the subcommand table. It is a function rather than a package
// variable, and no subcommand registers itself: what this binary can do is
// visible in one place, in the order a reader meets it, with nothing happening
// before main.
func commands() []command {
	return []command{
		composeCommand(),
		crawlCommand(),
		serveCommand(),
		tilesCommand(),
		translateCommand(),
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "atlas:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no subcommand")
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	usage(os.Stderr)
	return fmt.Errorf("unknown subcommand %q", args[0])
}

func usage(w *os.File) {
	fmt.Fprintln(w, "atlas -- the .atlas pipeline and the application that reads it")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: atlas <subcommand> [flags]")
	fmt.Fprintln(w)
	names := commands()
	sort.Slice(names, func(i, j int) bool { return names[i].name < names[j].name })
	width := 0
	for _, c := range names {
		width = max(width, len(c.name))
	}
	for _, c := range names {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "every subcommand takes --log-level and --log-json; see docs/logging.md")
}

// flags builds a subcommand's flag set with a usage line that names the
// subcommand rather than the binary.
func flags(name string, argsLine string) *flag.FlagSet {
	fs := flag.NewFlagSet("atlas "+name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: atlas %s %s\n\n", name, strings.TrimSpace(argsLine))
		fs.PrintDefaults()
	}
	return fs
}
