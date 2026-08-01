<h1 align="center">OpenSnitch TUI</h1>

<p align="center">
  A keyboard-first terminal control center for the OpenSnitch application firewall.
</p>

<p align="center">
  <a href="https://github.com/Adamkadaban/opensnitch-tui/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/Adamkadaban/opensnitch-tui?include_prereleases&sort=semver"></a>
  <a href="https://github.com/Adamkadaban/opensnitch-tui/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Adamkadaban/opensnitch-tui/actions/workflows/ci.yml/badge.svg"></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-GPL--3.0-blue"></a>
  <img alt="Made with vibes" src="https://img.shields.io/badge/made_with-vibes-ff69b4">
</p>

> [!WARNING]
> This project was built with substantial AI assistance. Review firewall changes carefully and test on a disposable machine before production use.

<p align="center">
  <a href="https://asciinema.org/a/HqPc46dL8TbHQG7YgiR7g02ia">
    <img alt="OpenSnitch TUI demo" src="https://asciinema.org/a/HqPc46dL8TbHQG7YgiR7g02ia.svg" width="760">
  </a>
</p>

## What it does

- Handles live allow, deny, and reject prompts with timed and composite rules.
- Aggregates dashboard statistics from multiple OpenSnitch daemons.
- Manages rules, firewall state, daemon tasks, alerts, and safe node settings.
- Imports and exports private node-scoped rule archives.
- Inspects processes and optionally scans executables with YARA.
- Supports narrow terminals, deterministic screenshots, and keyboard-only navigation.

OpenSnitch `v1.8.0` is the primary compatibility target.

## Quick start

Close the desktop OpenSnitch UI first so it does not own `/tmp/osui.sock`, then:

```bash
make build
./bin/opensnitch-tui
```

The daemon normally reconnects automatically. If it does not:

```bash
sudo systemctl restart opensnitch.service
```

For installation, release variants, daemon configuration, YARA, and multi-node setup, see **[docs/SETUP.md](docs/SETUP.md)**.

## Navigation

Use `Tab` and `Shift+Tab` to switch views. Navigation uses arrow keys only; there are no vi bindings.

| View | Main controls |
|---|---|
| Dashboard | Aggregated node telemetry |
| Events | Arrows, PgUp/PgDn, Home/End |
| Alerts | `Enter` details, `x` delete locally, `o` export |
| Rules | `c` copy, `i` import, `o` export, `e` enable, `d` disable, `x` delete, `m` modify |
| Firewall | Left/right node, up/down chain, `e` enable, `d` disable, `r` reload |
| Tasks | Left/right node, up/down monitor, `s` start, `x` stop |
| Nodes | `Enter` details, `e` edit safe settings, `s` save, `Esc` cancel/back |
| Settings | Arrows and Enter/Space |
| Prompt | `a` allow, `d` deny, `r` reject, `v` advanced matches, `i` inspect |

## Configuration

The optional TUI configuration lives at:

```text
~/.config/opensnitch-tui/config.yaml
```

```yaml
theme: midnight
default_prompt_action: deny
default_prompt_duration: once
default_prompt_target: process.path
prompt_timeout_seconds: 300
alerts_interrupt: false
pause_prompt_on_inspect: true
yara_rule_dir: ""
yara_enabled: false
nodes: []
```

Common flags:

```text
-config PATH
-theme midnight|canopy|dawn
-listen unix:///tmp/osui.sock
```

TCP listeners remain available for trusted private multi-node networks:

```bash
./bin/opensnitch-tui -listen 0.0.0.0:50051
```

## Releases

Version tags publish checksummed Linux archives:

| Artifact | Linking | YARA |
|---|---|---|
| `linux_x86_64_portable` | Static | No |
| `linux_x86_64_yara` | Dynamic | Yes |
| `linux_arm64_portable` | Static | No |

Portable builds are single binaries and do not require `libyara`.

## Development

```bash
make build
make test
go test -race ./...
make lint
make capture-ui
```

`make capture-ui` requires `tmux` and [Freeze](https://github.com/charmbracelet/freeze). Captures are written to ignored `artifacts/tui-captures/`.

The real daemon integration test is intentionally restricted to disposable Linux machines:

```bash
sudo make test-opensnitch-integration
```

See **[docs/SETUP.md](docs/SETUP.md)** for the safety notes.

## License

[GNU General Public License v3.0](LICENSE)
