package regexp

import (
	"bytes"
	"math/rand/v2"
	stdregexp "regexp"
	"runtime"
	"slices"
	"testing"
)

// variedText returns size bytes of deterministic source-like text in which
// lines rarely repeat. It contains no NUL byte, like every indexed file.
func variedText(size int) []byte {
	const alphabet = "abcdefghijklmnopqrstuvwxyz_$()=;,' "
	rng := rand.New(rand.NewPCG(1, 2))
	buf := make([]byte, 0, size)
	for len(buf) < size {
		for range 20 + rng.IntN(280) {
			buf = append(buf, alphabet[rng.IntN(len(alphabet))])
		}
		buf = append(buf, '\n')
	}
	buf = buf[:size]
	buf[size-1] = '\n'
	return buf
}

// cachedBytes measures the DFA states m retains, independently of the
// matcher's own accounting.
func cachedBytes(m *matcher) int {
	n := len(m.dstate) * dstateBytes
	for enc := range m.dstate {
		n += len(enc)
	}
	return n
}

func heapAlloc() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// A short term of the form "a byte, N arbitrary bytes, a byte the input never
// contains" makes a new DFA state for almost every input byte and never
// matches. The matcher must keep its state cache within maxDFACacheBytes.
func TestDFACacheStaysWithinBudget(t *testing.T) {
	const pattern = `(?i)(?m)e.{200}\x00`
	buf := variedText(32 << 10)

	entryPoints := map[string]func(re *Regexp, chunk []byte, beginText, endText bool) int{
		"Match": func(re *Regexp, chunk []byte, beginText, endText bool) int {
			return re.Match(chunk, beginText, endText)
		},
		"MatchString": func(re *Regexp, chunk []byte, beginText, endText bool) int {
			return re.MatchString(string(chunk), beginText, endText)
		},
	}

	for name, match := range entryPoints {
		t.Run(name, func(t *testing.T) {
			before := heapAlloc()

			re, err := Compile(pattern)
			if err != nil {
				t.Fatal(err)
			}

			// Scan line by line. The DFA returns to its line-start state at
			// every newline, so this visits the same states as one call over
			// the whole buffer while letting the test look at the cache as
			// it grows.
			rest := buf
			for len(rest) > 0 {
				line := rest[:bytes.IndexByte(rest, '\n')+1]
				rest = rest[len(line):]
				scanned := len(buf) - len(rest)

				if end := match(re, line, scanned == len(line), len(rest) == 0); end >= 0 {
					t.Fatalf("unexpected match after %d bytes", scanned)
				}
				if got := cachedBytes(&re.m); got > maxDFACacheBytes {
					t.Fatalf("after %d bytes the matcher retains %d states (%d bytes), budget is %d bytes",
						scanned, len(re.m.dstate), got, maxDFACacheBytes)
				}
				if re.m.cacheBytes != cachedBytes(&re.m) {
					t.Fatalf("after %d bytes the matcher accounts for %d bytes but retains %d",
						scanned, re.m.cacheBytes, cachedBytes(&re.m))
				}
			}

			// The accounting above only sees states still in the cache. Check
			// the heap as well: states dropped by a reset must be collectable.
			after := heapAlloc()
			if after > before && after-before > 2*maxDFACacheBytes {
				t.Fatalf("heap grew by %d bytes, want at most %d", after-before, 2*maxDFACacheBytes)
			}
			runtime.KeepAlive(re)
		})
	}
}

// Dropping the cache mid-scan must not change which lines match. The term
// overflows the budget many times over this input and does match some lines;
// the standard library's regexp, which has no DFA cache, is the reference.
func TestDFACacheResetKeepsMatches(t *testing.T) {
	const term = `e.{120};`
	buf := variedText(32 << 10)

	ref := stdregexp.MustCompile(`(?i)` + term)
	var want []int
	for i, line := range bytes.Split(buf, nl) {
		if ref.Match(line) {
			want = append(want, i+1)
		}
	}
	if len(want) == 0 {
		t.Fatal("test input has no matching line")
	}

	re, err := Compile(`(?i)(?m)` + term)
	if err != nil {
		t.Fatal(err)
	}

	// Find every matching line the way index.grepData does.
	var (
		got       []int
		lineno    = 1
		start     = 0
		beginText = true
		initial   = re.m.start
	)
	for start < len(buf) {
		end := re.Match(buf[start:], beginText, true)
		beginText = false
		if size := cachedBytes(&re.m); size > maxDFACacheBytes {
			t.Fatalf("matcher retains %d bytes of states, budget is %d", size, maxDFACacheBytes)
		}
		if end < 0 {
			break
		}
		end += start
		lineStart := bytes.LastIndexByte(buf[start:end], '\n') + 1 + start
		lineno += countNL(buf[start:lineStart])
		got = append(got, lineno)
		lineno++
		start = min(end+1, len(buf))
	}

	// A reset rebuilds the start state, so an unchanged pointer means the
	// budget was never reached.
	if re.m.start == initial {
		t.Fatal("the cache was never reset; the test no longer exercises the budget")
	}
	if !slices.Equal(got, want) {
		t.Fatalf("matching lines differ from the reference:\n got %v\nwant %v", got, want)
	}
}
