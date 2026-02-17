package index

import (
	"bytes"
	"fmt"
	"testing"

	cindex "veloria/internal/codesearch/index"
	cregexp "veloria/internal/codesearch/regexp"
)

// --- Benchmark: CompileSearch ---

func BenchmarkCompileSearch(b *testing.B) {
	cases := []struct {
		name string
		pat  string
		opt  SearchOptions
	}{
		{"Literal", "function", SearchOptions{LiteralSearch: true}},
		{"Regex_Simple", `function\s+\w+`, SearchOptions{}},
		{"Regex_CaseInsensitive", `add_action`, SearchOptions{IgnoreCase: true}},
		{"Regex_Complex", `\$[a-z_]+\s*=\s*get_option`, SearchOptions{}},
		{"Regex_Alternation", `add_action|add_filter|apply_filters`, SearchOptions{}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, err := CompileSearch(tc.pat, &tc.opt)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// --- Benchmark: SearchCompiled (main benchmark) ---

func BenchmarkSearchCompiled(b *testing.B) {
	idx := buildPluginIndex(b, false)
	logIndexStats(b, idx)

	patterns := []struct {
		name string
		pat  string
		opt  SearchOptions
	}{
		{"Literal", "function", SearchOptions{LiteralSearch: true, MaxResults: 1000}},
		{"Literal_Rare", "WC_Install", SearchOptions{LiteralSearch: true, MaxResults: 1000}},
		{"Regex_Simple", `function\s+\w+`, SearchOptions{MaxResults: 1000}},
		{"Regex_CaseInsensitive", `add_action`, SearchOptions{IgnoreCase: true, MaxResults: 1000}},
		{"Regex_Complex", `\$[a-z_]+\s*=\s*get_option`, SearchOptions{MaxResults: 1000}},
		{"Regex_Alternation", `add_action|add_filter|apply_filters`, SearchOptions{MaxResults: 1000}},
		{"Regex_DotStar", `function.*init`, SearchOptions{MaxResults: 1000}},
		{"FileFilter_PHP", "function", SearchOptions{LiteralSearch: true, MaxResults: 1000, FileRegexp: `\.php$`}},
		{"FileFilter_JS", "function", SearchOptions{LiteralSearch: true, MaxResults: 1000, FileRegexp: `\.js$`}},
	}

	for _, pat := range patterns {
		b.Run(pat.name, func(b *testing.B) {
			cs, err := CompileSearch(pat.pat, &pat.opt)
			if err != nil {
				b.Fatal(err)
			}

			// Warm the DFA pool
			idx.SearchCompiled(cs)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				resp, err := idx.SearchCompiled(cs)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(countLineMatches(resp)), "matches/op")
				b.ReportMetric(float64(resp.FilesOpened), "files_opened/op")
			}
		})
	}
}

// --- Benchmark: GrepFile ---

func BenchmarkGrepFile(b *testing.B) {
	type compressionMode struct {
		name     string
		compress bool
	}

	modes := []compressionMode{
		{"Uncompressed", false},
		{"Compressed", true},
	}

	patterns := []struct {
		name string
		pat  string
	}{
		{"Literal_ManyMatches", `(?m)function`},
		{"Literal_FewMatches", `(?m)WC_Install`},
		{"Regex_Simple", `(?m)function\s+\w+`},
		{"Regex_Complex", `(?m)\$[a-z_]+\s*=\s*get_option`},
	}

	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			idx := buildPluginIndex(b, mode.compress)
			srcFile := largestSourceFile(b, idx)
			fileSize := sourceFileSize(b, srcFile)
			b.Logf("file size: %s", formatBytes(fileSize))

			for _, pat := range patterns {
				b.Run(pat.name, func(b *testing.B) {
					re, err := cregexp.Compile(pat.pat)
					if err != nil {
						b.Fatal(err)
					}

					b.SetBytes(fileSize)
					b.ReportAllocs()
					b.ResetTimer()

					for b.Loop() {
						_, err := grepFile(srcFile, re, 0, 1000)
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// --- Benchmark: GrepFile with Context Lines ---

func BenchmarkGrepFileContext(b *testing.B) {
	idx := buildPluginIndex(b, false)
	srcFile := largestSourceFile(b, idx)
	fileSize := sourceFileSize(b, srcFile)

	re, err := cregexp.Compile(`(?m)function`)
	if err != nil {
		b.Fatal(err)
	}

	for _, ctx := range []int{0, 3, 10} {
		b.Run(fmt.Sprintf("Context_%d", ctx), func(b *testing.B) {
			b.SetBytes(fileSize)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				_, err := grepFile(srcFile, re, ctx, 1000)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// --- Benchmark: PostingQuery ---

func BenchmarkPostingQuery(b *testing.B) {
	idx := buildPluginIndex(b, false)
	logIndexStats(b, idx)

	patterns := []struct {
		name string
		pat  string
	}{
		{"Literal", `(?m)function`},
		{"Regex_Simple", `(?m)function\s+\w+`},
		{"Regex_Alternation", `(?m)add_action|add_filter|apply_filters`},
	}

	for _, pat := range patterns {
		b.Run(pat.name, func(b *testing.B) {
			re, err := cregexp.Compile(pat.pat)
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				idx.RLock()
				_ = idx.idx.PostingQuery(cindex.RegexpQuery(re.Syntax))
				idx.RUnlock()
			}
		})
	}
}

// --- Benchmark: ExtractContext ---

func BenchmarkExtractBeforeContext(b *testing.B) {
	var buf bytes.Buffer
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&buf, "line %d: some content here for context extraction benchmark\n", i)
	}
	data := buf.Bytes()

	lineStart := 0
	for i := 0; i < 500; i++ {
		nl := bytes.IndexByte(data[lineStart:], '\n')
		if nl < 0 {
			break
		}
		lineStart += nl + 1
	}

	for _, n := range []int{0, 3, 10} {
		b.Run(fmt.Sprintf("%d_lines", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = extractBeforeContext(data, lineStart, n)
			}
		})
	}
}

func BenchmarkExtractAfterContext(b *testing.B) {
	var buf bytes.Buffer
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&buf, "line %d: some content here for context extraction benchmark\n", i)
	}
	data := buf.Bytes()

	lineEnd := 0
	for i := 0; i < 500; i++ {
		nl := bytes.IndexByte(data[lineEnd:], '\n')
		if nl < 0 {
			break
		}
		lineEnd += nl + 1
	}

	for _, n := range []int{0, 3, 10} {
		b.Run(fmt.Sprintf("%d_lines", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = extractAfterContext(data, lineEnd, n)
			}
		})
	}
}

// --- Benchmark: ReadSourceFile (gzip decompression overhead) ---

func BenchmarkReadSourceFile(b *testing.B) {
	b.Run("Uncompressed", func(b *testing.B) {
		idx := buildPluginIndex(b, false)
		srcFile := largestSourceFile(b, idx)

		data, err := readSourceFile(srcFile)
		if err != nil {
			b.Fatal(err)
		}
		b.Logf("file size: %s", formatBytes(int64(len(data))))
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, err := readSourceFile(srcFile)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Compressed", func(b *testing.B) {
		idx := buildPluginIndex(b, true)
		srcFile := largestSourceFile(b, idx)

		data, err := readSourceFile(srcFile)
		if err != nil {
			b.Fatal(err)
		}
		b.Logf("file size: %s", formatBytes(int64(len(data))))
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, err := readSourceFile(srcFile)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// countLineMatches counts total line matches in a SearchResponse.
func countLineMatches(resp *SearchResponse) int {
	total := 0
	for _, fm := range resp.Matches {
		total += len(fm.Matches)
	}
	return total
}
