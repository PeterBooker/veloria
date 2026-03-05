package repo

import (
	"testing"
	"time"
)

func TestParseLastUpdated(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantOK  bool
		wantUTC time.Time
	}{
		{
			name:    "wordpress 12h format",
			raw:     "2024-12-15 2:30pm GMT",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 14, 30, 0, 0, time.UTC),
		},
		{
			name:    "24h format",
			raw:     "2024-12-15 14:30:05",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 14, 30, 5, 0, time.UTC),
		},
		{
			name:    "RFC3339",
			raw:     "2024-12-15T14:30:05Z",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 14, 30, 5, 0, time.UTC),
		},
		{
			name:    "ISO 8601 with offset",
			raw:     "2024-12-15T14:30:05+05:00",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 9, 30, 5, 0, time.UTC),
		},
		{
			name:    "ISO 8601 without zone",
			raw:     "2024-12-15T14:30:05",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 14, 30, 5, 0, time.UTC),
		},
		{
			name:    "date only",
			raw:     "2024-12-15",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "with leading/trailing whitespace",
			raw:     "  2024-12-15  ",
			wantOK:  true,
			wantUTC: time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "empty string",
			raw:    "",
			wantOK: false,
		},
		{
			name:   "garbage",
			raw:    "not-a-date",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLastUpdated(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("parseLastUpdated(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if ok && !got.Equal(tt.wantUTC) {
				t.Errorf("parseLastUpdated(%q) = %v, want %v", tt.raw, got, tt.wantUTC)
			}
		})
	}
}
