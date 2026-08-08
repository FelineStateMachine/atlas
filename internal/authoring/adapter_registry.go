package authoring

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	TerminationSinglePage         = "single-page"
	TerminationNextExhausted      = "next-exhausted"
	TerminationTransferLimitClear = "transfer-limit-clear"
)

// AdapterRegistry is an immutable lookup table for acquisition protocol
// behavior. A fresh registry is cheap and avoids process-wide mutable state.
type AdapterRegistry struct {
	adapters map[string]Adapter
}

// Adapter owns protocol versioning and continuation semantics. Mapping source
// records into typed Atlas features deliberately remains a separate concern.
type Adapter interface {
	Name() string
	Version() string
	PlanFeature(Project, Source) (Request, error)
	Next(Request, []byte, *PageProgress) (PageDecision, error)
}

type PageProgress struct {
	RootLocator string
	Returned    int64
	Matched     *int64
	SeenIDs     map[string]bool
	LastID      string
}

type PageDecision struct {
	Done        bool
	Next        Request
	Termination string
	Returned    int64
	Matched     *int64
}

type adapterDefinition struct {
	name    string
	version string
	next    func(Request, []byte, *PageProgress) (PageDecision, error)
	plan    func(Project, Source) (Request, error)
}

func (adapter adapterDefinition) Name() string { return adapter.name }

func (adapter adapterDefinition) Version() string { return adapter.version }

func (adapter adapterDefinition) PlanFeature(project Project, source Source) (Request, error) {
	if adapter.plan == nil {
		return Request{}, fmt.Errorf("adapter %s does not acquire features", adapter.name)
	}
	request, err := adapter.plan(project, source)
	if err != nil {
		return Request{}, err
	}
	request.Adapter = adapter.name
	request.AdapterVersion = adapter.version
	request.AcquisitionID = acquisitionID(request)
	request.ID = requestID(request)
	return request, nil
}

func (adapter adapterDefinition) Next(request Request, body []byte, progress *PageProgress) (PageDecision, error) {
	return adapter.next(request, body, progress)
}

func defaultAdapterRegistry() AdapterRegistry {
	definitions := []adapterDefinition{
		{name: "geojson", version: "geojson/v1", plan: planGeoJSON, next: geoJSONPage},
		{name: "arcgis-feature-service", version: "arcgis-feature-service/v2", plan: planArcGIS, next: arcGISPage},
		{name: "ogc-api-features", version: "ogc-api-features/v2", plan: planOGC, next: ogcPage},
		{name: "raster-file", version: "raster-file/v1", next: singlePage},
		{name: "xyz", version: "xyz/v1", next: singlePage},
		{name: "wmts", version: "wmts-rest/v1", next: singlePage},
		{name: "asset-file", version: "asset-file/v1", next: singlePage},
	}
	registry := AdapterRegistry{adapters: make(map[string]Adapter, len(definitions))}
	for index := range definitions {
		definition := definitions[index]
		registry.adapters[definition.name] = definition
	}
	return registry
}

func planGeoJSON(project Project, source Source) (Request, error) {
	return newFeatureRequest(project, source, project.ResolveLocator(source.Locator), source.Locator), nil
}

func planArcGIS(project Project, source Source) (Request, error) {
	parsed, err := url.Parse(project.ResolveLocator(source.Locator))
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
	extent, err := sourceExtent(source.Mapping.Geometry, project.Target.CoordinateSpace)
	if err != nil {
		return Request{}, fmt.Errorf("source %s query extent: %w", source.ID, err)
	}
	query.Set("geometry", extentString(extent))
	query.Set("geometryType", "esriGeometryEnvelope")
	query.Set("spatialRel", "esriSpatialRelIntersects")
	spatialReference := source.Query.SpatialReference
	if spatialReference == 0 {
		spatialReference = recognizedSpatialReference(source.Mapping.Geometry.SourceSpace)
	}
	if spatialReference == 0 {
		return Request{}, fmt.Errorf("source %s requires query.spatial-reference for source space %q", source.ID, source.Mapping.Geometry.SourceSpace)
	}
	query.Set("inSR", strconv.Itoa(spatialReference))
	query.Set("outSR", strconv.Itoa(spatialReference))
	objectID := source.Query.ObjectID
	if objectID == "" {
		objectID = "OBJECTID"
	}
	if !validArcGISField(objectID) {
		return Request{}, fmt.Errorf("source %s has invalid query.object-id %q", source.ID, objectID)
	}
	query.Set("orderByFields", objectID+" ASC")
	where := source.Query.Where
	if where == "" {
		where = "1=1"
	}
	query.Set("where", where)
	if source.Query.Limit > 0 {
		query.Set("resultRecordCount", strconv.Itoa(source.Query.Limit))
	}
	query.Set("resultOffset", "0")
	parsed.RawQuery = query.Encode()
	return newFeatureRequest(project, source, parsed.String(), parsed.String()), nil
}

