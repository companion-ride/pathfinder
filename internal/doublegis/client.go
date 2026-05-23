package doublegis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const defaultBaseURL = "https://routing.api.2gis.com/routing/7.0.0/global"

var linestringRe = regexp.MustCompile(`LINESTRING\s*\(([^)]+)\)`)

type RouteSegment struct {
	WKT         string
	Color       string
	Comment     string
	StreetNames []string
	Distance    int
	Duration    int
}

type RouteInfo struct {
	TotalDistance    int
	TotalDuration   int
	UITotalDistance  string
	UITotalDuration string
	Waypoints       []Waypoint
}

type RouteParams struct {
	RouteMode   string
	TrafficMode string
	Locale      string
	Timeout     time.Duration
}

type Client struct {
	keys       *keyring
	baseURL    string
	getParams  func() RouteParams
	httpClient *http.Client
}

func NewClient(apiKeys []string, getParams func() RouteParams) *Client {
	base := os.Getenv("DOUBLEGIS_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{
		keys:      newKeyring(apiKeys),
		baseURL:   base,
		getParams: getParams,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) GetRoute(ctx context.Context, startLat, startLon, endLat, endLon float64) ([]RouteSegment, string, *RouteInfo, []byte, error) {
	p := c.getParams()
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	reqBody := RoutingRequest{
		Points: []Point{
			{Type: "stop", Lat: startLat, Lon: startLon},
			{Type: "stop", Lat: endLat, Lon: endLon},
		},
		Transport:   "driving",
		RouteMode:   p.RouteMode,
		TrafficMode: p.TrafficMode,
		Output:      "detailed",
		Locale:      p.Locale,
	}

	routingResp, rawJSON, err := c.doRoutingRequest(ctx, reqBody)
	if err != nil {
		return nil, "", nil, nil, err
	}

	if len(routingResp.Result) == 0 {
		return nil, "", nil, nil, fmt.Errorf("no routes found")
	}

	route := routingResp.Result[0]

	if len(route.Waypoints) < 2 {
		return nil, "", nil, nil, fmt.Errorf("route has %d waypoints, need at least 2", len(route.Waypoints))
	}

	segments, mergedWKT := extractSegments(route)
	if mergedWKT == "" {
		return nil, "", nil, nil, fmt.Errorf("no geometry in route response")
	}

	info := &RouteInfo{
		TotalDistance:    route.TotalDistance,
		TotalDuration:   route.TotalDuration,
		UITotalDistance:  route.UITotalDistance.String(),
		UITotalDuration: route.UITotalDuration,
		Waypoints:       route.Waypoints,
	}

	return segments, mergedWKT, info, rawJSON, nil
}

func (c *Client) GetWalkingRoute(ctx context.Context, fromLat, fromLon, toLat, toLon float64) (string, *RouteInfo, []byte, error) {
	p := c.getParams()
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	reqBody := RoutingRequest{
		Points: []Point{
			{Type: "stop", Lat: fromLat, Lon: fromLon},
			{Type: "stop", Lat: toLat, Lon: toLon},
		},
		Transport: "pedestrian",
		RouteMode: "fastest",
		Output:    "detailed",
		Locale:    p.Locale,
	}

	routingResp, rawJSON, err := c.doRoutingRequest(ctx, reqBody)
	if err != nil {
		return "", nil, nil, err
	}

	if len(routingResp.Result) == 0 {
		return "", nil, nil, fmt.Errorf("no walking routes found")
	}

	route := routingResp.Result[0]

	if len(route.Waypoints) < 2 {
		return "", nil, nil, fmt.Errorf("walking route has %d waypoints, need at least 2", len(route.Waypoints))
	}

	wkt := mergeGeometry(route)
	if wkt == "" {
		return "", nil, nil, fmt.Errorf("no geometry in walking route response")
	}

	info := &RouteInfo{
		TotalDistance:    route.TotalDistance,
		TotalDuration:   route.TotalDuration,
		UITotalDistance:  route.UITotalDistance.String(),
		UITotalDuration: route.UITotalDuration,
		Waypoints:       route.Waypoints,
	}

	return wkt, info, rawJSON, nil
}

func (c *Client) doRoutingRequest(ctx context.Context, reqBody RoutingRequest) (*RoutingResponse, []byte, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling request: %w", err)
	}

	for attempt := 0; attempt < c.keys.size(); attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		key, keyIdx, ok := c.keys.acquire()
		if !ok {
			return nil, nil, fmt.Errorf("all 2gis api keys are rate-limited")
		}

		routingResp, body, retry, err := c.sendOnce(ctx, key, keyIdx, jsonBody)
		if retry {
			continue
		}
		return routingResp, body, err
	}

	slog.Error("all 2gis api keys exhausted")
	return nil, nil, fmt.Errorf("all 2gis api keys are rate-limited")
}

