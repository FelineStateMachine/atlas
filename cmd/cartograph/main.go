// Command cartograph is the workbench beside the Atlas reader. Where the
// reader serves the newest build of every game and asks no questions,
// cartograph answers for the collection itself: which sources feed each
// game, how mature every build stands along the axes that judge it, and what
// moved between any two builds of one game.
//
// It serves a dashboard on localhost and touches nothing on its own: reading
// the library is free, and every operation that fetches or writes runs only
// when a person submits it from the page.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

func main() {
	bundles := flag.String("bundles", "",
		"registry directory holding .atlas bundles (default: the application's own library)")
	addr := flag.String("addr", "127.0.0.1:6180", "address the dashboard listens on")
	flag.Parse()

	if *bundles == "" {
		library, err := bundle.DefaultDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cartograph: resolving the bundle library:", err)
			os.Exit(1)
		}
		*bundles = library
	}

	server := newServer(&library{dir: *bundles})
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cartograph:", err)
		os.Exit(1)
	}
	fmt.Printf("cartograph: reading %s\n", *bundles)
	fmt.Printf("cartograph: http://%s\n", listener.Addr())
	if err := http.Serve(listener, server.routes()); err != nil {
		fmt.Fprintln(os.Stderr, "cartograph:", err)
		os.Exit(1)
	}
}
