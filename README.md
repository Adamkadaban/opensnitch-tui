# OpenSnitch TUI

Terminal user interface for [OpenSnitch](https://github.com/evilsocket/opensnitch) built with Go and Bubble Tea. The goal is feature parity with the existing Python/Qt GUI: interactive prompts, rule management, node orchestration, firewall visibility, and telemetry dashboards.

## Repository Layout

- `cmd/opensnitch-tui`: program entrypoint.
- `internal/app`: runtime wiring (config, state, Bubble Tea program).
- `internal/state`: central store shared across views.
- `internal/ui`: Bubble Tea router and individual views.
- `internal/config`: YAML config loader stored under `~/.config/opensnitch-tui/config.yaml`.
- `references/`: upstream sources vendored locally (OpenSnitch proto/UI and Bubble Tea).

## Quickstart

1. Ensure Go 1.24+ is available and `go.work` points at the repo root (`go work sync` if modules change).
2. Install `golangci-lint` (>= 1.56) for linting.
3. Build, lint, and test frequently:

```bash
make build
make lint
make test
```

4. Run the TUI once a config exists:

```bash
make run -- -config ~/.config/opensnitch-tui/config.yaml
```

Use `-theme light|dark|auto` to override the configured palette for a session.

## Configuration

Configuration lives at `~/.config/opensnitch-tui/config.yaml` by default. Example:

```yaml
theme: auto
nodes:
	- id: primary
		name: workstation
		address: 127.0.0.1:50051
		cert_path: /etc/opensnitch/ui/client.crt
		key_path: /etc/opensnitch/ui/client.key
		skip_tls: false
```

The bootstrapped UI renders configured nodes immediately; connection state will be filled in by the daemon client layer in subsequent milestones.

## Development Workflow

- Follow `AGENTS.md` instructions: keep comments sparse, lint and test continuously, and verify the project builds after each change.
- Use `make lint` before commits to catch formatting or vet issues.
- Use `go test ./...` (or `make test`) to ensure state reducers, views, and future daemon interactions remain healthy.
- When editing vendor references (proto or Bubble Tea), regenerate code via dedicated scripts before committing to avoid drift.

## Testing & Linting

- **Linting:** `make lint` (golangci-lint) enforces formatting, vet, and static checks. Install golangci-lint ≥ 1.56 and keep it on PATH.
- **Unit tests:** `go test ./...` or `make test` exercises store reducers, view logic, and controller adapters. Run after every change.
- **Snapshot / TUI regression tests:** use `make snapshots` (wraps `go test ./internal/ui/... -run Snapshot`) to refresh vt100 recordings whenever UI output intentionally changes. Commit updated artifacts under `testdata/` alongside code.
- **Pre-push sanity:** `make verify` chains lint + tests so CI matches local runs.
- **Golden files:** when tests under `internal/ui/view/viewtest` fail, inspect diffs via `git diff` before updating expected output with `UPDATE_SNAPSHOTS=1 go test ./path/to/package`.

## Status

The current milestone focuses on scaffolding: config loading, theming, Bubble Tea router, dashboard/nodes views, and build/test automation. Upcoming work will wire gRPC clients, live telemetry, modal prompts, firewall views, and full feature parity with the Qt UI.
