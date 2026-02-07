package doublegis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const baseURL = "http://routing.api.2gis.com/routing/7.0.0/global"

var linestringRe = regexp.MustCompile(`LINESTRING\s*\(([^)]+)\)`)

type RouteSegment struct {
	WKT   string
	Color string
}

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) GetRoute(ctx context.Context, startLat, startLon, endLat, endLon float64) ([]RouteSegment, string, error) {
	reqBody := RoutingRequest{
		Points: []Point{
			{Type: "stop", Lat: startLat, Lon: startLon},
			{Type: "stop", Lat: endLat, Lon: endLon},
		},
		Transport:   "driving",
		RouteMode:   "fastest",
		TrafficMode: "jam",
	}

	routingResp, err := c.doRoutingRequest(ctx, reqBody)
	if err != nil {
		return nil, "", err
	}

	if len(routingResp.Result) == 0 {
		return nil, "", fmt.Errorf("no routes found")
	}

	segments, mergedWKT := extractSegments(routingResp.Result[0])
	if mergedWKT == "" {
		return nil, "", fmt.Errorf("no geometry in route response")
	}

	return segments, mergedWKT, nil
}

func (c *Client) GetWalkingRoute(ctx context.Context, fromLat, fromLon, toLat, toLon float64) (string, error) {
	reqBody := RoutingRequest{
		Points: []Point{
			{Type: "stop", Lat: fromLat, Lon: fromLon},
			{Type: "stop", Lat: toLat, Lon: toLon},
		},
		Transport: "pedestrian",
		RouteMode: "fastest",
	}

	routingResp, err := c.doRoutingRequest(ctx, reqBody)
	if err != nil {
		return "", err
	}

	if len(routingResp.Result) == 0 {
		return "", fmt.Errorf("no walking routes found")
	}

	wkt := mergeGeometry(routingResp.Result[0])
	if wkt == "" {
		return "", fmt.Errorf("no geometry in walking route response")
	}

	return wkt, nil
}

func (c *Client) doRoutingRequest(ctx context.Context, reqBody RoutingRequest) (*RoutingResponse, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("%s?key=%s", baseURL, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling 2gis api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("2gis api returned status %d", resp.StatusCode)
	}

	var routingResp RoutingResponse
	if err := json.NewDecoder(resp.Body).Decode(&routingResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &routingResp, nil
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
				continue
			}

			// Build per-segment WKT
			segCoords := strings.Split(matches[1], ",")
			trimmed := make([]string, len(segCoords))
			for i, c := range segCoords {
				trimmed[i] = strings.TrimSpace(c)
			}
			segWKT := fmt.Sprintf("LINESTRING(%s)", strings.Join(trimmed, ", "))
			segments = append(segments, RouteSegment{
				WKT:   segWKT,
				Color: seg.Color,
			})

			// Merge into full route coords
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