// sendOnce performs a single HTTP request with the given key.
// retry=true means the caller should rotate and try again (current key was 429'd).
func (c *Client) sendOnce(ctx context.Context, key string, keyIdx int, jsonBody []byte) (*RoutingResponse, []byte, bool, error) {
	url := fmt.Sprintf("%s?key=%s", c.baseURL, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, false, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, false, fmt.Errorf("calling 2gis api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		c.keys.markRateLimited(keyIdx, retryAfter)
		slog.Warn("2gis api key rate-limited, rotating",
			"key_index", keyIdx,
			"retry_after", retryAfter,
		)
		return nil, nil, true, nil
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		slog.Error("2GIS API error",
			"status", resp.StatusCode,
			"key_index", keyIdx,
			"body", string(errBody),
		)
		return nil, nil, false, fmt.Errorf("2gis api returned status %d: %s", resp.StatusCode, string(errBody))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, false, fmt.Errorf("reading response body: %w", err)
	}

	var routingResp RoutingResponse
	if err := json.Unmarshal(body, &routingResp); err != nil {
		return nil, nil, false, fmt.Errorf("decoding response: %w", err)
	}

	return &routingResp, body, false, nil
}

func extractSegments(route Route) ([]RouteSegment, string) {
	var segments []RouteSegment
	var allCoords []string

	for _, m := range route.Maneuvers {
		if m.OutcomingPath == nil {
			continue
		}
		for _, seg := range m.OutcomingPath.Geometry {
			matches := linestringRe.FindStringSubmatch(seg.Selection)
			if len(matches) < 2 {
				slog.Warn("skipping segment: geometry regex did not match", "selection", seg.Selection)
				continue
			}

			segCoords := strings.Split(matches[1], ",")
			trimmed := make([]string, len(segCoords))
			for i, c := range segCoords {
				trimmed[i] = strings.TrimSpace(c)
			}
			segWKT := fmt.Sprintf("LINESTRING(%s)", strings.Join(trimmed, ", "))
			segments = append(segments, RouteSegment{
				WKT:         segWKT,
				Color:       seg.Color,
				Comment:     m.Comment,
				StreetNames: m.OutcomingPath.Names,
				Distance:    m.OutcomingPath.Distance,
				Duration:    m.OutcomingPath.Duration,
			})

			for i, c := range trimmed {
				if i == 0 && len(allCoords) > 0 && allCoords[len(allCoords)-1] == c {
					continue
				}
				allCoords = append(allCoords, c)
			}
		}
	}

	if len(allCoords) == 0 {
		return nil, ""
	}

	mergedWKT := fmt.Sprintf("LINESTRING(%s)", strings.Join(allCoords, ", "))
	return segments, mergedWKT
}

func mergeGeometry(route Route) string {
	var allCoords []string

	for _, m := range route.Maneuvers {
		if m.OutcomingPath == nil {
			continue
		}
		for _, seg := range m.OutcomingPath.Geometry {
			matches := linestringRe.FindStringSubmatch(seg.Selection)
			if len(matches) < 2 {
				slog.Warn("skipping segment: geometry regex did not match", "selection", seg.Selection)
				continue
			}
			coords := strings.Split(matches[1], ",")
			for i, c := range coords {
				c = strings.TrimSpace(c)
				if i == 0 && len(allCoords) > 0 && allCoords[len(allCoords)-1] == c {
					continue
				}
				allCoords = append(allCoords, c)
			}
		}
	}

	if len(allCoords) == 0 {
		return ""
	}

	return fmt.Sprintf("LINESTRING(%s)", strings.Join(allCoords, ", "))
}
