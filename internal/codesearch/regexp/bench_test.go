package regexp

import (
	"fmt"
	"strings"
	"testing"
)

// generateTextBuffer creates a buffer of synthetic PHP-like text of approximately the given size.
func generateTextBuffer(size int) []byte {
	lines := []string{
		"function veloria_init() {",
		"    add_action('init', 'veloria_setup');",
		"    $option = get_option('veloria_setting');",
		"    apply_filters('veloria_filter', $value);",
		"    if ( is_admin() && current_user_can('manage_options') ) {",
		"        update_option('veloria_opt', $new_value);",
		"    }",
		"}",
		"class Veloria_Widget extends WP_Widget {",
		"    public function widget($args, $instance) {",
		"        echo $args['before_widget'];",
		"    }",
		"}",
		"",
	}

	var b strings.Builder
	b.Grow(size)
	for b.Len() < size {
		for _, line := range lines {
			b.WriteString(line)
			b.WriteByte('\n')
			if b.Len() >= size {
				break
			}
		}
	}
	return []byte(b.String()[:size])
}

func BenchmarkDFAMatch(b *testing.B) {
	type bufSize struct {
		name string
		size int
	}

	sizes := []bufSize{
		{"1KB", 1024},
		{"100KB", 100 * 1024},
	}

	patterns := []struct {
		name string
		pat  string
	}{
		{"Literal", "(?m)function"},
		{"Regex_Simple", `(?m)function\s+\w+`},
		{"Regex_CaseInsensitive", "(?i)(?m)add_action"},
		{"Regex_Complex", `(?m)\$[a-z_]+\s*=\s*get_option`},
		{"No_Match", "(?m)ZZZZNOTFOUND"},
	}

	for _, sz := range sizes {
		buf := generateTextBuffer(sz.size)

		b.Run(sz.name, func(b *testing.B) {
			for _, pat := range patterns {
				b.Run(pat.name, func(b *testing.B) {
					re, err := Compile(pat.pat)
					if err != nil {
						b.Fatal(err)
					}

					b.SetBytes(int64(len(buf)))
					b.ReportAllocs()
					b.ResetTimer()

					for b.Loop() {
						re.Match(buf, true, true)
					}
				})
			}
		})
	}
}

func BenchmarkDFACompile(b *testing.B) {
	patterns := []struct {
		name string
		pat  string
	}{
		{"Literal", "(?m)function"},
		{"Simple", `(?m)function\s+\w+`},
		{"CaseInsensitive", "(?i)(?m)add_action"},
		{"Complex", `(?m)\$[a-z_]+\s*=\s*get_option`},
		{"Alternation", `(?m)add_action|add_filter|apply_filters`},
	}

	for _, pat := range patterns {
		b.Run(pat.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, err := Compile(pat.pat)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDFAMatchIterative(b *testing.B) {
	// Benchmark finding ALL matches in a buffer by iterating,
	// similar to how grepFile works
	buf := generateTextBuffer(100 * 1024)

	patterns := []struct {
		name string
		pat  string
	}{
		{"Literal_ManyMatches", "(?m)function"},
		{"Literal_FewMatches", "(?m)WP_Widget"},
		{"Regex_Simple", `(?m)function\s+\w+`},
	}

	for _, pat := range patterns {
		b.Run(pat.name, func(b *testing.B) {
			re, err := Compile(pat.pat)
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(int64(len(buf)))
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				chunkStart := 0
				end := len(buf)
				matches := 0
				beginText := true
				for chunkStart < end {
					m := re.Match(buf[chunkStart:end], beginText, true) + chunkStart
					beginText = false
					if m < chunkStart {
						break
					}
					matches++
					// Advance past matched line
					lineEnd := m + 1
					if lineEnd > end {
						lineEnd = end
					}
					chunkStart = lineEnd
				}
				b.ReportMetric(float64(matches), "matches/op")
			}
		})
	}
}

func BenchmarkDFAMatchVaryingPatternLength(b *testing.B) {
	buf := generateTextBuffer(100 * 1024)

	// Test how pattern length affects DFA performance
	for _, length := range []int{3, 5, 10, 20} {
		pat := fmt.Sprintf("(?m)%s", strings.Repeat("[a-z]", length))
		b.Run(fmt.Sprintf("CharClass_x%d", length), func(b *testing.B) {
			re, err := Compile(pat)
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(int64(len(buf)))
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				re.Match(buf, true, true)
			}
		})
	}
}
