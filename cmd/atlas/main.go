// Command atlas is the one command-line binary: every lane's operations as
// subcommands of a single name, so a reader learns one tool and a script
// spells one path.
//
//	atlas serve -bundles DIR      # the headless application host
//
// The subcommand table is explicit and lives in this file. Each subcommand is
// defined in its own file beside it, as a function returning its entry --
// there is no registration by init, because a table you can read is worth more
// than one that assembles itself (issue #5 §9).
package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

// command is one subcommand: how it is spelled, what it is for, and how to run
// it. run receives the arguments after the subcommand name and returns the
// error that ends the process.
type command struct {
	name    string
	summary string
	run     func(args []string) error
}

// commands is the table. One line per subcommand.
func commands() []command {
	return []command{
		serveCommand(),
	}
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	name := os.Args[1]
	if name == "help" || name == "-h" || name == "--help" {
		usage(os.Stdout)
		return
	}
	for _, cmd := range commands() {
		if cmd.name != name {
			continue
		}
		if err := cmd.run(os.Args[2:]); err != nil {
			if err == flag.ErrHelp {
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "atlas %s: %v\n", name, err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintf(os.Stderr, "atlas: no such subcommand %q\n\n", name)
	usage(os.Stderr)
	os.Exit(2)
}

func usage(w *os.File) {
	fmt.Fprintln(w, "usage: atlas <subcommand> [flags]")
	fmt.Fprintln(w)
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cmd := range commands() {
		fmt.Fprintf(table, "  %s\t%s\n", cmd.name, cmd.summary)
	}
	_ = table.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run `atlas <subcommand> -h` for a subcommand's flags")
}
