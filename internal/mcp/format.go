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

	b.WriteString("\nResults by extension:\n")
	for _, ext := range resp.Extensions {
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
