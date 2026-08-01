> [!WARNING]
> This project is entirely vibecoded. Use at your own risk

# OpenSnitch TUI

TUI for [OpenSnitch](https://github.com/evilsocket/opensnitch) that includes a yara scanner.


## 📽 Demo
[![asciicast](https://asciinema.org/a/HqPc46dL8TbHQG7YgiR7g02ia.svg)](https://asciinema.org/a/HqPc46dL8TbHQG7YgiR7g02ia)



---

## 🧰 Requirements
- **Go** `1.24+`
- **golangci-lint** `>= 1.56` (for `make lint`)
- (Optional) **protoc** + `protoc-gen-go`/`protoc-gen-go-grpc` if regenerating stubs from `opensnitch/proto/ui.proto`
- (Optional) **YARA** support: cgo + libyara (e.g., `brew install yara`, `apt-get install libyara-dev`). Disable with `-tags no_yara`.

## 🚀 Quickstart
```bash
make build   # builds ./bin/opensnitch-tui
make lint    # golangci-lint run
make test    # go test ./...

# Run the TUI (pass your flags via ARGS)
make run ARGS="-config ~/.config/opensnitch-tui/config.yaml"
```
Common flags:
- `-config PATH` — YAML config (default `~/.config/opensnitch-tui/config.yaml`)
- `-theme light|dark|auto` — session theme override
- `-listen ADDRESS` — daemon listener (default `unix:///tmp/osui.sock`; TCP addresses remain supported)

## ⚙️ Configuration
Default location: `~/.config/opensnitch-tui/config.yaml`

```yaml
theme: midnight
default_prompt_action: deny
default_prompt_duration: always
default_prompt_target: process.path
prompt_timeout_seconds: 300
alerts_interrupt: false
pause_prompt_on_inspect: true
yara_rule_dir: /opt/yara_rules
yara_enabled: true
nodes: []
```

## 🧭 Usage (key hints)
- **Navigation:** arrow keys only (no vi keys)
- **Rules view:** `e` enable · `d` disable · `x` delete · `m` modify
- **Firewall view:** `←`/`→` select node · `↑`/`↓` select chain · `e` enable · `d` disable · `r` reload rules
- **Tasks view:** `←`/`→` select node · `↑`/`↓` select task profile · `s` start · `x` stop
- **Prompt dialog:** arrows to move focus/choices; `a` allow · `d` deny · `r` reject
- **Tables:** arrows to move; PgUp/PgDn/Home/End for paging

## 🔍 YARA scanning (optional)
- **Build requirements:** cgo enabled + **libyara** installed (`brew install yara` · `apt-get install libyara-dev`). Uses `github.com/hillu/go-yara/v4`.
- **Enable/disable:** set `yara_enabled: true|false` in config or toggle in **Settings → Security**. Default: `false`.
- **Rule directory:** set `yara_rule_dir: /path/to/yara_rules` (files ending in `.yar` / `.yara`). Rules are compiled once per directory and cached.
- **Disable at build time:** `go build -tags no_yara` (or `CGO_ENABLED=0`) uses a stub; YARA features will surface `yara not available`.

## 🗂 Repository Layout
- `cmd/opensnitch-tui/` — CLI entrypoint
- `internal/app/` — wiring: config, state, Bubble Tea program
- `internal/state/` — central store, reducers, selectors
- `internal/ui/` — router and views (dashboard, events, alerts, rules, nodes, settings, prompt)
- `internal/daemon/` — mock/server shim for tests; notification plumbing
- `internal/controller/` — interfaces for rule/prompt/settings managers
- `internal/pb/protocol/` — generated gRPC/proto stubs (from `opensnitch/proto/ui.proto`)
- `internal/config/` — YAML config loader
- `internal/theme/` — lipgloss styles
- `internal/util/` — misc helpers (ANSI-safe slicing, padding, display names)g

## 🛠 Build & Dev Workflow
- **Format & lint:** `gofmt -w` (IDE/Go tools) and `make lint`
- **Tests:** `make test` (aliases `go test ./...`)
- **Regenerating protos:** from repo root, run `make -C opensnitch/proto` (requires `protoc` + Go plugins)

## 🔍 Testing Notes
- Keep **unit tests** green (`go test ./...`)
- Add table/render tests under `internal/ui/views/...` when altering layout/keys
- Use `make capture-ui` to record deterministic screenshots before and after navigation inputs
- Visual captures require [`tmux`](https://github.com/tmux/tmux) and [`freeze`](https://github.com/charmbracelet/freeze)
- Captures are written under ignored `artifacts/tui-captures/`; never commit captures from a real daemon or production environment

### Real OpenSnitch v1.8 integration

> [!CAUTION]
> Run this only on a disposable Linux VM. It temporarily stops and starts the OpenSnitch systemd service, owns `/tmp/osui.sock` during the test, reloads the firewall's currently reported rules, and allows one `curl` request to `https://example.com`. It does not disable the firewall or create a persistent allow rule.

With OpenSnitch v1.8.0 installed and configured for its default `unix:///tmp/osui.sock` UI address:

```bash
sudo make test-opensnitch-integration
```

The harness detects `opensnitch.service` or `opensnitchd.service`; set `OPENSNITCH_SERVICE` only when the packaged unit uses another name. Normal `go test ./...` runs skip the real-service test without requiring root, systemd, or `curl`.
