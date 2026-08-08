package authoring

import (
	"context"
	"fmt"
	"sort"
)

type buildEvidence struct {
	Captures map[string]Capture
	Sets     map[string]CaptureSet
	Cached   map[string]bool
}

type captureGroup struct {
	Requests []PlannedRequest
}

func acquirePlanEvidence(ctx context.Context, plan Plan, cache *Cache, offline bool) (buildEvidence, error) {
	evidence := buildEvidence{
		Captures: make(map[string]Capture, len(plan.Requests)),
		Sets:     make(map[string]CaptureSet, len(plan.Requests)),
		Cached:   make(map[string]bool, len(plan.Requests)),
	}
	registry := defaultAdapterRegistry()
	usedRequests := 0
	var usedBytes int64
	for _, group := range planCaptureGroups(plan) {
		remainingRequests := plan.Budgets.Requests - usedRequests
		remainingBytes := plan.Budgets.TotalBytes - usedBytes
		if remainingRequests <= 0 || remainingBytes <= 0 {
			return buildEvidence{}, fmt.Errorf("captured evidence exhausts build budgets before source %s", group.Requests[0].Source)
		}
		requests := make([]Request, len(group.Requests))
		for index := range group.Requests {
			requests[index] = group.Requests[index].Request
		}
		set, cached, err := cache.AcquireSet(ctx, requests, registry, CaptureSetOptions{
			Offline: offline, MaxRequests: remainingRequests, MaxBytes: remainingBytes,
		})
		if err != nil {
			return buildEvidence{}, err
		}
		for _, root := range set.Roots {
			usedRequests += len(root.Pages)
		}
		usedBytes += set.TotalBytes
		if usedRequests > plan.Budgets.Requests || usedBytes > plan.Budgets.TotalBytes {
			return buildEvidence{}, fmt.Errorf("captured evidence exceeds build budgets")
		}
		for _, planned := range group.Requests {
			root, ok := captureRootForRequest(set, planned.Request)
			if !ok {
				return buildEvidence{}, fmt.Errorf("capture set omits request %s", planned.ID)
			}
			evidence.Captures[planned.ID] = root.Pages[0]
			evidence.Sets[planned.ID] = set
			evidence.Cached[planned.ID] = cached
		}
	}
	return evidence, nil
}

func planCaptureGroups(plan Plan) []captureGroup {
	bySource := make(map[string][]PlannedRequest)
	for _, request := range plan.Requests {
		key := string(request.Kind) + "\x00" + request.Source
		bySource[key] = append(bySource[key], request)
	}
	groups := make([]captureGroup, 0, len(bySource))
	for _, requests := range bySource {
		sort.Slice(requests, func(i, j int) bool {
			return requestCacheKey(requests[i].Request) < requestCacheKey(requests[j].Request)
		})
		groups = append(groups, captureGroup{Requests: requests})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Requests[0].ID < groups[j].Requests[0].ID })
	return groups
}

func captureRootForRequest(set CaptureSet, request Request) (CaptureRoot, bool) {
	want := requestCacheKey(request)
	index := sort.Search(len(set.Roots), func(index int) bool { return set.Roots[index].Acquisition >= want })
	if index == len(set.Roots) || set.Roots[index].Acquisition != want {
		return CaptureRoot{}, false
	}
	return set.Roots[index], true
}
