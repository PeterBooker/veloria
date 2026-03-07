---
name: deps
description: Check and tidy Go module dependencies. Use after adding/removing imports or before releases.
disable-model-invocation: true
---

# Dependency Management

Check and tidy Go module dependencies to keep go.mod/go.sum clean.

## Usage

- `/deps` - Tidy and verify dependencies

## Steps

1. **Tidy modules**
   ```bash
   go mod tidy 2>&1
   ```

2. **Verify checksums**
   ```bash
   go mod verify 2>&1
   ```

3. **Check for changes**
   ```bash
   git diff --stat go.mod go.sum 2>&1
   ```

4. **Report findings**
   ```
   ## Dependency Check

   ### Changes
   - [added/removed/changed dependencies]

   ### Verification
   - Checksums: OK / issues found

   ### Summary
   - go.mod changed: yes/no
   - go.sum changed: yes/no
   ```

5. **If dependencies changed**, show the diff and explain what was added or removed.

## When to Run

- After adding new imports
- After removing packages or code
- Before creating a PR
- After resolving merge conflicts in go.mod/go.sum
