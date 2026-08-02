package crawl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Politeness.
//
// A crawl of this corpus is thousands of small requests to somebody else's
// origin, run by hand, for a personal archive. The rule the Fetcher encodes is
// that Atlas is a guest: one schedule spaces every request of a run whatever its
// concurrency, and a refusal slows the whole run rather than the one worker that
// heard it.
//
// The schedule is a single monotonically advancing instant taken under a lock,
// not a token bucket. A bucket lets a run that idled spend its savings in a
// burst, which is exactly the shape a rate limiter is watching for; an advancing
// instant simply never lets two requests be closer together than the interval,
// however many goroutines are waiting.

// Defaults a run uses unless it says otherwise. The interval is the load-bearing
// one: at 150 ms a crawl asks for under seven things a second, which is slower
// than a person clicking around the same site.
const (
	DefaultInterval    = 150 * time.Millisecond
	DefaultConcurrency = 4
	DefaultTimeout     = 60 * time.Second
	// Attempts counts the first try. Beyond four, an origin that is refusing is
	// refusing, and asking again is rudeness rather than resilience.
	Attempts = 4
)

// UserAgent is what a crawl calls itself.
//
// It is a browser's string, and that is a decision rather than an oversight:
// several of these origins answer 403 to anything else, and a 403 is
// indistinguishable from "never published" in a tile pyramid, so an honest
// user-agent would silently punch holes in an archive. The politeness that
// matters -- the interval, the backoff, the refusal to re-ask -- is in the
// behaviour, not the header.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// ErrAbsent marks something the origin says it never published: a 404, or the
// 403 several tile origins answer with instead. A pyramid is a rectangle and its
// corners are usually empty, so this is an ordinary result and a caller records
// it rather than failing on it.
var ErrAbsent = errors.New("not published")

// ErrStaging marks a 202: an origin that has accepted the request and is
// preparing the answer. A caller waits and asks again.
var ErrStaging = errors.New("still being prepared")

// Fetcher is one run's whole outward face: its schedule, its client, and its
// manners. It is safe for concurrent use, which is the point -- every worker of
// a run shares one.
type Fetcher struct {
	client   *http.Client
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

// NewFetcher opens a run's schedule.
func NewFetcher(interval time.Duration, concurrency int) *Fetcher {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if concurrency < 1 {
		concurrency = DefaultConcurrency
	}
	return &Fetcher{
		interval: interval,
		client: &http.Client{
			Timeout:   DefaultTimeout,
			Transport: &http.Transport{MaxIdleConnsPerHost: concurrency},
		},
	}
}

// Get fetches one thing, and reports the content type the origin gave it.
func (f *Fetcher) Get(ctx context.Context, url string, headers map[string]string) ([]byte, string, error) {
	return f.do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		return request, nil
	}, url)
}

// Post sends one request body, which is how one of the corpus's origins is
// asked anything at all.
func (f *Fetcher) Post(ctx context.Context, url, contentType string, body []byte) ([]byte, string, error) {
	return f.do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", contentType)
		return request, nil
	}, url)
}

// do is the whole retry and backoff policy, in one place so that every request
// of every crawler obeys it.
func (f *Fetcher) do(ctx context.Context, build func() (*http.Request, error), url string) ([]byte, string, error) {
	var last error
	for attempt := range Attempts {
		if attempt > 0 {
			// Exponential, and applied to the shared schedule rather than to
			// this goroutine: an origin under strain should see the whole run
			// slow down.
			f.penalise(time.Duration(math.Pow(2, float64(attempt))) * time.Second)
		}
		if err := f.wait(ctx); err != nil {
			return nil, "", err
		}
		request, err := build()
		if err != nil {
			return nil, "", err
		}
		request.Header.Set("User-Agent", UserAgent)
		if request.Header.Get("Accept") == "" {
			request.Header.Set("Accept", "*/*")
		}
		response, err := f.client.Do(request)
		if err != nil {
			last = err
			continue
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()

		switch status := response.StatusCode; {
		case status == http.StatusNotFound, status == http.StatusForbidden:
			// A tile origin answers 403 for an object it never published, so
			// the two are one answer here. Treating a 403 as a failure would
			// make a run retry the empty corners of every pyramid four times.
			return nil, "", fmt.Errorf("%w: %s", ErrAbsent, url)
		case status == http.StatusAccepted:
			return nil, "", fmt.Errorf("%w: %s", ErrStaging, url)
		case status == http.StatusTooManyRequests, status >= 500:
			f.penalise(retryAfter(response, time.Duration(attempt+1)*2*time.Second))
			last = fmt.Errorf("HTTP %d from %s", status, url)
			continue
		case status != http.StatusOK:
			// Anything else is a disagreement rather than a hiccup, and asking
			// again will not settle it.
			return nil, "", fmt.Errorf("HTTP %d from %s", status, url)
		case readErr != nil:
			last = readErr
			continue
		}
		return body, response.Header.Get("Content-Type"), nil
	}
	return nil, "", last
}

// wait takes this request's slot in the run's schedule and sleeps until it.
func (f *Fetcher) wait(ctx context.Context) error {
	f.mu.Lock()
	now := time.Now()
	slot := f.next
	if slot.Before(now) {
		slot = now
	}
	f.next = slot.Add(f.interval)
	f.mu.Unlock()

	delay := time.Until(slot)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// penalise pushes the whole run's schedule forward. Slowing one worker would
// only hand its slot to another; slowing the schedule is the only thing an
// origin can actually feel.
func (f *Fetcher) penalise(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if until := time.Now().Add(d); until.After(f.next) {
		f.next = until
	}
}

// retryAfter reads the origin's own answer to "how long", falling back to the
// caller's guess. Only the seconds form is read: the HTTP-date form would need a
// clock the two ends agree on, and the fallback is safe.
func retryAfter(response *http.Response, fallback time.Duration) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After")))
	if err != nil || seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
