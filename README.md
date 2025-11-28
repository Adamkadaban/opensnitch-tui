# OpenSnitch TUI

Bubble Tea–based terminal UI for [OpenSnitch](https://github.com/evilsocket/opensnitch). Target: **feature parity with the Python/Qt GUI**—interactive prompts, rule lifecycle, multi-node orchestration, firewall visibility, and telemetry dashboards.

---

## 🧰 Requirements
- **Go** `1.24+`
- **golangci-lint** `>= 1.56` (for `make lint`)
- (Optional) **protoc** + `protoc-gen-go`/`protoc-gen-go-grpc` if regenerating stubs from `references/opensnitch/proto/ui.proto`

## 🚀 Quickstart
```bash
make build   # compile
make lint    # golangci-lint run
make test    # go test ./...

# Run the TUI (pass your flags via ARGS)
make run ARGS="-config ~/.config/opensnitch-tui/config.yaml"
```
Common flags:
- `-config PATH` — YAML config (default `~/.config/opensnitch-tui/config.yaml`)
- `-theme light|dark|auto` — session theme override

## ⚙️ Configuration
Default location: `~/.config/opensnitch-tui/config.yaml`

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

## 🧭 Usage (key hints)
- **Navigation:** arrow keys only (no vi keys)
- **Rules view:** `e` enable · `d` disable · `x` delete · `m` modify
- **Prompt dialog:** arrows to move focus/choices; `a` allow · `d` deny · `r` reject
- **Tables:** arrows to move; PgUp/PgDn/Home/End for paging

## 🗂 Repository Layout
- `cmd/opensnitch-tui/` — CLI entrypoint
- `internal/app/` — wiring: config, state, Bubble Tea program
- `internal/state/` — central store, reducers, selectors
- `internal/ui/` — router and views (dashboard, events, alerts, rules, nodes, settings, prompt)
- `internal/daemon/` — mock/server shim for tests; notification plumbing
- `internal/controller/` — interfaces for rule/prompt/settings managers
- `internal/pb/protocol/` — generated gRPC/proto stubs (from `references/opensnitch/proto/ui.proto`)
- `internal/config/` — YAML config loader
- `internal/theme/` — lipgloss styles
- `internal/util/` — misc helpers (ANSI-safe slicing, padding, display names)
- `references/` — vendored upstreams
	- `references/opensnitch/` — upstream daemon/UI/proto (read-only; regenerate stubs when upstream changes)
	- `references/bubbletea/` — Bubble Tea reference copy for hacking/patching

## 🛠 Build & Dev Workflow
- **Format & lint:** `gofmt -w` (IDE/Go tools) and `make lint`
- **Tests:** `make test` (aliases `go test ./...`)
- **Regenerating protos:** from repo root, run `make -C references/opensnitch/proto` (requires `protoc` + Go plugins)
- **Regenerating bubbletea:** commit local patches under `references/bubbletea`; keep module pins in sync

## 🔍 Testing Notes
- Keep **unit tests** green (`go test ./...`)
- Add table/render tests under `internal/ui/views/...` when altering layout/keys
- Snapshot/VT tests can be introduced under `internal/ui/view/viewtest` (none shipped yet)

## 📦 Release/Dist (future)
- Plan for `goreleaser` with Linux amd64/arm64 static builds
- Package sample config, man page, shell completions

## 🧱 Project Status
Active development. Implemented: router, dashboard/events/alerts/rules/nodes/settings views, rule editing, prompt UI scaffolding. Upcoming: live gRPC wiring, firewall view, full parity with Qt UI.

## 🤝 Contributing
- Follow `AGENTS.md`
- Keep comments minimal; prefer self-documenting code
- Always run `make test` before sending changes
