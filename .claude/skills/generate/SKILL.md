---
name: generate
description: Run go generate to build templ templates and frontend assets. Use after changing templates or CSS/JS.
disable-model-invocation: true
---

# Generate Assets

Run `go generate` to build templ templates and frontend CSS/JS assets.

## Usage

- `/generate` - Generate all assets

## Steps

1. **Run go generate**
   ```bash
   go generate ./assets/... 2>&1
   ```

   This executes two steps defined in `assets/embed.go`:
   1. `templ generate` - Compiles `.templ` files into Go code
   2. `npm install && npm run build` - Builds frontend CSS/JS into `assets/static/`

2. **Verify output**
   ```bash
   ls -la assets/static/css/ assets/static/js/ 2>&1
   ```

3. **Install the updated binary**
   ```bash
   go install ./...
   ```

4. **Report results**
   ```
   ## Generate Results

   ### templ
   - [number of templates compiled]

   ### Frontend
   - CSS: [files generated]
   - JS: [files generated]

   ### Status
   - Binary installed: yes/no
   ```

## When to Run

Run `/generate` after changing:
- Any `.templ` template file
- Frontend CSS in `frontend/css/`
- Frontend JS or `package.json`

## Prerequisites

- Node.js and npm must be available
- npm dependencies are installed automatically by the generate step
- `templ` CLI must be installed (`go install github.com/a-h/templ/cmd/templ@latest` if missing)

## CI Alignment

The CI pipeline runs `go generate ./assets/...` before every build. Running `/generate` locally ensures your embedded assets are current.
