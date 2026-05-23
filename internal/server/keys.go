package server

import (
	"errors"
	"os"
	"strings"
)

// parseAPIKeys reads 2GIS API keys from the environment.
//
// Precedence:
//  1. DOUBLE_GIS_API_KEYS (comma-separated, whitespace and blank entries
//     discarded) — wins if it contains at least one non-empty entry.
//  2. DOUBLE_GIS_API_KEY (legacy single-key var) — used only when the
//     multi-key var is empty.
//
// Returns an error when both vars are empty or contain no usable values.
func parseAPIKeys() ([]string, error) {
	if multi := os.Getenv("DOUBLE_GIS_API_KEYS"); multi != "" {
		var keys []string
		for _, k := range strings.Split(multi, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			return keys, nil
		}
	}
	if single := strings.TrimSpace(os.Getenv("DOUBLE_GIS_API_KEY")); single != "" {
		return []string{single}, nil
	}
	return nil, errors.New("no 2gis api keys configured (set DOUBLE_GIS_API_KEYS or DOUBLE_GIS_API_KEY)")
}
