package ui

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, ""},
		{-1, ""},
		{500, "500ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{9999, "10.0s"},
		{10000, "10s"},
		{30000, "30s"},
		{59999, "60s"},
		{60000, "1m 0s"},
		{90000, "1m 30s"},
		{125000, "2m 5s"},
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.ms); got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestFormatCompact(t *testing.T) {
	tests := []struct {
		value int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1K"},
		{1500, "2K"},
		{999_999, "1000K"},
		{1_000_000, "1M"},
		{1_500_000, "2M"},
		{1_000_000_000, "1B"},
		{-1500, "-2K"},
	}
	for _, tt := range tests {
		if got := FormatCompact(tt.value); got != tt.want {
			t.Errorf("FormatCompact(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.bytes); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, tt := range tests {
		if got := FormatNumber(tt.n); got != tt.want {
			t.Errorf("FormatNumber(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatInstalls(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{9999, "9,999"},
		{10_000, "10K"},
		{1_000_000, "1M"},
		{1_000_000_000, "1B"},
	}
	for _, tt := range tests {
		if got := FormatInstalls(tt.n); got != tt.want {
			t.Errorf("FormatInstalls(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestPercentage(t *testing.T) {
	tests := []struct {
		a, b int
		want int
	}{
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{1, 3, 33},
		{0, 0, 0},
	}
	for _, tt := range tests {
		if got := Percentage(tt.a, tt.b); got != tt.want {
			t.Errorf("Percentage(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now", now.Add(-30 * time.Second), "just now"},
		{"1 minute ago", now.Add(-90 * time.Second), "1 minute ago"},
		{"5 minutes ago", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"1 hour ago", now.Add(-90 * time.Minute), "1 hour ago"},
		{"3 hours ago", now.Add(-3 * time.Hour), "3 hours ago"},
	}
	for _, tt := range tests {
		if got := TimeAgo(tt.t); got != tt.want {
			t.Errorf("TimeAgo(%s) = %q, want %q", tt.name, got, tt.want)
		}
	}

	// Yesterday should return a date
	yesterday := now.Add(-25 * time.Hour)
	got := TimeAgo(yesterday)
	if got != yesterday.Format("2006-01-02") {
		t.Errorf("TimeAgo(yesterday) = %q, want date format", got)
	}
}
