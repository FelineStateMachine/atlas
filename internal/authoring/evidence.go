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
	Request        string              `json:"request"`
	Acquisition    string              `json:"acquisition"`
	Source         string              `json:"source"`
	Adapter        string              `json:"adapter"`
	AdapterVersion string              `json:"adapterVersion"`
	SHA256         string              `json:"sha256"`
	Length         int64               `json:"length"`
	MediaType      string              `json:"mediaType"`
	CapturedAt     string              `json:"capturedAt"`
	License        string              `json:"license"`
	Attribution    string              `json:"attribution"`
	Set            string              `json:"set,omitempty"`
	SetDigest      string              `json:"setDigest,omitempty"`
	Termination    *CaptureTermination `json:"termination,omitempty"`
	Pages          []SelectedPage      `json:"pages,omitempty"`
}

type SelectedPage struct {
	Ordinal    int    `json:"ordinal"`
	Request    string `json:"request"`
	Locator    string `json:"locator"`
	SHA256     string `json:"sha256"`
	Length     int64  `json:"length"`
	MediaType  string `json:"mediaType"`
	CapturedAt string `json:"capturedAt"`
}

func buildReceipt(plan Plan, captures map[string]Capture) ([]byte, string, error) {
	return buildReceiptSets(plan, captures, nil)
}

func buildReceiptSets(plan Plan, captures map[string]Capture, sets map[string]CaptureSet) ([]byte, string, error) {
	selection := EvidenceSelection{
		Format: BuildReceiptFormat, Project: plan.Project, ProjectDigest: plan.ProjectDigest,
	}
	createdAt := ""
	for _, request := range plan.Requests {
		capture, ok := captures[request.ID]
		if !ok {
			return nil, "", fmt.Errorf("build selects no capture for request %s", request.ID)
		}
		selected := SelectedCapture{
			Request: request.ID, Acquisition: requestCacheKey(request.Request), Source: request.Source, SHA256: capture.Body.SHA256,
			Adapter: request.Adapter, AdapterVersion: request.AdapterVersion,
			Length: capture.Body.Length, MediaType: capture.Body.MediaType, CapturedAt: capture.CapturedAt,
			License: request.License, Attribution: request.Attribution,
		}
		if set, ok := sets[request.ID]; ok && set.Digest != "" {
			root, found := captureRootForRequest(set, request.Request)
			if !found {
				return nil, "", fmt.Errorf("capture set selects no root for request %s", request.ID)
			}
			termination := root.Termination
			selected.Set, selected.SetDigest, selected.Termination = set.ID, set.Digest, &termination
			selected.Pages = make([]SelectedPage, len(root.Pages))
			for index, page := range root.Pages {
				selected.Pages[index] = SelectedPage{
					Ordinal: index, Request: page.RequestHash, Locator: page.Locator,
					SHA256: page.Body.SHA256, Length: page.Body.Length, MediaType: page.Body.MediaType,
					CapturedAt: page.CapturedAt,
				}
				if page.CapturedAt > createdAt {
					createdAt = page.CapturedAt
				}
			}
		}
		selection.Captures = append(selection.Captures, selected)
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
		if err := validateSelectedSet(capture); err != nil {
			return EvidenceSelection{}, err
		}
		if previousRequest > capture.Request || (previousRequest == capture.Request && previousSource > capture.Source) {
			return EvidenceSelection{}, fmt.Errorf("replay captures are not canonically ordered")
		}
		seen[capture.Request] = true
		previousRequest, previousSource = capture.Request, capture.Source
	}
	return selection, nil
}

func validateSelectedSet(capture SelectedCapture) error {
	if capture.Set == "" && capture.SetDigest == "" && capture.Termination == nil && len(capture.Pages) == 0 {
		return nil
	}
	if validateSHA256("capture set", capture.Set) != nil || validateSHA256("capture set digest", capture.SetDigest) != nil || capture.Termination == nil || capture.Termination.Kind == "" || len(capture.Pages) == 0 {
		return fmt.Errorf("invalid capture set selection for request %s", capture.Request)
	}
	seen := make(map[string]bool, len(capture.Pages))
	for index, page := range capture.Pages {
		if page.Ordinal != index || validateSHA256("page request", page.Request) != nil || validateSHA256("page body", page.SHA256) != nil || page.Locator == "" || page.Length < 0 || page.MediaType == "" || seen[page.Request] {
			return fmt.Errorf("invalid selected page for request %s", capture.Request)
		}
		if _, err := time.Parse(time.RFC3339Nano, page.CapturedAt); err != nil {
			return fmt.Errorf("invalid selected page time for request %s", capture.Request)
		}
		seen[page.Request] = true
	}
	first := capture.Pages[0]
	if first.Request != capture.Acquisition || first.SHA256 != capture.SHA256 || first.Length != capture.Length || first.MediaType != capture.MediaType || first.CapturedAt != capture.CapturedAt {
		return fmt.Errorf("capture set primary page differs for request %s", capture.Request)
	}
	return nil
}

