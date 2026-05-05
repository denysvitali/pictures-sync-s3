# Repository Guidelines

## Project Structure & Module Organization
`pictures-sync-s3` is a Go project organized as:

- `cmd/pictures-sync/`: SD card monitor/sync daemon.
- `cmd/webui/`: HTTP + WebSocket management UI service.
- `pkg/`: core packages (`state`, `sdmonitor`, `syncmanager`, `settings`, `ledcontroller`, `wifimanager`).
- `web/`: embedded UI assets (`index.html`, `app.js`, `style.css`).
- `config/`: configuration template (`rclone.conf.template`) and deployment helpers.
- Root docs: `README.md`, `QUICKSTART.md`, `CLAUDE.md`, `setup-gokrazy.sh`.

## Build, Test, and Development Commands
- `go build ./cmd/pictures-sync` — build sync daemon.
- `go build ./cmd/webui` — build web UI binary.
- `go build ./...` — compile all packages.
- `go test ./...` — run all tests.
- `go test ./pkg/state` — run package tests while iterating.
- `PORT=8080 ./webui` — run UI locally at `http://localhost:8080`.
- `./setup-gokrazy.sh` — initial Gokrazy setup.
- `gok -i <instance> update|edit|overwrite --full /dev/sdX` — deploy/update workflow.

## Coding Style & Naming Conventions
- Use standard Go style with `gofmt` on changed files.
- Variable/function names: `camelCase`; exported types/functions: `PascalCase`.
- Keep package boundaries clear and avoid duplicating existing state/wiring logic.
- Prefer explicit names and short files with one primary responsibility.

## Testing Guidelines
- Current coverage is lightweight; new features should include tests when feasible.
- Place tests in package `_test.go` files such as `state_test.go`, `syncmanager_test.go`.
- Use table-driven tests for branching logic (threshold checks, status transitions).
- Include command and path-specific edge cases when touching I/O and filesystem behavior.

## Commit & Pull Request Guidelines
- Commit messages in this repo follow a short conventional format like `feat: improve sync` and `feat: improve UI`; keep this style and imperative phrasing.
- In PRs include: summary, changed files, validation commands run (`go test ./...`, relevant `go build`), and any deployment notes.
- For UI changes, attach a screenshot or brief UI behavior notes from local webui run.
- Never commit generated WebUI distribution assets under `pkg/webui/dist/`, under any circumstance. These files are build output, must stay ignored, and must not be force-added even when a UI build regenerates hashed filenames.

## Security & Configuration Tips
- Do not commit real credentials (`rclone.conf`, Wi‑Fi passwords, Tailscale keys).
- Validate paths that write under `/perm` carefully; this app relies on atomic writes for state and settings.
