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
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const CaptureFormat = "atlas-capture/v1"

const sha256HexLength = sha256.Size * 2

var errExistingContent = errors.New("path already holds different content")

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
	dir, err := cache.captureDir(requestID)
	if err != nil {
		return Capture{}, err
	}
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
		bodySHA := strings.TrimSuffix(entry.Name(), ".json")
		capture, err := cache.Select(requestID, bodySHA)
		if err != nil {
			return Capture{}, fmt.Errorf("validate capture %s: %w", entry.Name(), err)
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

// Select returns one exact, validated capture for a request and body hash.
func (cache *Cache) Select(requestID, bodySHA string) (Capture, error) {
	dir, err := cache.captureDir(requestID)
	if err != nil {
		return Capture{}, err
	}
	if err := validateSHA256("body", bodySHA); err != nil {
		return Capture{}, err
	}
	path := filepath.Join(dir, bodySHA+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Capture{}, fmt.Errorf("read capture %s: %w", bodySHA, err)
	}
	capture, err := decodeCapture(data)
	if err != nil {
		return Capture{}, fmt.Errorf("decode capture %s: %w", bodySHA, err)
	}
	if err := cache.validateCapture(capture, requestID, bodySHA); err != nil {
		return Capture{}, err
	}
	return capture, nil
}

func (cache *Cache) BlobPath(ref BlobRef) string {
	path, err := cache.blobPath(ref.SHA256)
	if err != nil {
		return filepath.Join(cache.root, "blobs", "sha256", "invalid")
	}
	return path
}

// Acquire fetches or reads one planned request, records only changed bytes and
// returns the immutable capture selected for this build.
func (cache *Cache) Acquire(ctx context.Context, request Request, offline bool) (Capture, bool, error) {
	requestKey := requestCacheKey(request)
	if err := validateSHA256("request", requestKey); err != nil {
		return Capture{}, false, err
	}
	if offline {
		capture, err := cache.Latest(requestKey)
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
	if held, err := cache.Select(requestKey, hash); err == nil {
		return held, true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Capture{}, false, fmt.Errorf("select captured source %s: %w", request.Source, err)
	}
	if mediaType == "" {
		mediaType = request.MediaType
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	ref := BlobRef{SHA256: hash, Length: int64(len(body)), MediaType: mediaType}
	blobPath, err := cache.blobPath(hash)
	if err != nil {
		return Capture{}, false, err
	}
	if _, err := writeOnce(blobPath, body); err != nil {
		return Capture{}, false, fmt.Errorf("cache evidence: %w", err)
	}
	capture := Capture{
		Format: CaptureFormat, RequestHash: requestKey, Source: request.Source,
		Adapter: request.Adapter, Kind: string(request.Kind), Locator: request.IdentityLocator,
		CapturedAt: cache.now().UTC().Format(time.RFC3339Nano),
		License:    request.License, Attribution: request.Attribution, Body: ref,
	}
	data, err := json.Marshal(capture)
	if err != nil {
		return Capture{}, false, err
	}
	data = append(data, '\n')
	dir, err := cache.captureDir(requestKey)
	if err != nil {
		return Capture{}, false, err
	}
	path := filepath.Join(dir, hash+".json")
	written, writeErr := writeOnce(path, data)
	if writeErr != nil && !errors.Is(writeErr, errExistingContent) {
		return Capture{}, false, fmt.Errorf("cache capture: %w", writeErr)
	}
	selected, err := cache.Select(requestKey, hash)
	if err != nil {
		return Capture{}, false, fmt.Errorf("validate cached capture: %w", err)
	}
	return selected, !written, nil
}

func (cache *Cache) captureDir(requestID string) (string, error) {
	if err := validateSHA256("request", requestID); err != nil {
		return "", err
	}
	return filepath.Join(cache.root, "captures", requestID[:2], requestID), nil
}

func (cache *Cache) blobPath(bodySHA string) (string, error) {
	if err := validateSHA256("body", bodySHA); err != nil {
		return "", err
	}
	return filepath.Join(cache.root, "blobs", "sha256", bodySHA[:2], bodySHA), nil
}

func (cache *Cache) validateCapture(capture Capture, requestID, bodySHA string) error {
	if capture.Format != CaptureFormat {
		return fmt.Errorf("capture format %q, want %q", capture.Format, CaptureFormat)
	}
	if capture.RequestHash != requestID {
		return fmt.Errorf("capture request hash %q does not match %q", capture.RequestHash, requestID)
	}
	if capture.Source == "" || capture.Adapter == "" || capture.Locator == "" {
		return fmt.Errorf("capture envelope is incomplete")
	}
	switch RequestKind(capture.Kind) {
	case RequestFeatures, RequestRaster, RequestAsset:
	default:
		return fmt.Errorf("capture kind %q is invalid", capture.Kind)
	}
	if _, err := time.Parse(time.RFC3339Nano, capture.CapturedAt); err != nil {
		return fmt.Errorf("capture time %q: %w", capture.CapturedAt, err)
	}
	if capture.Body.SHA256 != bodySHA {
		return fmt.Errorf("capture body %q does not match path %q", capture.Body.SHA256, bodySHA)
	}
	if capture.Body.Length < 0 || strings.TrimSpace(capture.Body.MediaType) == "" {
		return fmt.Errorf("capture body metadata is incomplete")
	}
	return cache.validateBlob(capture.Body)
}

func (cache *Cache) validateBlob(ref BlobRef) error {
	path, err := cache.blobPath(ref.SHA256)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read captured body %s: %w", ref.SHA256, err)
	}
	if int64(len(data)) != ref.Length {
		return fmt.Errorf("captured body %s length is %d, want %d", ref.SHA256, len(data), ref.Length)
	}
	digest := sha256.Sum256(data)
	if actual := hex.EncodeToString(digest[:]); actual != ref.SHA256 {
		return fmt.Errorf("captured body hash is %s, want %s", actual, ref.SHA256)
	}
	return nil
}

func decodeCapture(data []byte) (Capture, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var capture Capture
	if err := decoder.Decode(&capture); err != nil {
		return Capture{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Capture{}, fmt.Errorf("capture carries more than one JSON value")
		}
		return Capture{}, fmt.Errorf("decode capture trailer: %w", err)
	}
	return capture, nil
}

func validateSHA256(name, value string) error {
	if len(value) != sha256HexLength || strings.ToLower(value) != value {
		return fmt.Errorf("%s SHA-256 %q is not lowercase hexadecimal", name, value)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s SHA-256 %q: %w", name, value, err)
	}
	return nil
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

func writeOnce(path string, data []byte) (bool, error) {
	if held, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(held, data) {
			return false, fmt.Errorf("%w at %s", errExistingContent, path)
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	stage, err := os.CreateTemp(filepath.Dir(path), ".writing-*")
	if err != nil {
		return false, err
	}
	name := stage.Name()
	defer os.Remove(name)
	if _, err := stage.Write(data); err != nil {
		_ = stage.Close()
		return false, err
	}
	if err := stage.Sync(); err != nil {
		_ = stage.Close()
		return false, err
	}
	if err := stage.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return false, err
	}
	if err := os.Link(name, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		held, readErr := os.ReadFile(path)
		if readErr != nil {
			return false, readErr
		}
		if !bytes.Equal(held, data) {
			return false, fmt.Errorf("%w at %s", errExistingContent, path)
		}
		return false, nil
	}
	return true, nil
}
