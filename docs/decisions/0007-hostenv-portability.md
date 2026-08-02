# 7. One pure handler, everything OS-shaped behind hostenv

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §3.3, §10 decision 3;
  [app.md](../app.md) §1

## Context

The application needs a desktop window, a headless server for the dev loop and
CI, and — eventually, maybe — a zero-install static distribution where Go
compiles to `js/wasm` and a Service Worker synthesizes the responses. Written
the ordinary way, the handler reaches for `os`, builds paths, and opens
dialogs, and each of those calls quietly picks one of those three futures.

## Decision

The application is **one pure `http.Handler`**. It touches no filesystem,
builds no path, opens no dialog and reads no environment. Everything OS-shaped
arrives through three small interfaces:

```go
type Hostenv interface {
    Volumes()  VolumeStore
    Sessions() SessionStore
    PickFile(ctx context.Context) (io.ReadCloser, string, error)
}
```

`SessionStore` is bytes under a name, which is what lets a file, an OPFS
handle, and a test's map be the same store. `VolumeStore` hands over
descriptors; the fold that decides which build serves is pure and lives in
`format/bundle`. `PickFile` distinguishes three answers — a file,
`ErrNoSelection`, `ErrNotAvailable` — because an import that cannot happen and
an import nobody wanted are different things to say.

The rule is mechanical, not cultural: `golden/depcheck`'s `hostenv` analyzer
fails any import of `os`, `os/exec`, `path/filepath`, `syscall` or a window
toolkit from `internal/app` outside `internal/app/hostenv`.

## Consequences

- The OS implementations sit in `internal/app/hostenv/oshost`, so a host that
  is not an operating system links none of them.
- The PWA host stays reachable at near-zero cost now. **No PWA work is
  scheduled**; the discipline is the whole investment.
- A pull request that leaks an OS call past `hostenv` fails a check rather than
  earning a review comment.
- Testing the application needs no temp directories: the stores are interfaces
  and the handler is a function.
