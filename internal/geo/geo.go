package geo

import (
	"fmt"
	"math"
	"strings"

	"github.com/golang/geo/s2"
)

type Coord struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

type Segment struct {
	Coords      []Coord  `json:"coords"`
	Color       string   `json:"color"`
	Comment     string   `json:"comment,omitempty"`
	StreetNames []string `json:"street_names,omitempty"`
	Distance    int      `json:"distance"`
	Duration    int      `json:"duration"`
}

// ParseWKTLinestring parses a WKT LINESTRING into a slice of Coords.
// WKT format: "LINESTRING(lon1 lat1, lon2 lat2, ...)"
func ParseWKTLinestring(wkt string) ([]Coord, error) {
	wkt = strings.TrimSpace(wkt)
	if !strings.HasPrefix(wkt, "LINESTRING(") || !strings.HasSuffix(wkt, ")") {
		return nil, fmt.Errorf("invalid WKT LINESTRING format")
	}

	inner := wkt[len("LINESTRING(") : len(wkt)-1]
	parts := strings.Split(inner, ",")

	coords := make([]Coord, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		var lon, lat float64
		n, err := fmt.Sscanf(p, "%f %f", &lon, &lat)
		if err != nil || n < 2 {
			return nil, fmt.Errorf("parsing coordinate %q: %w", p, err)
		}
		coords = append(coords, Coord{Lon: lon, Lat: lat})
	}

	return coords, nil
}

// FindClosestPoint finds the closest point on the polyline (given as WKT)
// to the target coordinate using the s2 geometry library.
func FindClosestPoint(wktLinestring string, target Coord) (Coord, error) {
	coords, err := ParseWKTLinestring(wktLinestring)
	if err != nil {
		return Coord{}, fmt.Errorf("parsing linestring: %w", err)
	}

	if len(coords) < 2 {
		return Coord{}, fmt.Errorf("linestring must have at least 2 points")
	}

	latlngs := make([]s2.LatLng, len(coords))
	for i, c := range coords {
		latlngs[i] = s2.LatLngFromDegrees(c.Lat, c.Lon)
	}

	polyline := s2.PolylineFromLatLngs(latlngs)

	targetPoint := s2.PointFromLatLng(s2.LatLngFromDegrees(target.Lat, target.Lon))

	closestPoint, _ := polyline.Project(targetPoint)

	closestLatLng := s2.LatLngFromPoint(closestPoint)

	return Coord{
		Lat: closestLatLng.Lat.Degrees(),
		Lon: closestLatLng.Lng.Degrees(),
	}, nil
}

// HaversineSum returns the great-circle distance in meters along the polyline
// formed by consecutive coords. Fewer than 2 coords yields 0.
func HaversineSum(coords []Coord) int {
	const earthRadiusM = 6371000.0
	var total float64
	for i := 1; i < len(coords); i++ {
		lat1 := coords[i-1].Lat * math.Pi / 180
		lat2 := coords[i].Lat * math.Pi / 180
		dLat := (coords[i].Lat - coords[i-1].Lat) * math.Pi / 180
		dLon := (coords[i].Lon - coords[i-1].Lon) * math.Pi / 180
		a := math.Sin(dLat/2)*math.Sin(dLat/2) +
			math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
		total += earthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	}
	return int(math.Round(total))
}
