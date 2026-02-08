---
name: profile
description: Run CPU and memory profiling with pprof to identify performance hotspots. Use when investigating high resource usage.
disable-model-invocation: true
argument-hint: [cpu|memory|all] [package-path]
allowed-tools: Bash(go test *, go tool pprof *), Read, Glob, Grep, Write
---

# CPU and Memory Profiling

Profile Go code to identify CPU hotspots and memory allocators using pprof.

## Usage

- `/profile cpu ./internal/index/` - CPU profiling on index package
- `/profile memory ./internal/repo/` - Memory profiling on repo package
- `/profile all ./...` - Both CPU and memory on all packages

## Steps

1. **Parse arguments**
   - `$ARGUMENTS[0]` or `$0`: Profile type (`cpu`, `memory`, or `all`)
   - `$ARGUMENTS[1]` or `$1`: Package path (defaults to `./...`)

2. **Create profile output directory**
   ```bash
   mkdir -p .profiles
   ```

3. **Run profiling benchmarks**

   For CPU profiling:
   ```bash
   go test -cpuprofile=.profiles/cpu.prof -bench=. $1 2>&1
   ```

   For memory profiling:
   ```bash
   go test -memprofile=.profiles/mem.prof -bench=. $1 2>&1
   ```

4. **Analyze CPU profile**
   ```bash
   go tool pprof -top -cum .profiles/cpu.prof 2>&1 | head -30
   ```

   Identify:
   - Top 10 CPU consumers by cumulative time
   - Functions with high self time (computation hotspots)
   - Unexpected entries (potential optimization targets)

5. **Analyze memory profile**
   ```bash
   go tool pprof -top -alloc_space .profiles/mem.prof 2>&1 | head -30
   ```

   Identify:
   - Top allocators by total bytes
   - Functions with high allocation counts
   - Potential sources of GC pressure

6. **Generate flamegraph data** (if requested)
   ```bash
   go tool pprof -raw .profiles/cpu.prof > .profiles/cpu.raw
   ```

7. **Report findings**

   Structure the report as:

   ### CPU Hotspots
   | Function | Self% | Cum% | Observation |
   |----------|-------|------|-------------|

   ### Memory Allocators
   | Function | Bytes | Allocs | Observation |
   |----------|-------|--------|-------------|

   ### Optimization Suggestions
   - List specific, actionable recommendations
   - Reference line numbers where applicable
   - Note any patterns (e.g., repeated allocations in loops)

## Interpreting Results

### CPU Profile Indicators
- **High self%**: Direct computation hotspot
- **High cum% but low self%**: Calls expensive functions
- **runtime.***: GC or scheduler overhead

### Memory Profile Indicators
- **High alloc_space**: Total memory pressure
- **High alloc_objects**: GC pressure from many small allocations
- **Repeated patterns**: Loop allocations, string concatenation

## Common Hotspots in Veloria

Watch for issues in:
- `(*Index).Search` - Regex compilation, line reading
- `(*Repository).Load` - Index file mapping
- `(*IndexedExtension).Update` - Hot-swap operations
- HTTP handlers - JSON marshaling, response writing

## Cleanup

Profile files are stored in `.profiles/`. Add to `.gitignore` if not already present.
