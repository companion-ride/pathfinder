package geo

import (
	"math"
	"testing"
)

func TestHaversineSum(t *testing.T) {
	tests := []struct {
		name   string
		coords []Coord
		want   int
		tolM   float64
	}{
		{name: "empty", coords: nil, want: 0, tolM: 0},
		{name: "single point", coords: []Coord{{Lat: 51.10, Lon: 71.40}}, want: 0, tolM: 0},
		{
			name:   "two points ~1.11km apart (0.01 deg lat)",
			coords: []Coord{{Lat: 51.10, Lon: 71.40}, {Lat: 51.11, Lon: 71.40}},
			want:   1112,
			tolM:   3,
		},
		{
			name: "three points sums consecutive legs",
			coords: []Coord{
				{Lat: 51.10, Lon: 71.40},
				{Lat: 51.11, Lon: 71.40},
				{Lat: 51.12, Lon: 71.40},
			},
			want: 2224,
			tolM: 5,
		},
		{
			name:   "duplicate consecutive points contribute zero",
			coords: []Coord{{Lat: 51.10, Lon: 71.40}, {Lat: 51.10, Lon: 71.40}},
			want:   0,
			tolM:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HaversineSum(tt.coords)
			if math.Abs(float64(got-tt.want)) > tt.tolM {
				t.Errorf("HaversineSum = %d, want %d (±%.0f m)", got, tt.want, tt.tolM)
			}
		})
	}
}
