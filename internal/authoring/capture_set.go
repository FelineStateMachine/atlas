package authoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const CaptureSetFormat = "atlas-capture-set/v1"

type CaptureSetOptions struct {
	Offline     bool
	MaxRequests int
	MaxBytes    int64
}

// CaptureSet is the atomic evidence unit selected by a build. Its manifest is
// published only after every root and continuation has terminated completely.
type CaptureSet struct {
	Format     string        `json:"format"`
	ID         string        `json:"id"`
	Digest     string        `json:"digest"`
	Adapter    string        `json:"adapter"`
	Version    string        `json:"adapterVersion"`
	Kind       RequestKind   `json:"kind"`
	CapturedAt string        `json:"capturedAt"`
	TotalBytes int64         `json:"totalBytes"`
	Roots      []CaptureRoot `json:"roots"`
}

type CaptureRoot struct {
	Acquisition string             `json:"acquisition"`
	Pages       []Capture          `json:"pages"`
	Termination CaptureTermination `json:"termination"`
}

type CaptureTermination struct {
	Kind     string `json:"kind"`
	Returned int64  `json:"returned,omitempty"`
	Matched  *int64 `json:"matched,omitempty"`
}

// AcquireSet returns one complete logical acquisition. Offline selection never
// composes independently-latest pages, so a build cannot observe a torn crawl.
func (cache *Cache) AcquireSet(ctx context.Context, requests []Request, registry AdapterRegistry, options CaptureSetOptions) (CaptureSet, bool, error) {
	roots, err := canonicalSetRequests(requests)
	if err != nil {
		return CaptureSet{}, false, err
	}
	setID := captureSetID(roots)
	if options.Offline {
		set, err := cache.LatestSet(setID)
		if err != nil {
			return CaptureSet{}, false, fmt.Errorf("capture set %s is not cached: %w", roots[0].Source, err)
		}
		if err := validateSetForRequests(set, roots); err != nil {
			return CaptureSet{}, false, err
		}
		return set, true, nil
	}
	if options.MaxRequests <= 0 {
		options.MaxRequests = defaultBuildBudgets.Requests
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultBuildBudgets.TotalBytes
	}
	adapter, err := registry.Lookup(roots[0].Adapter)
	if err != nil {
		return CaptureSet{}, false, err
	}
	if adapter.Version() != roots[0].AdapterVersion {
		return CaptureSet{}, false, fmt.Errorf("adapter %s version is %s, request requires %s", adapter.Name(), adapter.Version(), roots[0].AdapterVersion)
	}
	set := CaptureSet{
		Format: CaptureSetFormat, ID: setID, Adapter: adapter.Name(),
		Version: adapter.Version(), Kind: roots[0].Kind,
		Roots: make([]CaptureRoot, 0, len(roots)),
	}
	usedRequests := 0
	for _, root := range roots {
		captured, count, err := cache.acquireRoot(ctx, root, adapter, options, usedRequests, set.TotalBytes)
		if err != nil {
			return CaptureSet{}, false, fmt.Errorf("capture set %s: %w", root.Source, err)
		}
		usedRequests += count
		for _, page := range captured.Pages {
			set.TotalBytes += page.Body.Length
			if page.CapturedAt > set.CapturedAt {
				set.CapturedAt = page.CapturedAt
			}
		}
		set.Roots = append(set.Roots, captured)
	}
	set.Digest, err = captureSetDigest(set)
	if err != nil {
		return CaptureSet{}, false, err
	}
	data, err := json.Marshal(set)
	if err != nil {
		return CaptureSet{}, false, err
	}
	path, err := cache.captureSetPath(set.ID, set.Digest)
	if err != nil {
		return CaptureSet{}, false, err
	}
	written, err := writeOnce(path, append(data, '\n'))
	if err != nil && !errors.Is(err, errExistingContent) {
		return CaptureSet{}, false, fmt.Errorf("publish capture set: %w", err)
	}
	selected, err := cache.SelectSet(set.ID, set.Digest)
	if err != nil {
		return CaptureSet{}, false, fmt.Errorf("validate capture set: %w", err)
	}
	return selected, !written, nil
}