func recognizedSpatialReference(sourceSpace string) int {
	switch strings.ToUpper(strings.TrimSpace(sourceSpace)) {
	case "EPSG:4326", "OGC:CRS84", "CRS84", "WGS84", "URN:OGC:DEF:CRS:OGC::CRS84":
		return 4326
	case "EPSG:3857", "EPSG:102100", "ESRI:102100", "WEB-MERCATOR":
		return 3857
	default:
		return 0
	}
}

func validArcGISField(field string) bool {
	for index, character := range field {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' || (index > 0 && ((character >= '0' && character <= '9') || character == '.')) {
			continue
		}
		return false
	}
	return field != ""
}

func planOGC(project Project, source Source) (Request, error) {
	if source.Query.Collection == "" {
		return Request{}, fmt.Errorf("source %s requires query.collection", source.ID)
	}
	parsed, err := url.Parse(project.ResolveLocator(source.Locator))
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
	return newFeatureRequest(project, source, parsed.String(), parsed.String()), nil
}

func newFeatureRequest(project Project, source Source, locator, identity string) Request {
	return Request{
		Kind: RequestFeatures, Source: source.ID, Adapter: source.Adapter,
		Locator: locator, IdentityLocator: identity, MediaType: source.MediaType,
		License: source.License, Attribution: source.Attribution,
	}
}

func (registry AdapterRegistry) Lookup(name string) (Adapter, error) {
	adapter, ok := registry.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q", name)
	}
	return adapter, nil
}

func singlePage(_ Request, _ []byte, progress *PageProgress) (PageDecision, error) {
	return PageDecision{Done: true, Termination: TerminationSinglePage, Returned: progress.Returned}, nil
}

func geoJSONPage(_ Request, body []byte, progress *PageProgress) (PageDecision, error) {
	page, err := decodeFeaturePage(body)
	if err != nil {
		return PageDecision{}, err
	}
	if page.ExceededTransferLimit || nextLink(page.Links) != "" {
		return PageDecision{}, fmt.Errorf("GeoJSON response declares pagination but the geojson adapter is single-page")
	}
	returned, err := pageReturned(page)
	if err != nil {
		return PageDecision{}, err
	}
	return PageDecision{Done: true, Termination: TerminationSinglePage, Returned: progress.Returned + returned}, nil
}

func arcGISPage(request Request, body []byte, progress *PageProgress) (PageDecision, error) {
	page, err := decodeFeaturePage(body)
	if err != nil {
		return PageDecision{}, err
	}
	returned, err := pageReturned(page)
	if err != nil {
		return PageDecision{}, err
	}
	total := progress.Returned + returned
	if err := recordPageIDs("ArcGIS", page, progress, true); err != nil {
		return PageDecision{}, err
	}
	if !page.ExceededTransferLimit {
		if progress.Returned > 0 && returned == 0 {
			return PageDecision{}, fmt.Errorf("ArcGIS continuation is missing the page promised by its transfer limit")
		}
		return PageDecision{Done: true, Termination: TerminationTransferLimitClear, Returned: total}, nil
	}
	if returned == 0 {
		return PageDecision{}, fmt.Errorf("ArcGIS transfer limit continuation returned no features")
	}
	parsed, err := url.Parse(request.IdentityLocator)
	if err != nil {
		return PageDecision{}, fmt.Errorf("parse ArcGIS continuation: %w", err)
	}
	query := parsed.Query()
	offset, err := optionalInt64(query.Get("resultOffset"))
	if err != nil {
		return PageDecision{}, fmt.Errorf("ArcGIS resultOffset: %w", err)
	}
	query.Set("resultOffset", strconv.FormatInt(offset+returned, 10))
	parsed.RawQuery = query.Encode()
	next := continuationRequest(request, parsed.String())
	return PageDecision{Next: next, Returned: total}, nil
}

func compareStableID(left, right string) int {
	leftInteger, leftOK := new(big.Int).SetString(left, 10)
	rightInteger, rightOK := new(big.Int).SetString(right, 10)
	if leftOK && rightOK {
		return leftInteger.Cmp(rightInteger)
	}
	return strings.Compare(left, right)
}

