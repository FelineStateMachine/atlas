package authoring

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const CaptureFormat = "atlas-capture/v1"

type BlobRef struct {
	SHA256    string `json:"sha256"`
	Length    int64  `json:"length"`
	MediaType string `json:"mediaType"`
}

// Capture is an immutable source-neutral envelope around exact evidence bytes.
type Capture struct {
	Format      string  `json:"format"`
	RequestHash string  `json:"requestHash"`
	Source      string  `json:"source"`
	Adapter     string  `json:"adapter"`
	Kind        string  `json:"kind"`
	Locator     string  `json:"locator"`
	CapturedAt  string  `json:"capturedAt"`
	License     string  `json:"license,omitempty"`
	Attribution string  `json:"attribution,omitempty"`
	Body        BlobRef `json:"body"`
}

// Cache is shared by projects. The clock and client are replaceable for a
// deterministic test; production uses UTC now and a bounded HTTP client.
type Cache struct {
	root   string
	client *http.Client
	now    func() time.Time
}

func OpenCache(root string) *Cache {
	return &Cache{
		root: root, client: &http.Client{Timeout: 2 * time.Minute},
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (cache *Cache) Root() string { return cache.root }

func (cache *Cache) Has(requestID string) bool {
	_, err := cache.Latest(requestID)
	return err == nil
}

func (cache *Cache) Latest(requestID string) (Capture, error) {
	dir := filepath.Join(cache.root, "captures", requestID[:2], requestID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Capture{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var captures []Capture
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return Capture{}, err
		}
		var capture Capture
		if err := json.Unmarshal(data, &capture); err != nil {
			return Capture{}, fmt.Errorf("decode capture %s: %w", entry.Name(), err)
		}
		captures = append(captures, capture)
	}
	if len(captures) == 0 {
		return Capture{}, os.ErrNotExist
	}
	sort.Slice(captures, func(i, j int) bool {
		if captures[i].CapturedAt != captures[j].CapturedAt {
			return captures[i].CapturedAt < captures[j].CapturedAt
		}
		return captures[i].Body.SHA256 < captures[j].Body.SHA256
	})
	return captures[len(captures)-1], nil
}

func (cache *Cache) BlobPath(ref BlobRef) string {
	return filepath.Join(cache.root, "blobs", "sha256", ref.SHA256[:2], ref.SHA256)
}

// Acquire fetches or reads one planned request, records only changed bytes and
// returns the immutable capture selected for this build.
func (cache *Cache) Acquire(ctx context.Context, request Request, offline bool) (Capture, bool, error) {
	if offline {
		capture, err := cache.Latest(request.ID)
		if err != nil {
			return Capture{}, false, fmt.Errorf("request %s is not cached: %w", request.Source, err)
		}
		return capture, true, nil
	}
	body, mediaType, err := cache.read(ctx, request)
	if err != nil {
		return Capture{}, false, err
	}
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	if current, err := cache.Latest(request.ID); err == nil && current.Body.SHA256 == hash {
		return current, true, nil
	}
	if mediaType == "" {
		mediaType = request.MediaType
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	ref := BlobRef{SHA256: hash, Length: int64(len(body)), MediaType: mediaType}
	if err := writeOnce(cache.BlobPath(ref), body); err != nil {
		return Capture{}, false, fmt.Errorf("cache evidence: %w", err)
	}
	capture := Capture{
		Format: CaptureFormat, RequestHash: request.ID, Source: request.Source,
		Adapter: request.Adapter, Kind: string(request.Kind), Locator: request.IdentityLocator,
		CapturedAt: cache.now().UTC().Format(time.RFC3339Nano),
		License:    request.License, Attribution: request.Attribution, Body: ref,
	}
	data, err := json.Marshal(capture)
	if err != nil {
		return Capture{}, false, err
	}
	data = append(data, '\n')
	path := filepath.Join(cache.root, "captures", request.ID[:2], request.ID, hash+".json")
	if err := writeOnce(path, data); err != nil {
		return Capture{}, false, fmt.Errorf("cache capture: %w", err)
	}
	return capture, false, nil
}

func (cache *Cache) read(ctx context.Context, request Request) ([]byte, string, error) {
	if !isRemote(request.Locator) {
		data, err := os.ReadFile(request.Locator)
		if err != nil {
			return nil, "", fmt.Errorf("read %s source %s: %w", request.Adapter, request.Source, err)
		}
		return data, request.MediaType, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, request.Locator, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", request.MediaType)
	req.Header.Set("User-Agent", "Atlas authoring/1")
	response, err := cache.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s source %s: %w", request.Adapter, request.Source, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, "", fmt.Errorf("fetch %s source %s: %s", request.Adapter, request.Source, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read %s source %s: %w", request.Adapter, request.Source, err)
	}
	mediaType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	return body, mediaType, nil
}

func writeOnce(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(path), ".writing-*")
	if err != nil {
		return err
	}
	name := stage.Name()
	defer os.Remove(name)
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}
