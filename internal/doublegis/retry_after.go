package doublegis

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultRetryAfter = 60 * time.Second

// parseRetryAfter interprets an HTTP Retry-After header per RFC 7231 §7.1.3.
// Returns defaultRetryAfter for missing, malformed, non-positive, or past-date
// values.
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return defaultRetryAfter
	}

	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return defaultRetryAfter
		}
		return time.Duration(secs) * time.Second
	}

	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return defaultRetryAfter
		}
		return d
	}

	return defaultRetryAfter
}
