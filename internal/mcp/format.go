package mcp

import (
	"fmt"
	"strings"
)

// FormatSearchSummary formats a search response as a human-readable summary.
// This is the default view returned on the first call (offset=0).
// When searchID is non-empty it is included so clients can paginate without re-searching.
func FormatSearchSummary(resp *SearchResponse, searchID, query, repo string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Search: %q in %s\n", query, repo)
	fmt.Fprintf(&b, "%d matches across %d %s\n", resp.TotalMatches, resp.TotalExtensions, repo)

	if searchID != "" {
		fmt.Fprintf(&b, "search_id: %s\n", searchID)
	}

	if len(resp.Extensions) == 0 {
		return b.String()
	}

	const maxSummaryExtensions = 50

	b.WriteString("\nResults by extension:\n")
	for i, ext := range resp.Extensions {
		if i >= maxSummaryExtensions {
			fmt.Fprintf(&b, "  ... and %d more extensions\n", len(resp.Extensions)-maxSummaryExtensions)
			break
		}
		if ext.ActiveInstalls > 0 {
			fmt.Fprintf(&b, "  %s (%s) v%s — %d matches, %s active installs\n",
				ext.Name, ext.Slug, ext.Version, ext.TotalMatches, formatNumber(ext.ActiveInstalls))
		} else {
			fmt.Fprintf(&b, "  %s (%s) v%s — %d matches\n",
				ext.Name, ext.Slug, ext.Version, ext.TotalMatches)
		}
	}

	b.WriteString("\nUse search_id with offset/limit to paginate through detailed match results.")

	return b.String()
}

// FormatSearchMatches formats paginated match details as text.
func FormatSearchMatches(resp *SearchResponse, offset, limit int) string {
	var b strings.Builder

	// Flatten all matches across extensions for pagination.
	type flatMatch struct {
		slug string
		name string
		MatchDetail
	}

	var all []flatMatch
	for _, ext := range resp.Extensions {
		for _, m := range ext.Matches {
			all = append(all, flatMatch{
				slug:        ext.Slug,
				name:        ext.Name,
				MatchDetail: m,
			})
		}
	}

	total := len(all)
	if offset >= total {
		fmt.Fprintf(&b, "No matches at offset %d (total: %d)\n", offset, total)
		return b.String()
	}

	end := offset + limit
	if end > total {
		end = total
	}

	page := all[offset:end]

	fmt.Fprintf(&b, "Matches %d–%d of %d\n\n", offset+1, end, total)

	for _, m := range page {
		fmt.Fprintf(&b, "— %s (%s) %s:%d\n", m.name, m.slug, m.File, m.Line)

		for _, line := range m.Before {
			fmt.Fprintf(&b, "  | %s\n", line)
		}
		fmt.Fprintf(&b, "  > %s\n", m.Content)
		for _, line := range m.After {
			fmt.Fprintf(&b, "  | %s\n", line)
		}

		b.WriteString("\n")
	}

	if end < total {
		fmt.Fprintf(&b, "More results available. Use offset=%d to see next page.", end)
	}

	return b.String()
}

