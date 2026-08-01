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
- **Rules view:** `c` copy · `i` import · `o` export · `e` enable · `d` disable · `x` delete · `m` modify
- **Alerts view:** `↑`/`↓` select · `Enter` details · `esc` back · `x` delete locally · `o` export
- **Firewall view:** `←`/`→` select node · `↑`/`↓` select chain · `e` enable · `d` disable · `r` reload rules
- **Tasks view:** `←`/`→` select node · `↑`/`↓` select task profile · `s` start · `x` stop
- **Nodes view:** `↑`/`↓` select node · `Enter` details · `e` enter/exit safe config editing · arrows or `Enter`/`space` change values · `s` save · `esc` cancel/back
- **Prompt dialog:** arrows to move focus/choices; `a` allow · `d` deny · `r` reject · `v` advanced matches · `space`/`Enter` toggle advanced conditions
- **Tables:** arrows to move; PgUp/PgDn/Home/End for paging

## 🔍 YARA scanning (optional)
- **Build requirements:** cgo enabled + **libyara** installed (`brew install yara` · `apt-get install libyara-dev`). Uses `github.com/hillu/go-yara/v4`.
- **Enable/disable:** set `yara_enabled: true|false` in config or toggle in **Settings → Security**. Default: `false`.
- **Rule directory:** set `yara_rule_dir: /path/to/yara_rules` (files ending in `.yar` / `.yara`). Rules are compiled once per directory and cached.
- **Disable at build time:** `go build -tags no_yara` (or `CGO_ENABLED=0`) uses a stub; YARA features will surface `yara not available`.

## 📦 Rule archives

The Rules view imports and exports only the currently selected, connected node. Archives use canonical, indented JSON with one rule per file under:

```text
${XDG_DATA_HOME:-~/.local/share}/opensnitch-tui/rules/<sanitized-node>/
```

Directories are `0700`, files are `0600`, and exports stage and fsync a complete node generation before replacing the prior archive with rollback protection. Filenames are sanitized, while rule names inside JSON remain unchanged. Import reads only regular `.json` files from that fixed node directory and rejects links, malformed or duplicate rules, unsafe operator trees, and oversized batches. Nested `LIST` operators are rejected because OpenSnitch v1.8 only preserves immediate list children. Same-name rules replace existing rules only after the daemon acknowledges the `CHANGE_RULE` batch; daemon errors leave the in-memory rule state unchanged.

## 🚨 Alert archives

The Alerts view exports only the selected alert to:

```text
${XDG_DATA_HOME:-~/.local/share}/opensnitch-tui/alerts/
```

Alert exports use sanitized filenames, atomic replacement, `0700` directory permissions, and `0600` files. Structured payloads are bounded; process environment values and sensitive authentication fields are redacted.

## 🗂 Repository Layout
- `cmd/opensnitch-tui/` — CLI entrypoint
- `internal/app/` — wiring: config, state, Bubble Tea program
- `internal/state/` — central store, reducers, selectors
- `internal/rulearchive/` — secure node-scoped canonical JSON import/export
- `internal/alertarchive/` — secure bounded structured alert export
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

## 📦 Releases

Version tags matching `v*` publish checksummed Linux archives:

- `linux/amd64` with YARA support
- `linux/arm64` portable build using the `no_yara` tag

The release workflow runs module verification and tests before publishing. Create a release with:

```bash
git tag v1.0.0
git push origin v1.0.0
```

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