func recordPageIDs(protocol string, page geoJSON, progress *PageProgress, ordered bool) error {
	if progress.SeenIDs == nil {
		progress.SeenIDs = make(map[string]bool)
	}
	for _, feature := range page.Features {
		id := scalarString(feature.ID)
		if id == "" {
			return fmt.Errorf("%s response has a feature without an object ID", protocol)
		}
		if progress.SeenIDs[id] {
			return fmt.Errorf("%s response repeats object ID %s across pages", protocol, id)
		}
		if ordered && progress.LastID != "" && compareStableID(progress.LastID, id) >= 0 {
			return fmt.Errorf("%s response object IDs are not strictly ordered", protocol)
		}
		progress.SeenIDs[id] = true
		progress.LastID = id
	}
	return nil
}

func ogcPage(request Request, body []byte, progress *PageProgress) (PageDecision, error) {
	page, err := decodeFeaturePage(body)
	if err != nil {
		return PageDecision{}, err
	}
	returned, err := pageReturned(page)
	if err != nil {
		return PageDecision{}, err
	}
	total := progress.Returned + returned
	if err := recordPageIDs("OGC", page, progress, false); err != nil {
		return PageDecision{}, err
	}
	matched, err := reconcileMatched(progress.Matched, page.NumberMatched)
	if err != nil {
		return PageDecision{}, err
	}
	next := nextLink(page.Links)
	if next != "" {
		if matched != nil && total >= *matched {
			return PageDecision{}, fmt.Errorf("OGC response continues after all %d matched features were returned", *matched)
		}
		locator, err := resolveContinuation(progress.RootLocator, request.IdentityLocator, next)
		if err != nil {
			return PageDecision{}, err
		}
		return PageDecision{Next: continuationRequest(request, locator), Returned: total, Matched: matched}, nil
	}
	if matched != nil && total != *matched {
		return PageDecision{}, fmt.Errorf("OGC response is incomplete: %d matched but only %d returned", *matched, total)
	}
	return PageDecision{Done: true, Termination: TerminationNextExhausted, Returned: total, Matched: matched}, nil
}

func decodeFeaturePage(body []byte) (geoJSON, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var page geoJSON
	if err := decoder.Decode(&page); err != nil {
		return geoJSON{}, fmt.Errorf("decode feature page: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return geoJSON{}, fmt.Errorf("feature page carries more than one JSON value")
		}
		return geoJSON{}, fmt.Errorf("decode feature page trailer: %w", err)
	}
	if page.Type != "FeatureCollection" {
		return geoJSON{}, fmt.Errorf("feature page is %q, want FeatureCollection", page.Type)
	}
	return page, nil
}

func pageReturned(page geoJSON) (int64, error) {
	returned := int64(len(page.Features))
	if page.NumberReturned != nil && *page.NumberReturned != returned {
		return 0, fmt.Errorf("response count differs from its feature payload")
	}
	return returned, nil
}

func reconcileMatched(held, next *int64) (*int64, error) {
	if next == nil {
		return held, nil
	}
	if *next < 0 {
		return nil, fmt.Errorf("response has a negative matched count")
	}
	if held != nil && *held != *next {
		return nil, fmt.Errorf("response changed numberMatched from %d to %d", *held, *next)
	}
	value := *next
	return &value, nil
}

func nextLink(links []geoJSONLink) string {
	for _, link := range links {
		if strings.EqualFold(link.Rel, "next") && strings.TrimSpace(link.Href) != "" {
			return strings.TrimSpace(link.Href)
		}
	}
	return ""
}

func resolveContinuation(rootLocator, currentLocator, reference string) (string, error) {
	root, err := url.Parse(rootLocator)
	if err != nil {
		return "", fmt.Errorf("parse continuation root: %w", err)
	}
	current, err := url.Parse(currentLocator)
	if err != nil {
		return "", fmt.Errorf("parse current page: %w", err)
	}
	next, err := url.Parse(reference)
	if err != nil {
		return "", fmt.Errorf("parse next page: %w", err)
	}
	resolved := current.ResolveReference(next)
	if !strings.EqualFold(root.Scheme, resolved.Scheme) || !strings.EqualFold(root.Host, resolved.Host) {
		return "", fmt.Errorf("next page leaves the configured source origin")
	}
	return resolved.String(), nil
}

func continuationRequest(root Request, locator string) Request {
	next := root
	next.Locator = locator
	next.IdentityLocator = locator
	finalizeRequest(&next)
	return next
}

func optionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
