//go:build !dev

package main

import "net/http"

// Release builds carry no parity harness; the frontend's tour hotkey POSTs
// into a route that does not exist and receives a 404.
func registerParityRoutes(*http.ServeMux) {}
