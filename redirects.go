package main

import (
	"net/http"
	"net/url"
	"strings"
)

// The webview cannot follow a redirect, so the host follows it.
//
// A Wails page is served over a custom URL scheme, and a scheme task has no
// way to express a redirect: WebKit hands the 302 to the page as if it were a
// document, and the reader is looking at the two words Go writes in a redirect
// body. Chromium's WebView2 and WebKitGTK answer the same way, for the same
// reason -- it is the transport, not the platform.
//
// The application is right to redirect. A world lives at exactly one URL and /
// is a doorway, not a second name for it (docs/app.md §2.2), and the headless
// host serves those redirects to a client that follows them like any other.
// So the doorway is walked through here, in the host, where the limitation is.
// The handler is not told; `atlas serve` and the desktop shell mount the same
// bytes, and the HTTP goldens keep judging the redirect that is really sent.

// redirectHops is how many doorways a request may walk through before the host
// decides the application is going in circles. Two is the most this
// application can need -- /open to /v/… , or / to / to /v/… on an empty
// library -- and the rest is headroom for a route that has not been written.
const redirectHops = 5

// followRedirects resolves the application's own redirects before the webview
// sees them. Only GET is resolved, because only a navigation can be redirected
// here, and only a Location that names a path on this application: anything
// else is somebody else's document and is passed to the page untouched.
func followRedirects(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		request := r
		for hop := 0; ; hop++ {
			caught := &redirectCatcher{real: w}
			next.ServeHTTP(caught, request)
			if caught.location == "" {
				return
			}
			if hop == redirectHops {
				http.Error(w, "the application redirected in a circle",
					http.StatusLoopDetected)
				return
			}
			target, err := url.ParseRequestURI(caught.location)
			if err != nil {
				http.Error(w, "the application redirected somewhere unreadable",
					http.StatusInternalServerError)
				return
			}
			request = request.Clone(request.Context())
			request.URL.Path, request.URL.RawQuery = target.Path, target.RawQuery
			request.RequestURI = caught.location
		}
	})
}

// redirectCatcher is a ResponseWriter that swallows a redirect and lets
// everything else through as it happens.
//
// Everything else means everything: the header map is its own until the status
// is known, and from the moment a non-redirect status is written the bytes go
// straight to the real writer. That is what keeps the two streaming answers --
// the events stream and the import's rows -- streaming rather than buffered
// (docs/app.md §5, §2.4).
type redirectCatcher struct {
	real     http.ResponseWriter
	header   http.Header
	location string
	passing  bool
}

func (c *redirectCatcher) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *redirectCatcher) WriteHeader(code int) {
	if c.passing || c.location != "" {
		return
	}
	if code >= 300 && code < 400 {
		// A Location naming a path is this application's own doorway. An
		// absolute URL is not -- the application never writes one, and if
		// something ever does, it belongs to the page rather than to the host.
		if to := c.Header().Get("Location"); strings.HasPrefix(to, "/") {
			c.location = to
			return
		}
	}
	c.passing = true
	for name, values := range c.Header() {
		c.real.Header()[name] = values
	}
	c.real.WriteHeader(code)
}

func (c *redirectCatcher) Write(body []byte) (int, error) {
	if !c.passing && c.location == "" {
		c.WriteHeader(http.StatusOK)
	}
	if c.location != "" {
		// "Found." and a link, which is the redirect's body and is nobody's to
		// read. Reported as written so the handler is not told it failed.
		return len(body), nil
	}
	return c.real.Write(body)
}

// Unwrap is how http.ResponseController finds the flusher underneath, which is
// what the handler asks for on every row of an import and every event of the
// stream.
func (c *redirectCatcher) Unwrap() http.ResponseWriter { return c.real }