func replayPlanEvidence(plan Plan, selection EvidenceSelection, cache *Cache) (buildEvidence, error) {
	legacy, hasSets := true, false
	for _, selected := range selection.Captures {
		if selected.SetDigest != "" {
			hasSets = true
			legacy = false
		}
	}
	if !hasSets && legacy {
		captures, err := replayCaptures(plan, selection, cache)
		if err != nil {
			return buildEvidence{}, err
		}
		return legacyBuildEvidence(plan, captures), nil
	}
	selected := make(map[string]SelectedCapture, len(selection.Captures))
	for _, capture := range selection.Captures {
		selected[capture.Request] = capture
	}
	if selection.Project != plan.Project || len(selected) != len(plan.Requests) {
		return buildEvidence{}, fmt.Errorf("replay receipt does not match project %s requests", plan.Project)
	}
	evidence := buildEvidence{Captures: make(map[string]Capture), Sets: make(map[string]CaptureSet), Cached: make(map[string]bool)}
	usedSets := make(map[string]CaptureSet)
	usedRequests := 0
	var usedBytes int64
	for _, group := range planCaptureGroups(plan) {
		first := selected[group.Requests[0].ID]
		set, ok := usedSets[first.SetDigest]
		if !ok {
			var err error
			set, err = cache.SelectSet(first.Set, first.SetDigest)
			if err != nil {
				return buildEvidence{}, fmt.Errorf("select replay capture set for %s: %w", group.Requests[0].Source, err)
			}
			requests := make([]Request, len(group.Requests))
			for index := range group.Requests {
				requests[index] = group.Requests[index].Request
			}
			if err := validateSetForRequests(set, requests); err != nil {
				return buildEvidence{}, err
			}
			usedSets[first.SetDigest] = set
			usedBytes += set.TotalBytes
			for _, root := range set.Roots {
				usedRequests += len(root.Pages)
			}
		}
		for _, planned := range group.Requests {
			want, ok := selected[planned.ID]
			if !ok || want.Set != set.ID || want.SetDigest != set.Digest || want.Source != planned.Source || want.Acquisition != requestCacheKey(planned.Request) || want.Adapter != planned.Adapter || want.AdapterVersion != planned.AdapterVersion || want.License != planned.License || want.Attribution != planned.Attribution {
				return buildEvidence{}, fmt.Errorf("replay receipt does not select capture set for request %s", planned.ID)
			}
			root, ok := captureRootForRequest(set, planned.Request)
			if !ok || !selectedRootMatches(want, root) {
				return buildEvidence{}, fmt.Errorf("replay capture set differs for request %s", planned.ID)
			}
			evidence.Captures[planned.ID] = root.Pages[0]
			evidence.Sets[planned.ID] = set
			evidence.Cached[planned.ID] = true
		}
	}
	if usedRequests > plan.Budgets.Requests || usedBytes > plan.Budgets.TotalBytes {
		return buildEvidence{}, fmt.Errorf("replay capture sets exceed build budgets")
	}
	return evidence, nil
}

func selectedRootMatches(selected SelectedCapture, root CaptureRoot) bool {
	if selected.Termination == nil || selected.Termination.Kind != root.Termination.Kind || selected.Termination.Returned != root.Termination.Returned || !equalOptionalInt64(selected.Termination.Matched, root.Termination.Matched) || len(selected.Pages) != len(root.Pages) {
		return false
	}
	for index, page := range root.Pages {
		want := selected.Pages[index]
		if want.Ordinal != index || want.Request != page.RequestHash || want.Locator != page.Locator || want.SHA256 != page.Body.SHA256 || want.Length != page.Body.Length || want.MediaType != page.Body.MediaType || want.CapturedAt != page.CapturedAt {
			return false
		}
	}
	return true
}

func equalOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func legacyBuildEvidence(plan Plan, captures map[string]Capture) buildEvidence {
	evidence := buildEvidence{Captures: captures, Sets: make(map[string]CaptureSet, len(captures)), Cached: make(map[string]bool, len(captures))}
	for _, planned := range plan.Requests {
		capture := captures[planned.ID]
		request := planned.Request
		set := CaptureSet{Format: CaptureSetFormat, ID: captureSetID([]Request{request}), Adapter: request.Adapter, Version: request.AdapterVersion, Kind: request.Kind, CapturedAt: capture.CapturedAt, TotalBytes: capture.Body.Length, Roots: []CaptureRoot{{Acquisition: requestCacheKey(request), Pages: []Capture{capture}, Termination: CaptureTermination{Kind: TerminationSinglePage}}}}
		evidence.Sets[planned.ID] = set
		evidence.Cached[planned.ID] = true
	}
	return evidence
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
