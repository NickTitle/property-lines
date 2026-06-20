package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 12 << 20

type parcelQueryRequest struct {
	ServiceURL   string  `json:"serviceUrl"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RadiusMeters float64 `json:"radiusMeters"`
	Limit        int     `json:"limit"`
}

type parcelAreaRequest struct {
	ServiceURL string  `json:"serviceUrl"`
	North      float64 `json:"north"`
	South      float64 `json:"south"`
	East       float64 `json:"east"`
	West       float64 `json:"west"`
	Limit      int     `json:"limit"`
}

type geographicBounds struct {
	North float64
	South float64
	East  float64
	West  float64
}

type parcelSource struct {
	Name       string
	ServiceURL string
	Bounds     geographicBounds
}

type arcGISFeatureSet struct {
	Error    *arcGISError    `json:"error,omitempty"`
	Fields   []arcGISField   `json:"fields,omitempty"`
	Features []arcGISFeature `json:"features,omitempty"`
	CRS      map[string]any  `json:"crs,omitempty"`
	Type     string          `json:"type,omitempty"`
}

type arcGISError struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details"`
}

type arcGISField struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Type  string `json:"type"`
}

type arcGISFeature struct {
	Attributes map[string]any `json:"attributes"`
	Geometry   arcGISGeometry `json:"geometry"`
}

type arcGISGeometry struct {
	Rings [][][2]float64 `json:"rings"`
	Paths [][][2]float64 `json:"paths"`
	X     *float64       `json:"x"`
	Y     *float64       `json:"y"`
}

type featureCollection struct {
	Type     string       `json:"type"`
	Features []geoFeature `json:"features"`
}

