---
name: race-check
description: Run Go race detector to find data races in concurrent code. Use after any change to mutexes, goroutines, or channels.
disable-model-invocation: true
argument-hint: "package-path"
---

# Race Condition Detection

Run Go's race detector to identify data races in concurrent code.

## Usage

- `/race-check` - Run race detection on all packages
- `/race-check ./internal/repo/...` - Run on specific package

## Steps

1. **Identify target packages**
   - If `$ARGUMENTS` specifies a package, use that
   - Otherwise, run on all packages with tests

2. **Run race detector**
   ```bash
   go test -race -timeout 5m $ARGUMENTS 2>&1
   ```

   Use extended timeout since race detection is slower.

4. **Parse race detector output**

   Look for patterns like:
   ```
   WARNING: DATA RACE
   Write at 0x... by goroutine N:
   ...
   Previous read at 0x... by goroutine M:
   ```

5. **For each race detected, identify:**
   - The memory address being accessed
   - Which goroutines are involved
   - The file and line numbers of conflicting accesses
   - The stack traces showing how each goroutine reached that point

6. **Analyze the race**

   Read the identified source files and determine:
   - What data structure is being accessed unsafely
   - Whether it's a read-write or write-write race
   - What synchronization is missing or incorrect

7. **Suggest fixes based on codebase patterns**

   Veloria uses these synchronization patterns:

   **RWMutex for read-heavy data:**
   ```go
   e.mu.RLock()
   defer e.mu.RUnlock()
   // read operations
   ```

   **Separate UpdateMutex for hot-swap:**
   ```go
   e.UpdateMutex.Lock()
   defer e.UpdateMutex.Unlock()
   // update operations
   ```

   **Atomic operations for simple counters:**
   ```go
   atomic.AddInt64(&counter, 1)
   ```

8. **Report findings**

   For each race:
   ```
   ## Race #N: [brief description]

   **Location:** file.go:123 vs file.go:456
   **Type:** read-write race on [field/variable]
   **Goroutines:** [description of concurrent operations]

   **Suggested Fix:**
   [specific code change with before/after]
   ```

## Critical Areas in Veloria

These packages require extra scrutiny:

| Package | Concurrent Access Pattern |
|---------|--------------------------|
| `internal/repo/` | Multiple readers during search, exclusive writer during update |
| `internal/index/` | Index read during search, replaced during hot-swap |
| `internal/tasks/` | Background workers accessing shared state |
| `internal/manager/` | Coordinates multiple repositories |
| `internal/cache/` | Ristretto handles its own concurrency |

## Common Race Patterns

### 1. Missing RLock during read
```go
// WRONG
return e.index.Search(query)

// RIGHT
e.mu.RLock()
idx := e.index
e.mu.RUnlock()
return idx.Search(query)
```

### 2. Inconsistent lock ordering
```go
// WRONG: Different order in different functions
func A() { mu1.Lock(); mu2.Lock() }
func B() { mu2.Lock(); mu1.Lock() } // Deadlock risk

// RIGHT: Always same order
func A() { mu1.Lock(); mu2.Lock() }
func B() { mu1.Lock(); mu2.Lock() }
```

### 3. Unlocking in defer with early return
```go
// WRONG: Lock held during long operation
mu.Lock()
defer mu.Unlock()
result := expensiveOperation() // Blocks others

// RIGHT: Copy under lock, process outside
mu.Lock()
data := sharedData
mu.Unlock()
result := process(data)
```

## No Races Found

If no races are detected, confirm:
- Tests exercise concurrent code paths
- Tests use `t.Parallel()` where appropriate
- Test coverage includes hot-swap scenarios
