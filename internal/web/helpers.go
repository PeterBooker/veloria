package web

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
)

var gzipMagicHeader = [2]byte{0x1f, 0x8b}

// BuildSearchSummary builds a SearchSummary from a Search record.
// Match counts are read directly from the DB columns populated at search completion.
func BuildSearchSummary(s searchmodel.Search) SearchSummary {
	summary := SearchSummary{Search: s}
	if s.Status == searchmodel.StatusCompleted && s.TotalMatches != nil {
		summary.MatchCount = *s.TotalMatches
		summary.MatchesKnown = true
	}
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
	percent := 0.0
	if total > 0 {
		// Truncate to 2 decimal places so we never round up to 100%.
		percent = math.Floor(float64(indexed)*10000/float64(total)) / 100
	}

	return RepoSummary{
		Repo:           repo,
		Title:          title,
		Total:          total,
		Indexed:        indexed,
		IndexedPercent: percent,
	}
}

const maxChartPoints = 500

// BuildChartData serializes values to a JSON array for client-side charting.
// If values exceed maxChartPoints, it downsamples by picking evenly spaced points.
func BuildChartData(values []int64) ChartData {
	if len(values) == 0 {
		return ChartData{}
	}

	var max int64
	for _, v := range values {
		if v > max {
			max = v
		}
	}

	if len(values) > maxChartPoints {
		values = downsample(values, maxChartPoints)
	}

	b, _ := json.Marshal(values)
	return ChartData{
		JSON: string(b),
		Max:  max,
	}
}

// downsample picks n evenly spaced points from values, always including the first and last.
func downsample(values []int64, n int) []int64 {
	result := make([]int64, n)
	last := len(values) - 1
	for i := range n {
		result[i] = values[i*last/(n-1)]
	}
	return result
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

// ParseTags parses a JSONB tags column into a map.
func ParseTags(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var tags map[string]string
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil
	}
	return tags
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
// It transparently handles gzip-compressed source files.
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

	// Detect gzip by peeking at the first two bytes.
	var r io.Reader = file
	var header [2]byte
	n, _ := io.ReadFull(file, header[:])
	isGzip := n == len(header) && bytes.Equal(header[:], gzipMagicHeader[:])
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		return nil, serr
	}
	if isGzip {
		gz, gerr := gzip.NewReader(file)
		if gerr != nil {
			return nil, gerr
		}
		defer func() {
			if cerr := gz.Close(); err == nil && cerr != nil {
				err = cerr
			}
		}()
		r = gz
	}

	start := max(lineNumber-radius, 1)
	end := lineNumber + radius

	scanner := bufio.NewScanner(r)
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