type geoFeature struct {
	Type       string         `json:"type"`
	Geometry   map[string]any `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

var automaticParcelSources = []parcelSource{
	{
		Name:       "NYS Tax Parcels Public",
		ServiceURL: "https://gisservices.its.ny.gov/arcgis/rest/services/NYS_Tax_Parcels_Public/MapServer/1",
		Bounds: geographicBounds{
			North: 45.1,
			South: 40.45,
			East:  -71.75,
			West:  -79.8,
		},
	},
}

func main() {
	port := getenv("PORT", "4190")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /api/parcels/query", handleParcelQuery)
	mux.HandleFunc("POST /api/parcels/area", handleParcelArea)
	mux.Handle("GET /", staticHandler())

	server := &http.Server{
		Addr:              "127.0.0.1:" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("property-lines listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func staticHandler() http.Handler {
	static := http.FileServer(http.Dir("static"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "static/index.html")
			return
		}

		r.URL.Path = path.Clean(r.URL.Path)
		static.ServeHTTP(w, r)
	})
}

func handleParcelQuery(w http.ResponseWriter, r *http.Request) {
	var request parcelQueryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid parcel query JSON.")
		return
	}

	request.ServiceURL = strings.TrimSpace(request.ServiceURL)
	sourceName, err := prepareParcelQuery(&request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	collection, usedURL, err := queryArcGISLayer(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queryUrl":   usedURL,
		"geojson":    collection,
		"serviceUrl": request.ServiceURL,
		"sourceName": sourceName,
	})
}

func handleParcelArea(w http.ResponseWriter, r *http.Request) {
	var request parcelAreaRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid parcel area query JSON.")
		return
	}

	request.ServiceURL = strings.TrimSpace(request.ServiceURL)
	sourceName, err := prepareParcelArea(&request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	collection, usedURL, err := queryArcGISArea(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queryUrl":   usedURL,
		"geojson":    collection,
		"serviceUrl": request.ServiceURL,
		"sourceName": sourceName,
		"bounds": map[string]float64{
			"north": request.North,
			"south": request.South,
			"east":  request.East,
			"west":  request.West,
		},
	})
}

func prepareParcelQuery(request *parcelQueryRequest) (string, error) {
	if math.Abs(request.Latitude) > 90 || math.Abs(request.Longitude) > 180 {
		return "", errors.New("Latitude or longitude is out of range.")
	}

	return prepareParcelSource(&request.ServiceURL, request.Latitude, request.Longitude)
}

func prepareParcelArea(request *parcelAreaRequest) (string, error) {
	if math.Abs(request.North) > 90 || math.Abs(request.South) > 90 || math.Abs(request.East) > 180 || math.Abs(request.West) > 180 {
		return "", errors.New("Area bounds are out of range.")
	}

	if request.North <= request.South || request.East <= request.West {
		return "", errors.New("Area bounds are not valid.")
	}

	centerLat := (request.North + request.South) / 2
	centerLon := (request.East + request.West) / 2
	widthMeters := metersBetween(centerLat, request.West, centerLat, request.East)
	heightMeters := metersBetween(request.South, request.West, request.North, request.West)

	if widthMeters > 5000 || heightMeters > 5000 {
		return "", errors.New("Zoom in before caching. Visible area must be under 5 km wide and tall.")
	}

	return prepareParcelSource(&request.ServiceURL, centerLat, centerLon)
}

func prepareParcelSource(serviceURL *string, latitude, longitude float64) (string, error) {
	if strings.TrimSpace(*serviceURL) != "" {
		*serviceURL = strings.TrimSpace(*serviceURL)
		if err := validateParcelServiceURL(*serviceURL); err != nil {
			return "", err
		}
		return "Manual parcel layer", nil
	}

	source, ok := parcelSourceForPoint(latitude, longitude)
	if !ok {
		return "", errors.New("No automatic parcel source is configured for this location. Paste a public ArcGIS parcel layer URL.")
	}

	*serviceURL = source.ServiceURL
	return source.Name, nil
}

func parcelSourceForPoint(latitude, longitude float64) (parcelSource, bool) {
	for _, source := range automaticParcelSources {
		if containsCoordinate(source.Bounds, latitude, longitude) {
			return source, true
		}
	}

	return parcelSource{}, false
}

func containsCoordinate(bounds geographicBounds, latitude, longitude float64) bool {
	return latitude <= bounds.North && latitude >= bounds.South && longitude <= bounds.East && longitude >= bounds.West
}

func validateParcelServiceURL(serviceURL string) error {
	parsed, err := url.Parse(serviceURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("Parcel layer URL must be a complete http or https URL.")
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("Parcel layer URL must use http or https.")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return errors.New("Local parcel layer URLs are not allowed.")
	}

	return nil
}

func queryArcGISLayer(ctx context.Context, request parcelQueryRequest) (featureCollection, string, error) {
	limit := request.Limit
	if limit <= 0 || limit > 25 {
		limit = 10
	}

	radius := request.RadiusMeters
	if radius < 0 {
		radius = 0
	}
	if radius > 200 {
		radius = 200
	}

	geoJSONURL := arcGISQueryURL(request, limit, radius, "geojson")
	body, err := fetch(ctx, geoJSONURL)
	if err == nil {
		var collection featureCollection
		if json.Unmarshal(body, &collection) == nil && collection.Type == "FeatureCollection" {
			return collection, geoJSONURL, nil
		}
	}

	jsonURL := arcGISQueryURL(request, limit, radius, "json")
	body, err = fetch(ctx, jsonURL)
	if err != nil {
		return featureCollection{}, jsonURL, err
	}

	var featureSet arcGISFeatureSet
	if err := json.Unmarshal(body, &featureSet); err != nil {
		return featureCollection{}, jsonURL, fmt.Errorf("Parcel layer returned invalid JSON: %w", err)
	}

	if featureSet.Error != nil {
		return featureCollection{}, jsonURL, fmt.Errorf("ArcGIS query failed: %s", featureSet.Error.Message)
	}

	return esriFeatureSetToGeoJSON(featureSet), jsonURL, nil
}

func queryArcGISArea(ctx context.Context, request parcelAreaRequest) (featureCollection, string, error) {
	limit := request.Limit
	if limit <= 0 || limit > 500 {
		limit = 300
	}

	geoJSONURL := arcGISAreaQueryURL(request, limit, "geojson")
	body, err := fetch(ctx, geoJSONURL)
	if err == nil {
		var collection featureCollection
		if json.Unmarshal(body, &collection) == nil && collection.Type == "FeatureCollection" {
			return collection, geoJSONURL, nil
		}
	}

	jsonURL := arcGISAreaQueryURL(request, limit, "json")
	body, err = fetch(ctx, jsonURL)
	if err != nil {
		return featureCollection{}, jsonURL, err
	}

	var featureSet arcGISFeatureSet
	if err := json.Unmarshal(body, &featureSet); err != nil {
		return featureCollection{}, jsonURL, fmt.Errorf("Parcel layer returned invalid JSON: %w", err)
	}

	if featureSet.Error != nil {
		return featureCollection{}, jsonURL, fmt.Errorf("ArcGIS query failed: %s", featureSet.Error.Message)
	}

	return esriFeatureSetToGeoJSON(featureSet), jsonURL, nil
}

func arcGISQueryURL(request parcelQueryRequest, limit int, radius float64, format string) string {
	layerURL := strings.TrimRight(request.ServiceURL, "/")
	if strings.HasSuffix(strings.ToLower(layerURL), "/query") {
		layerURL = strings.TrimSuffix(layerURL, "/query")
	}

	values := url.Values{}
	values.Set("f", format)
	values.Set("where", "1=1")
	values.Set("outFields", "*")
	values.Set("returnGeometry", "true")
	values.Set("outSR", "4326")
	values.Set("inSR", "4326")
	values.Set("geometryType", "esriGeometryPoint")
	values.Set("spatialRel", "esriSpatialRelIntersects")
	values.Set("resultRecordCount", strconv.Itoa(limit))
	values.Set("geometry", fmt.Sprintf(`{"x":%.8f,"y":%.8f,"spatialReference":{"wkid":4326}}`, request.Longitude, request.Latitude))

	if radius > 0 {
		values.Set("distance", strconv.FormatFloat(radius, 'f', 1, 64))
		values.Set("units", "esriSRUnit_Meter")
	}

	return layerURL + "/query?" + values.Encode()
}

func arcGISAreaQueryURL(request parcelAreaRequest, limit int, format string) string {
	layerURL := strings.TrimRight(request.ServiceURL, "/")
	if strings.HasSuffix(strings.ToLower(layerURL), "/query") {
		layerURL = strings.TrimSuffix(layerURL, "/query")
	}

	values := url.Values{}
	values.Set("f", format)
	values.Set("where", "1=1")
	values.Set("outFields", "*")
	values.Set("returnGeometry", "true")
	values.Set("outSR", "4326")
	values.Set("inSR", "4326")
	values.Set("geometryType", "esriGeometryEnvelope")
	values.Set("spatialRel", "esriSpatialRelIntersects")
	values.Set("resultRecordCount", strconv.Itoa(limit))
	values.Set("geometry", fmt.Sprintf(
		`{"xmin":%.8f,"ymin":%.8f,"xmax":%.8f,"ymax":%.8f,"spatialReference":{"wkid":4326}}`,
		request.West,
		request.South,
		request.East,
		request.North,
	))

	return layerURL + "/query?" + values.Encode()
}

func metersBetween(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000
	toRadians := math.Pi / 180
	phi1 := lat1 * toRadians
	phi2 := lat2 * toRadians
	deltaPhi := (lat2 - lat1) * toRadians
	deltaLambda := (lon2 - lon1) * toRadians
	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	return 2 * earthRadiusMeters * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func fetch(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, errors.New("Parcel query was cancelled.")
		}
		return nil, fmt.Errorf("Parcel layer request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("Parcel layer response could not be read: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("Parcel layer returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func esriFeatureSetToGeoJSON(featureSet arcGISFeatureSet) featureCollection {
	collection := featureCollection{
		Type:     "FeatureCollection",
		Features: make([]geoFeature, 0, len(featureSet.Features)),
	}

	for _, feature := range featureSet.Features {
		geometry := esriGeometryToGeoJSON(feature.Geometry)
		if geometry == nil {
			continue
		}

		collection.Features = append(collection.Features, geoFeature{
			Type:       "Feature",
			Geometry:   geometry,
			Properties: normalizeAttributes(feature.Attributes),
		})
	}

	return collection
}

func esriGeometryToGeoJSON(geometry arcGISGeometry) map[string]any {
	switch {
	case len(geometry.Rings) > 0:
		polygons := make([][][][2]float64, 0)
		current := make([][][2]float64, 0)
		for _, ring := range geometry.Rings {
			if len(ring) == 0 {
				continue
			}
			current = append(current, ring)
		}
		if len(current) > 0 {
			polygons = append(polygons, current)
		}
		if len(polygons) == 1 {
			return map[string]any{
				"type":        "Polygon",
				"coordinates": polygons[0],
			}
		}
		return map[string]any{
			"type":        "MultiPolygon",
			"coordinates": polygons,
		}
	case len(geometry.Paths) > 0:
		return map[string]any{
			"type":        "MultiLineString",
			"coordinates": geometry.Paths,
		}
	case geometry.X != nil && geometry.Y != nil:
		return map[string]any{
			"type":        "Point",
			"coordinates": [2]float64{*geometry.X, *geometry.Y},
		}
	default:
		return nil
	}
}

func normalizeAttributes(attributes map[string]any) map[string]any {
	if attributes == nil {
		return map[string]any{}
	}

	normalized := make(map[string]any, len(attributes))
	for key, value := range attributes {
		switch numeric := value.(type) {
		case float64:
			if numeric > 1e11 || numeric < -1e11 {
				normalized[key] = time.UnixMilli(int64(numeric)).UTC().Format(time.RFC3339)
			} else {
				normalized[key] = value
			}
		default:
			normalized[key] = value
		}
	}
	return normalized
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buffer.Bytes())
}
