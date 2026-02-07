package doublegis

type RoutingRequest struct {
	Points      []Point `json:"points"`
	Transport   string  `json:"transport"`
	RouteMode   string  `json:"route_mode"`
	TrafficMode string  `json:"traffic_mode,omitempty"`
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
	Maneuvers []Maneuver `json:"maneuvers"`
}

type Maneuver struct {
	OutcomingPath *OutcomingPath `json:"outcoming_path"`
}

type OutcomingPath struct {
	Geometry []GeometrySegment `json:"geometry"`
}

type GeometrySegment struct {
	Selection string `json:"selection"`
	Color     string `json:"color"`
}
