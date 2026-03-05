package repo

import (
	"strings"
	"time"
)

// lastUpdatedFormats lists time formats commonly seen in WordPress API responses,
// ordered from most specific to least specific.
var lastUpdatedFormats = []string{
	"2006-01-02 3:04pm MST",     // e.g. "2024-12-15 2:30pm GMT"
	"2006-01-02 15:04:05",       // e.g. "2024-12-15 14:30:05"
	time.RFC3339,                // e.g. "2024-12-15T14:30:05Z"
	"2006-01-02T15:04:05-07:00", // ISO 8601 with offset
	"2006-01-02T15:04:05",       // ISO 8601 without zone
	"2006-01-02",                // date only
}

// parseLastUpdated tries a cascade of time formats and returns the parsed
// time in UTC on success.
func parseLastUpdated(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	for _, layout := range lastUpdatedFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
