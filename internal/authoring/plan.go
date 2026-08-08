package authoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type RequestKind string

const (
	RequestFeatures RequestKind = "features"
	RequestRaster   RequestKind = "raster"
	RequestAsset    RequestKind = "asset"
)

// Request is one exact acquisition planned without touching the network.
type Request struct {
	ID              string      `json:"id"`
	AcquisitionID   string      `json:"acquisition"`
	Kind            RequestKind `json:"kind"`
	Source          string      `json:"source"`
	Adapter         string      `json:"adapter"`
	AdapterVersion  string      `json:"adapterVersion"`
	Locator         string      `json:"-"`
	IdentityLocator string      `json:"locator"`
	MediaType       string      `json:"mediaType,omitempty"`
	License         string      `json:"license,omitempty"`
	Attribution     string      `json:"attribution,omitempty"`
	Raster          string      `json:"raster,omitempty"`
	Asset           string      `json:"asset,omitempty"`
	Zoom            int64       `json:"zoom,omitempty"`
	X               int64       `json:"x,omitempty"`
	Y               int64       `json:"y,omitempty"`
}

type PlannedRequest struct {
	Request
	Cached        bool  `json:"cached"`
	EstimateBytes int64 `json:"estimateBytes,omitempty"`
}

// Plan is the complete inspectable work graph for one build.
type Plan struct {
	Project       string           `json:"project"`
	Title         string           `json:"title"`
	ProjectDigest string           `json:"projectDigest"`
	World         string           `json:"world"`
	Requests      []PlannedRequest `json:"requests"`
	Estimated     int64            `json:"estimatedBytes"`
	Cached        int              `json:"cachedRequests"`
	Licenses      []Obligation     `json:"licenses,omitempty"`
	Output        string           `json:"output"`
}

