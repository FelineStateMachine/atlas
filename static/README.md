# The static tree the desktop shell ships

This directory is what the desktop shell mounts at `/static`: the seam's built
bundle, and nothing else. It holds one file, `app.js`, and that file is a build
artifact — `make static` writes it, git ignores it, and this README is the only
thing here that is committed.

    make static   # npm build in render/, then render/dist/app.js into here
    make desktop  # the same, then the shell built around it

`make static` installs the same bytes in `dist/static` as well. That is the
build output a `-static` mount is pointed at (`make serve-static`, and the
parity harness's default); this is the tree the shell embeds. One build, two
places, because a headless host reads its assets off disk and a shipped
application carries them.

The desktop shell embeds this whole directory (`//go:embed static` in
`main.go`), which is what makes the shipped application one self-contained
file. A build that never ran `make static` embeds only this README: then
`/static/app.js` answers `404`, `<atlas-viewport>` stays an undefined custom
element that renders nothing, and every other interaction in the application
still works. That is the deletability principle of issue #5 §3.2 — the
application must build, serve and pass its non-viewport tests without the
seam's assets present — checked by the shipping binary rather than only by the
test suite.

The stylesheet system is **not** here. It lives in `internal/app/assets/css`,
is embedded by the application itself, and is served from `/assets`. The split
is the point: deleting the seam costs a page one script tag, not its chrome.
