package doublegis

type RoutingRequest struct {
	Points      []Point  `json:"points"`
	Transport   string   `json:"transport"`
	RouteMode   string   `json:"route_mode"`
	TrafficMode string `json:"traffic_mode,omitempty"`
	Output      string `json:"output,omitempty"`
	Locale      string `json:"locale,omitempty"`
}

type Point struct {
	Type string  `json:"type"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type RoutingResponse struct {
	Result []Route `json:"result"`
}

type Route struct {
	Maneuvers       []Maneuver      `json:"maneuvers"`
	Waypoints       []Waypoint      `json:"waypoints"`
	TotalDistance    int             `json:"total_distance"`
	TotalDuration   int             `json:"total_duration"`
	UITotalDistance  UIDistanceValue `json:"ui_total_distance"`
	UITotalDuration string          `json:"ui_total_duration"`
}

type Waypoint struct {
	OriginalPoint  WaypointPoint `json:"original_point"`
	ProjectedPoint WaypointPoint `json:"projected_point"`
}

type WaypointPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Maneuver struct {
	Comment       string         `json:"comment"`
	OutcomingPath *OutcomingPath `json:"outcoming_path"`
}

type OutcomingPath struct {
	Distance int               `json:"distance"`
	Duration int               `json:"duration"`
	Names    []string          `json:"names"`
	Geometry []GeometrySegment `json:"geometry"`
}

type GeometrySegment struct {
	Selection string `json:"selection"`
	Color     string `json:"color"`
}

type UIDistanceValue struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

func (d UIDistanceValue) String() string {
	if d.Value == "" {
		return ""
	}
	return d.Value + " " + d.Unit
}
