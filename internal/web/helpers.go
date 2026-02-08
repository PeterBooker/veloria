package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/storage"
	typespb "veloria/internal/types"
)

// BuildSearchSummary fetches match count from S3 for a search.
func BuildSearchSummary(ctx context.Context, s3 storage.ResultStorage, s searchmodel.Search) SearchSummary {
	summary := SearchSummary{Search: s}
	if s.Status != searchmodel.StatusCompleted {
		return summary
	}
	if s3 == nil {
		return summary
	}

	var protoResults typespb.SearchResponse
	if err := s3.DownloadResult(ctx, s.ID.String(), &protoResults); err != nil {
		return summary
	}

	results := searchmodel.SearchResponseFromProto(&protoResults)
	summary.MatchCount = CountTotalMatches(results)
	summary.MatchesKnown = true
	return summary
}

// CountTotalMatches counts the total number of matches across all search results.
func CountTotalMatches(results *manager.SearchResponse) int {
	if results == nil {
		return 0
	}

	total := 0
	for _, result := range results.Results {
		total += result.TotalMatches
	}
	return total
}

// BuildRepoSummary creates a RepoSummary with calculated percentage.
func BuildRepoSummary(repo string, title string, total int, indexed int) RepoSummary {
	percent := 0
	if total > 0 {
		percent = int(math.Round(float64(indexed) * 100 / float64(total)))
	}

	return RepoSummary{
		Repo:           repo,
		Title:          title,
		Total:          total,
		Indexed:        indexed,
		IndexedPercent: percent,
	}
}

// BuildLineSeries creates an SVG polyline string from a slice of values.
func BuildLineSeries(values []int64) LineSeries {
	if len(values) == 0 {
		return LineSeries{}
	}

	var max int64
	for _, v := range values {
		if v > max {
			max = v
		}
	}

	const width = 100.0
	const height = 40.0

	step := 0.0
	if len(values) > 1 {
		step = width / float64(len(values)-1)
	}

	var sb strings.Builder
	for i, v := range values {
		x := float64(i) * step
		y := height
		if max > 0 {
			y = height - (float64(v)/float64(max))*height
		}
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(fmt.Sprintf("%.2f,%.2f", x, y))
	}

	return LineSeries{
		Points: sb.String(),
		Max:    max,
	}
}

// ParseLargestFiles parses the JSONB largest_files column and returns at most limit entries.
func ParseLargestFiles(data []byte, limit int) []FileStat {
	if len(data) == 0 {
		return nil
	}

	var files []FileStat
	if err := json.Unmarshal(data, &files); err != nil {
		return nil
	}
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files
}

// SafeJoin safely joins a base directory with a relative path, preventing directory traversal.
func SafeJoin(base string, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(base, clean)
	relPath, err := filepath.Rel(base, full)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("invalid path")
	}
	return full, nil
}

// ReadContextLines reads lines around a target line number from a file.
func ReadContextLines(path string, lineNumber int, radius int) (lines []SearchContextLine, err error) {
	file, err := os.Open(path) // #nosec G304 -- path is validated by SafePath caller
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := file.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	start := lineNumber - radius
	if start < 1 {
		start = 1
	}
	end := lineNumber + radius

	scanner := bufio.NewScanner(file)
	current := 0
	for scanner.Scan() {
		current++
		if current < start {
			continue
		}
		if current > end {
			break
		}
		lines = append(lines, SearchContextLine{
			Number:  current,
			Text:    scanner.Text(),
			IsMatch: current == lineNumber,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
