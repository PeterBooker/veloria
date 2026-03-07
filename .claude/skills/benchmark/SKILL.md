---
name: benchmark
description: Run Go benchmarks and compare results to detect performance regressions. Use before and after performance-related changes.
disable-model-invocation: true
argument-hint: "package-path"
---

# Performance Benchmarking

Run Go benchmarks to measure and compare performance. Flags regressions exceeding 10%.

## Usage

- `/benchmark` - Run benchmarks on all packages
- `/benchmark ./internal/index/...` - Run benchmarks on specific package
- `/benchmark -compare ./...` - Compare against saved baseline

## Steps

1. **Identify benchmark targets**
   - If `$ARGUMENTS` specifies a package, use that
   - Otherwise, find packages with `*_test.go` files containing `Benchmark` functions
   - Focus on performance-critical packages: `internal/index/`, `internal/repo/`, `internal/api/`

2. **Run benchmarks**
   ```bash
   go test -bench=. -benchmem -count=5 $ARGUMENTS 2>&1
   ```
   - Use `-count=5` for statistical reliability
   - Use `-benchmem` to track allocations

4. **Parse and analyze results**
   For each benchmark, extract:
   - `ns/op` - Nanoseconds per operation
   - `B/op` - Bytes allocated per operation
   - `allocs/op` - Allocations per operation

5. **Check for baseline comparison**
   - Look for `.benchmark-baseline` file in project root
   - If `-compare` flag used and baseline exists, compare results

6. **Report findings**
   Format results as a table:
   ```
   | Benchmark | ns/op | B/op | allocs/op | Change |
   |-----------|-------|------|-----------|--------|
   ```

   Flag any metric that regressed more than 10% with a warning.

7. **Offer to save baseline**
   If no baseline exists or results improved, offer to save current results:
   ```bash
   go test -bench=. -benchmem -count=5 ./... > .benchmark-baseline
   ```

## Performance-Critical Packages

Pay special attention to:
- `internal/index/` - Trigram search (hot path)
- `internal/repo/` - Repository access patterns
- `internal/api/search/` - Search request handling
- `internal/storage/` - S3 result compression

## Example Output

```
Benchmark Results for ./internal/index/...

| Benchmark           | ns/op    | B/op  | allocs/op |
|---------------------|----------|-------|-----------|
| BenchmarkSearch-8   | 1234567  | 4096  | 12        |
| BenchmarkOpen-8     | 567890   | 8192  | 24        |

No regressions detected (threshold: 10%)
```
