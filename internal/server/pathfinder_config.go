package server

import (
	"sync/atomic"
)

const defaultCustomRouteAvgSpeedMPS = 11.11

type PathfinderConfig struct {
	RouteMode                string  `json:"route_mode"`
	TrafficMode              string  `json:"traffic_mode"`
	Locale                   string  `json:"locale"`
	RequestTimeoutSeconds    int     `json:"request_timeout_seconds"`
	CustomRouteAvgSpeedMPS   float64 `json:"custom_route_avg_speed_mps"`
}

func DefaultPathfinderConfig() *PathfinderConfig {
	return &PathfinderConfig{
		RouteMode:              "fastest",
		TrafficMode:            "jam",
		Locale:                 "kk",
		RequestTimeoutSeconds:  30,
		CustomRouteAvgSpeedMPS: defaultCustomRouteAvgSpeedMPS,
	}
}

type PathfinderConfigLoader struct {
	config atomic.Pointer[PathfinderConfig]
}

func NewPathfinderConfigLoader(initial *PathfinderConfig) *PathfinderConfigLoader {
	l := &PathfinderConfigLoader{}
	l.config.Store(initial)
	return l
}

func (l *PathfinderConfigLoader) Get() *PathfinderConfig {
	return l.config.Load()
}

func (l *PathfinderConfigLoader) Store(c *PathfinderConfig) {
	l.config.Store(c)
}