func (cache *Cache) acquireRoot(ctx context.Context, root Request, adapter Adapter, options CaptureSetOptions, usedRequests int, usedBytes int64) (CaptureRoot, int, error) {
	captured := CaptureRoot{Acquisition: requestCacheKey(root)}
	current := root
	progress := PageProgress{RootLocator: root.IdentityLocator, SeenIDs: make(map[string]bool)}
	seen := make(map[string]bool)
	for {
		if usedRequests+len(captured.Pages) >= options.MaxRequests {
			return CaptureRoot{}, 0, fmt.Errorf("pagination exceeds request budget %d", options.MaxRequests)
		}
		if seen[current.IdentityLocator] {
			return CaptureRoot{}, 0, fmt.Errorf("pagination continuation cycle at %s", current.IdentityLocator)
		}
		seen[current.IdentityLocator] = true
		capture, _, err := cache.Acquire(ctx, current, false)
		if err != nil {
			return CaptureRoot{}, 0, err
		}
		if capture.Body.Length > options.MaxBytes-usedBytes-rootBytes(captured) {
			return CaptureRoot{}, 0, fmt.Errorf("pagination exceeds capture-set byte budget %d", options.MaxBytes)
		}
		captured.Pages = append(captured.Pages, capture)
		body, err := os.ReadFile(cache.BlobPath(capture.Body))
		if err != nil {
			return CaptureRoot{}, 0, fmt.Errorf("read continuation page: %w", err)
		}
		decision, err := adapter.Next(current, body, &progress)
		if err != nil {
			return CaptureRoot{}, 0, err
		}
		progress.Returned, progress.Matched = decision.Returned, decision.Matched
		if decision.Done {
			captured.Termination = CaptureTermination{Kind: decision.Termination, Returned: decision.Returned, Matched: decision.Matched}
			return captured, len(captured.Pages), nil
		}
		current = decision.Next
	}
}

func rootBytes(root CaptureRoot) int64 {
	var total int64
	for _, page := range root.Pages {
		total += page.Body.Length
	}
	return total
}

func canonicalSetRequests(requests []Request) ([]Request, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("capture set has no requests")
	}
	roots := append([]Request(nil), requests...)
	sort.Slice(roots, func(i, j int) bool { return requestCacheKey(roots[i]) < requestCacheKey(roots[j]) })
	seen := make(map[string]bool, len(roots))
	for _, request := range roots {
		key := requestCacheKey(request)
		if err := validateSHA256("acquisition", key); err != nil {
			return nil, err
		}
		if request.Adapter == "" || request.AdapterVersion == "" || request.Kind == "" || seen[key] {
			return nil, fmt.Errorf("capture set has an invalid or duplicate request")
		}
		if request.Adapter != roots[0].Adapter || request.AdapterVersion != roots[0].AdapterVersion || request.Kind != roots[0].Kind {
			return nil, fmt.Errorf("capture set mixes adapters, versions, or kinds")
		}
		seen[key] = true
	}
	return roots, nil
}

func captureSetID(requests []Request) string {
	keys := make([]string, len(requests))
	for index, request := range requests {
		keys[index] = requestCacheKey(request)
	}
	sort.Strings(keys)
	data, _ := json.Marshal(keys)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func captureSetDigest(set CaptureSet) (string, error) {
	set.Digest = ""
	data, err := json.Marshal(set)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (cache *Cache) LatestSet(setID string) (CaptureSet, error) {
	dir, err := cache.captureSetDir(setID)
	if err != nil {
		return CaptureSet{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return CaptureSet{}, err
	}
	var sets []CaptureSet
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		set, err := cache.SelectSet(setID, entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return CaptureSet{}, fmt.Errorf("validate capture set %s: %w", entry.Name(), err)
		}
		sets = append(sets, set)
	}
	if len(sets) == 0 {
		return CaptureSet{}, os.ErrNotExist
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].CapturedAt != sets[j].CapturedAt {
			return sets[i].CapturedAt < sets[j].CapturedAt
		}
		return sets[i].Digest < sets[j].Digest
	})
	return sets[len(sets)-1], nil
}