type Obligation struct {
	Source      string `json:"source"`
	License     string `json:"license,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

func PlanProject(project Project, cache *Cache, output string) (Plan, error) {
	digest, err := projectDigest(project)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		Project: project.ID, Title: project.Title, ProjectDigest: digest,
		World: project.Target.World, Output: output,
	}
	for _, source := range project.Sources {
		request, err := featureRequest(project, source)
		if err != nil {
			return Plan{}, err
		}
		planned := PlannedRequest{Request: request, EstimateBytes: source.EstimateBytes}
		planned.Cached = cache != nil && cache.Has(requestCacheKey(request))
		plan.Requests = append(plan.Requests, planned)
		plan.Estimated += source.EstimateBytes
		plan.Licenses = append(plan.Licenses, Obligation{Source: source.ID, License: source.License, Attribution: source.Attribution})
	}
	for _, raster := range project.Rasters {
		requests, err := rasterRequests(project, raster)
		if err != nil {
			return Plan{}, err
		}
		each := int64(0)
		if len(requests) > 0 {
			each = raster.EstimateBytes / int64(len(requests))
		}
		for index, request := range requests {
			estimate := each
			if index == len(requests)-1 {
				estimate += raster.EstimateBytes - each*int64(len(requests))
			}
			planned := PlannedRequest{Request: request, EstimateBytes: estimate}
			planned.Cached = cache != nil && cache.Has(requestCacheKey(request))
			plan.Requests = append(plan.Requests, planned)
		}
		plan.Estimated += raster.EstimateBytes
		plan.Licenses = append(plan.Licenses, Obligation{Source: raster.ID, License: raster.License, Attribution: raster.Attribution})
	}
	for _, asset := range project.Assets {
		request := assetRequest(project, asset)
		planned := PlannedRequest{Request: request, EstimateBytes: asset.EstimateBytes}
		planned.Cached = cache != nil && cache.Has(requestCacheKey(request))
		plan.Requests = append(plan.Requests, planned)
		plan.Estimated += asset.EstimateBytes
		plan.Licenses = append(plan.Licenses, Obligation{Source: asset.ID, License: asset.License, Attribution: asset.Attribution})
	}
	sort.Slice(plan.Requests, func(i, j int) bool { return plan.Requests[i].ID < plan.Requests[j].ID })
	for _, request := range plan.Requests {
		if request.Cached {
			plan.Cached++
		}
	}
	sort.Slice(plan.Licenses, func(i, j int) bool { return plan.Licenses[i].Source < plan.Licenses[j].Source })
	return plan, nil
}

func assetRequest(project Project, asset Asset) Request {
	request := Request{
		Kind: RequestAsset, Source: asset.ID, Adapter: "asset-file",
		Locator: project.ResolveLocator(asset.Locator), IdentityLocator: asset.Locator,
		MediaType: asset.MediaType, License: asset.License, Attribution: asset.Attribution,
		Asset: asset.ID,
	}
	finalizeRequest(&request)
	return request
}

func featureRequest(project Project, source Source) (Request, error) {
	identity := source.Locator
	locator := project.ResolveLocator(identity)
	switch source.Adapter {
	case "geojson":
	case "arcgis-feature-service":
		parsed, err := url.Parse(locator)
		if err != nil {
			return Request{}, fmt.Errorf("source %s locator: %w", source.ID, err)
		}
		path := strings.TrimSuffix(parsed.Path, "/")
		if source.Query.Layer > 0 && filepath.Base(path) != strconv.Itoa(source.Query.Layer) {
			path += "/" + strconv.Itoa(source.Query.Layer)
		}
		if !strings.HasSuffix(path, "/query") {
			path += "/query"
		}
		parsed.Path = path
		query := parsed.Query()
		query.Set("f", "geojson")
		query.Set("outFields", "*")
		query.Set("returnGeometry", "true")
		where := source.Query.Where
		if where == "" {
			where = "1=1"
		}
		query.Set("where", where)
		parsed.RawQuery = query.Encode()
		locator, identity = parsed.String(), parsed.String()
	case "ogc-api-features":
		if source.Query.Collection == "" {
			return Request{}, fmt.Errorf("source %s requires query.collection", source.ID)
		}
		parsed, err := url.Parse(locator)
		if err != nil {
			return Request{}, fmt.Errorf("source %s locator: %w", source.ID, err)
		}
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/collections/" + url.PathEscape(source.Query.Collection) + "/items"
		query := parsed.Query()
		extent, err := sourceExtent(source.Mapping.Geometry, project.Target.CoordinateSpace)
		if err != nil {
			return Request{}, fmt.Errorf("source %s query extent: %w", source.ID, err)
		}
		query.Set("bbox", extentString(extent))
		query.Set("f", "json")
		if source.Query.Limit > 0 {
			query.Set("limit", strconv.Itoa(source.Query.Limit))
		}
		parsed.RawQuery = query.Encode()
		locator, identity = parsed.String(), parsed.String()
	default:
		return Request{}, fmt.Errorf("source %s uses unknown adapter %q", source.ID, source.Adapter)
	}
	request := Request{
		Kind: RequestFeatures, Source: source.ID, Adapter: source.Adapter,
		Locator: locator, IdentityLocator: identity, MediaType: source.MediaType,
		License: source.License, Attribution: source.Attribution,
	}
	finalizeRequest(&request)
	return request, nil
}

func rasterRequests(project Project, raster Raster) ([]Request, error) {
	makeRequest := func(locator, identity string, zoom, x, y int64) Request {
		request := Request{
			Kind: RequestRaster, Source: raster.ID, Adapter: raster.Adapter,
			Locator: locator, IdentityLocator: identity, MediaType: raster.MediaType,
			License: raster.License, Attribution: raster.Attribution,
			Raster: raster.ID, Zoom: zoom, X: x, Y: y,
		}
		finalizeRequest(&request)
		return request
	}
	if raster.Adapter == "raster-file" {
		return []Request{makeRequest(project.ResolveLocator(raster.Locator), raster.Locator, 0, 0, 0)}, nil
	}
	var requests []Request
	for _, level := range raster.Levels {
		for y := level.MinY; y <= level.MaxY; y++ {
			for x := level.MinX; x <= level.MaxX; x++ {
				identity := strings.NewReplacer(
					"{z}", strconv.FormatInt(level.Zoom, 10), "{x}", strconv.FormatInt(x, 10), "{y}", strconv.FormatInt(y, 10),
					"{TileMatrix}", strconv.FormatInt(level.Zoom, 10), "{TileCol}", strconv.FormatInt(x, 10), "{TileRow}", strconv.FormatInt(y, 10),
				).Replace(raster.Locator)
				requests = append(requests, makeRequest(project.ResolveLocator(identity), identity, level.Zoom, x, y))
			}
		}
	}
	return requests, nil
}

func requestID(request Request) string {
	canonical := struct {
		Source, Raster, Asset, Acquisition string
	}{request.Source, request.Raster, request.Asset, request.AcquisitionID}
	if canonical.Acquisition == "" {
		canonical.Acquisition = acquisitionID(request)
	}
	data, _ := json.Marshal(canonical)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func acquisitionID(request Request) string {
	canonical := struct {
		Kind, Adapter, Version, Locator, MediaType string
		Zoom, X, Y                                 int64
	}{string(request.Kind), request.Adapter, request.AdapterVersion, request.IdentityLocator, request.MediaType, request.Zoom, request.X, request.Y}
	data, _ := json.Marshal(canonical)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func finalizeRequest(request *Request) {
	request.AdapterVersion = adapterVersion(request.Adapter)
	request.AcquisitionID = acquisitionID(*request)
	request.ID = requestID(*request)
}

func adapterVersion(adapter string) string {
	switch adapter {
	case "geojson":
		return "geojson/v1"
	case "arcgis-feature-service":
		return "arcgis-feature-service/v1"
	case "ogc-api-features":
		return "ogc-api-features/v1"
	case "raster-file":
		return "raster-file/v1"
	case "xyz":
		return "xyz/v1"
	case "wmts":
		return "wmts-rest/v1"
	case "asset-file":
		return "asset-file/v1"
	default:
		return adapter + "/unknown"
	}
}

func requestCacheKey(request Request) string {
	if request.AcquisitionID != "" {
		return request.AcquisitionID
	}
	return request.ID
}

func projectDigest(project Project) (string, error) {
	data, err := json.Marshal(project)
	if err != nil {
		return "", fmt.Errorf("canonicalize project: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func extentString(extent [4]float64) string {
	values := make([]string, len(extent))
	for index, value := range extent {
		values[index] = strconv.FormatFloat(value, 'g', -1, 64)
	}
	return strings.Join(values, ",")
}
