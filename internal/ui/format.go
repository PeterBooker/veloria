package ui

import (
	"fmt"
	"html/template"
	"time"
)

// SafeJS marks a string as safe JavaScript for embedding in templates.
func SafeJS(s string) template.JS {
	return template.JS(s) // #nosec G203 -- server-generated JSON
}

// FormatDuration formats milliseconds into a human-readable duration string.
func FormatDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", float64(d)/float64(time.Second))
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()+0.5))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", mins, secs)
}

// FormatCompact formats an integer into a compact notation (e.g. 1.5K, 2M).
func FormatCompact(value int) string {
	if value < 0 {
		return fmt.Sprintf("-%s", formatCompactNumber(-value))
	}
	return formatCompactNumber(value)
}

// FormatBytes formats byte counts into human-readable form (e.g. 1.5 MB).
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// FormatNumber formats an integer with comma separators (e.g. 1,234,567).
func FormatNumber(n int) string {
	return formatNumberWithCommas(n)
}

// TimeAgo returns a relative timestamp for today, or a date string for earlier.
func TimeAgo(t time.Time) string {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if t.Before(today) {
		return t.Format("2006-01-02")
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	default:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
}

// FormatInstalls formats install counts into compact notation.
func FormatInstalls(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.0fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	default:
		return formatNumberWithCommas(n)
	}
}

// Elapsed returns a human-readable duration since the given start time.
func Elapsed(start time.Time) string {
	d := time.Since(start)
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// Percentage calculates the integer percentage of a/b.
func Percentage(a, b int) int {
	if b == 0 {
		return 0
	}
	return a * 100 / b
}

func formatCompactNumber(value int) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.0fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.0fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func formatNumberWithCommas(n int) string {
	if n < 0 {
		return "-" + formatNumberWithCommas(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatNumberWithCommas(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}