func (cache *Cache) SelectSet(setID, digest string) (CaptureSet, error) {
	path, err := cache.captureSetPath(setID, digest)
	if err != nil {
		return CaptureSet{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CaptureSet{}, err
	}
	set, err := decodeCaptureSet(data)
	if err != nil {
		return CaptureSet{}, err
	}
	if err := cache.validateCaptureSet(set, setID, digest); err != nil {
		return CaptureSet{}, err
	}
	return set, nil
}

func decodeCaptureSet(data []byte) (CaptureSet, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var set CaptureSet
	if err := decoder.Decode(&set); err != nil {
		return CaptureSet{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CaptureSet{}, fmt.Errorf("capture set has invalid trailing data")
	}
	return set, nil
}

func (cache *Cache) validateCaptureSet(set CaptureSet, setID, digest string) error {
	if set.Format != CaptureSetFormat || set.ID != setID || set.Digest != digest || set.Adapter == "" || set.Version == "" || len(set.Roots) == 0 {
		return fmt.Errorf("capture set envelope is invalid")
	}
	switch set.Kind {
	case RequestFeatures, RequestRaster, RequestAsset:
	default:
		return fmt.Errorf("capture set kind %q is invalid", set.Kind)
	}
	computed, err := captureSetDigest(set)
	if err != nil || computed != digest {
		return fmt.Errorf("capture set digest does not match its contents")
	}
	var total int64
	previous := ""
	latest := ""
	adapter, err := defaultAdapterRegistry().Lookup(set.Adapter)
	if err != nil || adapter.Version() != set.Version {
		return fmt.Errorf("capture set adapter is unavailable")
	}
	for _, root := range set.Roots {
		if validateSHA256("acquisition", root.Acquisition) != nil || root.Acquisition <= previous || len(root.Pages) == 0 || root.Termination.Kind == "" {
			return fmt.Errorf("capture set root is invalid")
		}
		previous = root.Acquisition
		if root.Pages[0].RequestHash != root.Acquisition {
			return fmt.Errorf("capture set root does not begin with its acquisition")
		}
		rootSource := root.Pages[0].Source
		for _, page := range root.Pages {
			if page.Source != rootSource || page.Adapter != set.Adapter || page.Kind != string(set.Kind) {
				return fmt.Errorf("capture set page envelope differs from its root")
			}
			if err := cache.validateCapture(page, page.RequestHash, page.Body.SHA256); err != nil {
				return err
			}
			if page.Body.Length > int64(^uint64(0)>>1)-total {
				return fmt.Errorf("capture set byte total overflows")
			}
			total += page.Body.Length
			if page.CapturedAt > latest {
				latest = page.CapturedAt
			}
		}
		if err := cache.validateCaptureRoot(set, root, adapter); err != nil {
			return err
		}
	}
	if total != set.TotalBytes {
		return fmt.Errorf("capture set byte total is %d, want %d", total, set.TotalBytes)
	}
	if latest != set.CapturedAt {
		return fmt.Errorf("capture set time does not match its pages")
	}
	return nil
}

func (cache *Cache) validateCaptureRoot(set CaptureSet, root CaptureRoot, adapter Adapter) error {
	progress := PageProgress{RootLocator: root.Pages[0].Locator, SeenIDs: make(map[string]bool)}
	for index, page := range root.Pages {
		body, err := os.ReadFile(cache.BlobPath(page.Body))
		if err != nil {
			return err
		}
		request := Request{
			Kind: set.Kind, Source: page.Source, Adapter: set.Adapter, AdapterVersion: set.Version,
			Locator: page.Locator, IdentityLocator: page.Locator, MediaType: page.Body.MediaType,
		}
		decision, err := adapter.Next(request, body, &progress)
		if err != nil {
			return fmt.Errorf("validate captured continuation: %w", err)
		}
		progress.Returned, progress.Matched = decision.Returned, decision.Matched
		last := index == len(root.Pages)-1
		if last && (!decision.Done || !terminationMatches(root.Termination, decision)) {
			return fmt.Errorf("capture set termination does not match its final page")
		}
		if !last && (decision.Done || decision.Next.IdentityLocator != root.Pages[index+1].Locator) {
			return fmt.Errorf("capture set page order does not follow recorded continuations")
		}
	}
	return nil
}

func terminationMatches(termination CaptureTermination, decision PageDecision) bool {
	return termination.Kind == decision.Termination && termination.Returned == decision.Returned && equalOptionalInt64(termination.Matched, decision.Matched)
}

func validateSetForRequests(set CaptureSet, requests []Request) error {
	if set.ID != captureSetID(requests) || len(set.Roots) != len(requests) {
		return fmt.Errorf("cached capture set does not match the planned requests")
	}
	for index, request := range requests {
		if set.Roots[index].Acquisition != requestCacheKey(request) || set.Adapter != request.Adapter || set.Version != request.AdapterVersion || set.Kind != request.Kind {
			return fmt.Errorf("cached capture set does not match request %s", request.Source)
		}
	}
	return nil
}

func (cache *Cache) captureSetDir(setID string) (string, error) {
	if err := validateSHA256("capture set", setID); err != nil {
		return "", err
	}
	return filepath.Join(cache.root, "capture-sets", setID[:2], setID), nil
}

func (cache *Cache) captureSetPath(setID, digest string) (string, error) {
	dir, err := cache.captureSetDir(setID)
	if err != nil {
		return "", err
	}
	if err := validateSHA256("capture set digest", digest); err != nil {
		return "", err
	}
	return filepath.Join(dir, digest+".json"), nil
}
