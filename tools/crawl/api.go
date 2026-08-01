package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// errAbsent marks a tile the CDN does not hold. The published bounds of a layer
// are a bounding box, and content rarely fills it exactly; the origin answers
// 403 rather than 404 for an object that is not there, so both mean the same
// thing here. An absent tile is a normal result, not a failure.
var errAbsent = errors.New("not published")

// fetcher spaces every request out by a fixed interval and caps how many are
// in flight, so a crawl of tens of thousands of tiles stays a light load
// rather than a burst. It backs off when the server asks it to.
type fetcher struct {
	http     *http.Client
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

func newFetcher(concurrency int, interval time.Duration) *fetcher {
	return &fetcher{
		http: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: concurrency,
			},
		},
		interval: interval,
	}
}

// wait returns once this caller owns the next slot in the request schedule.
func (f *fetcher) wait(ctx context.Context) error {
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
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// penalise pushes the whole schedule back, so a 429 slows every worker rather
// than just the one that saw it.
func (f *fetcher) penalise(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if until := time.Now().Add(d); until.After(f.next) {
		f.next = until
	}
}

const maxAttempts = 4

func (f *fetcher) get(ctx context.Context, url string) ([]byte, string, error) {
	return f.getReferred(ctx, url, "")
}

// getReferred names the page the request stands in for. Piggyback's tile CDN
// serves only requests that arrive as its own map's, and answers everything
// else 403 -- which get would read as "not published".
func (f *fetcher) getReferred(ctx context.Context, url, referer string) ([]byte, string, error) {
	return f.do(ctx, url, func() (*http.Request, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if referer != "" {
			request.Header.Set("Referer", referer)
		}
		return request, nil
	})
}

// post carries the same manners as get -- spacing, backoff, penalties shared
// with every other request -- because an API asked with POST is no less a
// guest's request than a tile asked with GET.
func (f *fetcher) post(ctx context.Context, url, contentType string, body []byte) ([]byte, string, error) {
	return f.do(ctx, url, func() (*http.Request, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", contentType)
		return request, nil
	})
}

func (f *fetcher) do(
	ctx context.Context,
	url string,
	build func() (*http.Request, error),
) ([]byte, string, error) {
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			f.penalise(backoff)
		}
		if err := f.wait(ctx); err != nil {
			return nil, "", err
		}

		request, err := build()
		if err != nil {
			return nil, "", err
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "*/*")

		response, err := f.http.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()

		switch {
		case response.StatusCode == http.StatusNotFound,
			response.StatusCode == http.StatusForbidden:
			return nil, "", fmt.Errorf("%w: %s", errAbsent, url)
		case response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode >= 500:
			f.penalise(retryAfter(response, time.Duration(attempt+1)*2*time.Second))
			lastErr = fmt.Errorf("HTTP %d from %s", response.StatusCode, url)
			continue
		case response.StatusCode != http.StatusOK:
			return nil, "", fmt.Errorf("HTTP %d from %s", response.StatusCode, url)
		case readErr != nil:
			lastErr = readErr
			continue
		}
		return body, response.Header.Get("Content-Type"), nil
	}
	return nil, "", lastErr
}

func retryAfter(response *http.Response, fallback time.Duration) time.Duration {
	if value := response.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func (f *fetcher) getJSON(ctx context.Context, url string, into any) ([]byte, error) {
	body, _, err := f.get(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	return body, nil
}

type apiGameConfig struct {
	TilesBaseURL string `json:"tiles_base_url"`
	CDNURL       string `json:"cdn_url"`
	URL          string `json:"url"`
}

// apiGameFull is what /games already returns for every game: identity, config,
// and the full map list. The /games/{id}/full endpoint requires an account, and
// carries nothing extra that a crawl needs.
type apiGameFull struct {
	ID     int64         `json:"id"`
	Title  string        `json:"title"`
	Slug   string        `json:"slug"`
	Config apiGameConfig `json:"config"`
	Maps   []apiMapRef   `json:"maps"`
}

type apiMapRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type apiRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type apiBound struct {
	X apiRange `json:"x"`
	Y apiRange `json:"y"`
}

type apiTileSet struct {
	Name      string              `json:"name"`
	Path      string              `json:"path"`
	MinZoom   int                 `json:"min_zoom"`
	MaxZoom   int                 `json:"max_zoom"`
	Extension string              `json:"extension"`
	Bounds    map[string]apiBound `json:"bounds"`
}

type apiMapFull struct {
	ID     int64  `json:"id"`
	GameID int64  `json:"game_id"`
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	URL    string `json:"url"`
	// Where the map opens, which is the one point known to be on it. A layer
	// published without a window is searched outward from here.
	InitialLatitude  json.RawMessage `json:"initial_latitude"`
	InitialLongitude json.RawMessage `json:"initial_longitude"`
	Config           struct {
		TileSets []apiTileSet `json:"tile_sets"`
	} `json:"config"`
	Groups []struct {
		Categories []struct {
			Locations []json.RawMessage `json:"locations"`
		} `json:"categories"`
	} `json:"groups"`
}

func (m *apiMapFull) pinCount() int {
	total := 0
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			total += len(category.Locations)
		}
	}
	return total
}

func listGames(ctx context.Context, f *fetcher) ([]apiGameFull, error) {
	var games []apiGameFull
	if _, err := f.getJSON(ctx, apiBase+"/games", &games); err != nil {
		return nil, err
	}
	return games, nil
}

func findGame(games []apiGameFull, target string) (*apiGameFull, bool) {
	if id, err := strconv.ParseInt(target, 10, 64); err == nil {
		for index := range games {
			if games[index].ID == id {
				return &games[index], true
			}
		}
	}
	target = strings.ToLower(target)
	for index := range games {
		if strings.ToLower(games[index].Slug) == target {
			return &games[index], true
		}
	}
	for index := range games {
		if strings.Contains(strings.ToLower(games[index].Title), target) {
			return &games[index], true
		}
	}
	return nil, false
}

// fetchMap returns both the decoded map and its exact response bytes: the
// archive stores what the server actually sent, hashed as received.
func fetchMap(ctx context.Context, f *fetcher, id int64) (*apiMapFull, []byte, error) {
	var full apiMapFull
	url := fmt.Sprintf("%s/maps/%d/full", apiBase, id)
	raw, err := f.getJSON(ctx, url, &full)
	if err != nil {
		return nil, nil, err
	}
	return &full, raw, nil
}

func selectMaps(game *apiGameFull, target string) ([]apiMapRef, error) {
	if target == "" {
		if len(game.Maps) == 0 {
			return nil, errors.New("game publishes no maps")
		}
		return game.Maps, nil
	}
	if id, err := strconv.ParseInt(target, 10, 64); err == nil {
		for _, candidate := range game.Maps {
			if candidate.ID == id {
				return []apiMapRef{candidate}, nil
			}
		}
	}
	lowered := strings.ToLower(target)
	for _, candidate := range game.Maps {
		if strings.ToLower(candidate.Slug) == lowered {
			return []apiMapRef{candidate}, nil
		}
	}
	available := make([]string, 0, len(game.Maps))
	for _, candidate := range game.Maps {
		available = append(available, candidate.Slug)
	}
	return nil, fmt.Errorf("no map matches %q; available: %s",
		target, strings.Join(available, ", "))
}
