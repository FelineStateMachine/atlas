# 17. The desktop host follows the application's redirects

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** [app.md](../app.md) §1.4; `redirects.go`; issue #5 §3.4, §4.2

## Context

The application gives a world exactly one URL and treats `/` as a doorway: it
answers `GET /` with `302 Found` and a `Location` naming the volume and world
the reader was last in ([app.md](../app.md) §2.2). `/open` does the same for the
topbar's selects, which cannot build a path out of their own values. Under
`atlas serve`, a browser follows those redirects like any other and lands at
the real URL, which is the whole point of having one.

Under Wails there is no browser fetch and no socket. The page is served over a
custom URL scheme, and a scheme task — `WKURLSchemeTask` on macOS, and its
equivalents on WebKitGTK and WebView2 — has no way to express a redirect. The
platform hands the `302` to the page as though it were a document. The first
launch of the desktop host showed the reader a white page reading **"Found."**,
which is the body Go writes in a redirect nobody was ever meant to read.

Three answers were available: make the handler stop redirecting, make the shell
mount a different handler, or resolve the redirect in the host.

## Decision

**The host follows them.** `redirects.go` wraps the handler before it is handed
to `wails.Run`: a GET whose answer is a 3xx with a `Location` naming a path is
re-issued against the same handler, up to five hops, and what comes back is
what the page receives.

The handler is not told. `atlas serve` goes on serving those redirects to
clients that follow them, the recorded HTTP transcript goes on judging what the
handler sends, and the two hosts differ in the transport rather than in the
application.

Three properties keep the wrapper honest:

- **GET only.** Only a navigation is redirected here; a POST answers with a
  partial.
- **Same application only.** A `Location` that does not begin with `/` is
  passed to the page untouched. The application never writes one, and if
  something ever does, it is the page's business and not the host's.
- **Streaming survives.** The wrapper keeps its own header map only until the
  status is known; from the first non-redirect status the bytes go straight to
  the real writer, and `Unwrap` hands `http.ResponseController` the flusher
  underneath. The events stream and the import's rows stream as they always
  did.

## Consequences

- The rule "everything OS-shaped lives behind hostenv" gains a sibling: *a
  limitation of the host's transport is answered in the host.* This is the
  first instance, and it is the shape any future one should take.
- The WASM service-worker host of [ADR 7](0007-hostenv-portability.md) will not
  need this: a `Response` from a service worker carries a redirect properly.
  The wrapper is the webview's, not the portability layer's.
- A redirect loop in the application shows up as `508 Loop Detected` in the
  window rather than as a hang. Two hops is the most this application can
  need; the other three are headroom.
- The finding itself is the argument for launching the thing. It is invisible
  to `go test`, to the HTTP goldens and to `atlas serve`, and it made the
  application's front door unusable.
