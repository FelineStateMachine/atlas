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
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

func main() {
	bundles := flag.String("bundles", "",
		"registry directory holding .atlas bundles (default: the application's own library)")
	addr := flag.String("addr", "127.0.0.1:6180", "address the dashboard listens on")
	archive := flag.String("archive", "",
		"fmg-archive root fetches fill and builds read (operations require it)")
	repo := flag.String("repo", "",
		"atlas repository whose tools operations run (default: found from the working directory)")
	flag.Parse()

	if *bundles == "" {
		library, err := bundle.DefaultDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cartograph: resolving the bundle library:", err)
			os.Exit(1)
		}
		*bundles = library
	}
	if *repo == "" {
		*repo = findRepo()
	}

	server := newServer(&library{dir: *bundles},
		&workshop{repo: *repo, archive: *archive, bundles: *bundles})
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

// findRepo walks up from the working directory looking for this module's own
// go.mod, so `go run ./cmd/cartograph` from anywhere inside the checkout
// needs no pointing. Anywhere else, -repo says where the tools live; the
// dashboard's views work without one, and only operations demand it.
func findRepo() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/FelineStateMachine/atlas\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