// FormatExtensionList formats a list of extensions as text.
func FormatExtensionList(resp *ListResponse, repo string, offset int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s: %d total\n", repo, resp.Total)

	if len(resp.Extensions) == 0 {
		b.WriteString("No extensions found.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "Showing %d–%d:\n\n", offset+1, offset+len(resp.Extensions))

	for _, ext := range resp.Extensions {
		status := "indexed"
		if !ext.Indexed {
			status = "not indexed"
		}
		fmt.Fprintf(&b, "  %s — %s v%s [%s]\n", ext.Slug, ext.Name, ext.Version, status)
	}

	end := offset + len(resp.Extensions)
	if end < resp.Total {
		fmt.Fprintf(&b, "\nMore results available. Use offset=%d to see next page.", end)
	}

	return b.String()
}

// FormatExtensionDetails formats extension metadata as text.
func FormatExtensionDetails(d *ExtensionDetails) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s (%s) v%s\n", d.Name, d.Slug, d.Version)

	if d.ShortDescription != "" {
		fmt.Fprintf(&b, "%s\n", d.ShortDescription)
	}

	b.WriteString("\n")

	if d.Source != "" {
		fmt.Fprintf(&b, "Source:          %s\n", d.Source)
	}
	if d.ActiveInstalls > 0 {
		fmt.Fprintf(&b, "Active installs: %s\n", formatNumber(d.ActiveInstalls))
	}
	if d.Downloaded > 0 {
		fmt.Fprintf(&b, "Downloads:       %s\n", formatNumber(d.Downloaded))
	}
	if d.Rating > 0 {
		fmt.Fprintf(&b, "Rating:          %d/100\n", d.Rating)
	}
	if d.Requires != "" {
		fmt.Fprintf(&b, "Requires WP:     %s\n", d.Requires)
	}
	if d.Tested != "" {
		fmt.Fprintf(&b, "Tested up to:    %s\n", d.Tested)
	}
	if d.RequiresPHP != "" {
		fmt.Fprintf(&b, "Requires PHP:    %s\n", d.RequiresPHP)
	}

	status := "indexed"
	if !d.Indexed {
		status = "not indexed"
	}
	fmt.Fprintf(&b, "Index status:    %s\n", status)

	if d.Indexed && d.FileCount > 0 {
		fmt.Fprintf(&b, "Files:           %s\n", formatNumber(d.FileCount))
		fmt.Fprintf(&b, "Total size:      %s\n", formatBytes(d.TotalSize))
	}

	return b.String()
}

// FormatRepoStats formats repository statistics as text.
func FormatRepoStats(stats []RepoStats) string {
	var b strings.Builder

	b.WriteString("Repository Statistics\n\n")

	for _, s := range stats {
		pct := 0.0
		if s.Total > 0 {
			pct = float64(s.Indexed) / float64(s.Total) * 100
		}
		fmt.Fprintf(&b, "  %-8s  %s total, %s indexed (%.1f%%)\n",
			s.Repo, formatNumber(s.Total), formatNumber(s.Indexed), pct)
	}

	return b.String()
}

// FormatFileList formats an extension's file listing as text.
func FormatFileList(resp *ListFilesResponse) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s/%s: %d files\n\n", resp.Repo, resp.Slug, resp.Total)

	for _, f := range resp.Files {
		fmt.Fprintf(&b, "  %-8s %s\n", formatBytes(f.Size), f.Path)
	}

	return b.String()
}

// FormatReadFile formats file contents with metadata header.
func FormatReadFile(resp *ReadFileResponse) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s/%s — %s (%d lines total)\n", resp.Repo, resp.Slug, resp.Path, resp.TotalLines)

	if resp.TotalLines > 0 {
		fmt.Fprintf(&b, "Showing lines %d–%d\n", resp.StartLine, resp.EndLine)
	}

	b.WriteString("\n")
	b.WriteString(resp.Content)

	if resp.EndLine < resp.TotalLines {
		fmt.Fprintf(&b, "\nMore lines available. Use start_line=%d to continue reading.", resp.EndLine+1)
	}

	return b.String()
}

// FormatGrepFile formats grep results as human-readable text.
func FormatGrepFile(resp *GrepFileResponse) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s/%s — grep %q\n", resp.Repo, resp.Slug, resp.Query)
	fmt.Fprintf(&b, "%d matches across %d files\n", resp.TotalMatches, len(resp.Files))

	if len(resp.Files) == 0 {
		return b.String()
	}

	b.WriteString("\n")

	for _, f := range resp.Files {
		fmt.Fprintf(&b, "— %s (%d matches)\n", f.Path, len(f.Matches))
		for _, m := range f.Matches {
			for _, line := range m.Before {
				fmt.Fprintf(&b, "  | %s\n", line)
			}
			fmt.Fprintf(&b, "  > %d: %s\n", m.Line, m.Content)
			for _, line := range m.After {
				fmt.Fprintf(&b, "  | %s\n", line)
			}
			b.WriteString("\n")
		}
	}

	if resp.TotalMatches >= maxGrepMatches {
		b.WriteString("Results capped at 1,000 matches. Narrow your search with file_match or a more specific query.\n")
	}

	return b.String()
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
	)
	switch {
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatNumber formats an integer with comma separators.
func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	s := fmt.Sprintf("%d", n)
	var result strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(ch)
	}
	return result.String()
}
