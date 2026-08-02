package oprunner

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The HTTP envelope: the three refusals and the streamed answer.
//
// They live in this package rather than in the workbench's handler because
// they are the properties issue #5 §5.6 asks to be carried verbatim, and a
// property that is worth naming is worth being able to test without a page
// around it.

// ErrForeignOrigin is a cross-site POST turned away.
var ErrForeignOrigin = errors.New("operations are submitted from the workbench alone")

// CheckOrigin refuses a request whose Origin is not this server's own.
//
// A browser sends Origin on any cross-site POST and omits it on a same-origin
// form submission from an ordinary page, so an absent header is not suspicious
// -- it is what a plain form looks like. A present one has to agree with the
// host the request arrived at, scheme included.
func CheckOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	for _, scheme := range []string{"http://", "https://"} {
		if origin == scheme+r.Host {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not %s", ErrForeignOrigin, origin, r.Host)
}

// ValidTarget admits the targets a crawl may be pointed at: lowercase names,
// digits, separators, and -- for a source addressed as two slugs joined by a
// slash -- exactly one slash.
//
// A leading dash is refused so a target can never be read as a flag. The rule
// is the reference workbench's, carried unchanged, because it is the shape the
// sources' own slugs actually take and a looser one would only be looser.
func ValidTarget(target string, pair bool) error {
	if target == "" {
		return errors.New("empty")
	}
	if strings.HasPrefix(target, "-") {
		return errors.New("starts with a dash")
	}
	slashes := 0
	for _, r := range target {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		case r == '/':
			slashes++
		default:
			return fmt.Errorf("contains %q", r)
		}
	}
	if pair && slashes != 1 {
		return errors.New("needs exactly one slash")
	}
	if !pair && slashes != 0 {
		return errors.New("takes no slash")
	}
	if strings.HasPrefix(target, "/") || strings.HasSuffix(target, "/") {
		return errors.New("has an empty half")
	}
	return nil
}

// A RowWriter renders one row onto the wire. The runner owns when a row
// happens; what a row looks like belongs to whoever asked for the operation,
// which is what keeps HTML out of this package.
type RowWriter func(io.Writer, Row) error

// Serve is the whole envelope around one operation: an origin-checked POST,
// one operation at a time, and the run's rows streamed as they arrive.
//
//   - A foreign Origin is 403 and nothing runs.
//   - A second operation is 409 and nothing runs.
//   - Otherwise the answer is 200 and a stream: every row is written through
//     render and flushed, so a long crawl reads as progress rather than as
//     silence.
//
// Once the first row is out the status is spent, which is why both refusals
// are decided before it. A failure after that point is the last row's business,
// not the status line's -- the operation was accepted and did happen.
func (r *Runner) Serve(w http.ResponseWriter, req *http.Request, op Operation, render RowWriter) {
	if err := CheckOrigin(req); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := op.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	release, ok := r.Acquire(op.Name)
	if !ok {
		http.Error(w, ErrBusy.Error()+": "+r.Busy(), http.StatusConflict)
		return
	}
	defer release()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// The rows are HTML fragments and are streamed, so a browser must not be
	// left to guess at the type from the first chunk it sees.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	control := http.NewResponseController(w)
	_ = Stream(req.Context(), op, func(row Row) error {
		if err := render(w, row); err != nil {
			return err
		}
		return control.Flush()
	})
	// Every ending is already a row: a failed program's exit, a page that went
	// away mid-run, a run that finished. There is nothing left to say here.
}
