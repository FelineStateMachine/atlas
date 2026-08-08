package authoring

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

const BuildReceiptFormat = "atlas-build-receipt/v3"

// EvidenceSelection is the executable replay lock embedded in every authored
// Atlas. It identifies the exact immutable captures selected for a build; it
// deliberately contains no adapter or mapping output.
type EvidenceSelection struct {
	Format        string            `json:"format"`
	Project       string            `json:"project"`
	ProjectDigest string            `json:"projectDigest"`
	Captures      []SelectedCapture `json:"captures"`
}

type SelectedCapture struct {
	Request        string `json:"request"`
	Acquisition    string `json:"acquisition"`
	Source         string `json:"source"`
	Adapter        string `json:"adapter"`
	AdapterVersion string `json:"adapterVersion"`
	SHA256         string `json:"sha256"`
	Length         int64  `json:"length"`
	MediaType      string `json:"mediaType"`
	CapturedAt     string `json:"capturedAt"`
	License        string `json:"license"`
	Attribution    string `json:"attribution"`
}

func buildReceipt(plan Plan, captures map[string]Capture) ([]byte, string, error) {
	selection := EvidenceSelection{
		Format: BuildReceiptFormat, Project: plan.Project, ProjectDigest: plan.ProjectDigest,
	}
	createdAt := ""
	for _, request := range plan.Requests {
		capture, ok := captures[request.ID]
		if !ok {
			return nil, "", fmt.Errorf("build selects no capture for request %s", request.ID)
		}
		selection.Captures = append(selection.Captures, SelectedCapture{
			Request: request.ID, Acquisition: requestCacheKey(request.Request), Source: request.Source, SHA256: capture.Body.SHA256,
			Adapter: request.Adapter, AdapterVersion: request.AdapterVersion,
			Length: capture.Body.Length, MediaType: capture.Body.MediaType, CapturedAt: capture.CapturedAt,
			License: request.License, Attribution: request.Attribution,
		})
		if capture.CapturedAt > createdAt {
			createdAt = capture.CapturedAt
		}
	}
	if createdAt == "" {
		return nil, "", fmt.Errorf("build selects no captured evidence")
	}
	sort.Slice(selection.Captures, func(i, j int) bool {
		if selection.Captures[i].Request != selection.Captures[j].Request {
			return selection.Captures[i].Request < selection.Captures[j].Request
		}
		return selection.Captures[i].Source < selection.Captures[j].Source
	})
	data, err := json.Marshal(selection)
	if err != nil {
		return nil, "", err
	}
	return append(data, '\n'), createdAt, nil
}

func loadEvidenceSelection(path string) (EvidenceSelection, error) {
	if filepath.Ext(path) == ".atlas" {
		file, err := vnext.OpenFile(path, vnext.StandardSchema())
		if err != nil {
			return EvidenceSelection{}, fmt.Errorf("open replay Atlas: %w", err)
		}
		defer file.Close()
		volume, err := file.Volume()
		if err != nil {
			return EvidenceSelection{}, fmt.Errorf("read replay Atlas: %w", err)
		}
		for _, asset := range volume.Assets {
			if asset.ID == "build-receipt" {
				return decodeEvidenceSelection(asset.Data)
			}
		}
		return EvidenceSelection{}, fmt.Errorf("replay Atlas has no build receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return EvidenceSelection{}, fmt.Errorf("read replay receipt: %w", err)
	}
	return decodeEvidenceSelection(data)
}

func decodeEvidenceSelection(data []byte) (EvidenceSelection, error) {
	return ParseEvidenceSelection(data)
}

// ParseEvidenceSelection strictly validates the executable receipt contract
// used by replay and the compiled-artifact inspector.
func ParseEvidenceSelection(data []byte) (EvidenceSelection, error) {
	var selection EvidenceSelection
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		return EvidenceSelection{}, fmt.Errorf("decode replay receipt: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return EvidenceSelection{}, fmt.Errorf("replay receipt carries more than one JSON value")
		}
		return EvidenceSelection{}, fmt.Errorf("decode replay receipt trailer: %w", err)
	}
	if selection.Format != BuildReceiptFormat || selection.Project == "" || len(selection.Captures) == 0 || validateSHA256("project", selection.ProjectDigest) != nil {
		return EvidenceSelection{}, fmt.Errorf("invalid replay receipt")
	}
	seen := make(map[string]bool, len(selection.Captures))
	previousRequest, previousSource := "", ""
	for _, capture := range selection.Captures {
		if validateSHA256("request", capture.Request) != nil || validateSHA256("acquisition", capture.Acquisition) != nil || validateSHA256("body", capture.SHA256) != nil ||
			capture.Source == "" || capture.Adapter == "" || capture.AdapterVersion == "" || capture.Length < 0 || strings.TrimSpace(capture.MediaType) == "" ||
			strings.TrimSpace(capture.License) == "" || strings.TrimSpace(capture.Attribution) == "" || seen[capture.Request] {
			return EvidenceSelection{}, fmt.Errorf("invalid replay capture for request %s", capture.Request)
		}
		if _, err := time.Parse(time.RFC3339Nano, capture.CapturedAt); err != nil {
			return EvidenceSelection{}, fmt.Errorf("invalid replay capture time for request %s", capture.Request)
		}
		if previousRequest > capture.Request || (previousRequest == capture.Request && previousSource > capture.Source) {
			return EvidenceSelection{}, fmt.Errorf("replay captures are not canonically ordered")
		}
		seen[capture.Request] = true
		previousRequest, previousSource = capture.Request, capture.Source
	}
	return selection, nil
}

func replayCaptures(plan Plan, selection EvidenceSelection, cache *Cache) (map[string]Capture, error) {
	if selection.Project != plan.Project {
		return nil, fmt.Errorf("replay receipt belongs to project %s, not %s", selection.Project, plan.Project)
	}
	selected := make(map[string]SelectedCapture, len(selection.Captures))
	for _, capture := range selection.Captures {
		selected[capture.Request] = capture
	}
	if len(selected) != len(plan.Requests) {
		return nil, fmt.Errorf("replay receipt selects %d requests, project plans %d", len(selected), len(plan.Requests))
	}
	captures := make(map[string]Capture, len(plan.Requests))
	for _, request := range plan.Requests {
		want, ok := selected[request.ID]
		if !ok || want.Source != request.Source || want.Acquisition != requestCacheKey(request.Request) || want.Adapter != request.Adapter ||
			want.AdapterVersion != request.AdapterVersion || want.License != request.License || want.Attribution != request.Attribution {
			return nil, fmt.Errorf("replay receipt does not select source %s request %s", request.Source, request.ID)
		}
		capture, err := cache.Select(want.Acquisition, want.SHA256)
		if err != nil {
			return nil, fmt.Errorf("select replay capture for %s: %w", request.Source, err)
		}
		if capture.Body.Length != want.Length || capture.Body.MediaType != want.MediaType || capture.CapturedAt != want.CapturedAt {
			return nil, fmt.Errorf("replay capture metadata differs for source %s", request.Source)
		}
		captures[request.ID] = capture
	}
	return captures, nil
}
