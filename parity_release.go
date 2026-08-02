//go:build !dev

package main

import (
	"io/fs"
	"net/http"
)

// Release builds carry no parity harness; the frontend's tour hotkey POSTs
// into a route that does not exist and receives a 404, and the headless
// server the harness drives does not exist to ask for.
func registerParityRoutes(*http.ServeMux) {}

func runHeadless(fs.FS) bool { return false }
