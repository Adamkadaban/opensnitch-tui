# Setup and troubleshooting

OpenSnitch TUI acts as the OpenSnitch **UI server**. One or more OpenSnitch daemons connect to it over a Unix socket or TCP.

## Compatibility

- OpenSnitch daemon `v1.8.0`
- Linux
- Go `1.24+` when building from source
- A terminal with color support

The default endpoint matches OpenSnitch v1.8:

```text
unix:///tmp/osui.sock
```

Only one UI server can own that socket. Close the Python/Qt OpenSnitch UI before starting the TUI.

## Release variants

Releases contain three Linux builds:

| Archive suffix | Architecture | Linking | YARA |
|---|---|---|---|
| `linux_x86_64_portable` | amd64 | Static | No |
| `linux_x86_64_yara` | amd64 | Dynamic | Yes |
| `linux_arm64_portable` | arm64 | Static | No |

The portable builds are the simplest option. Extract the archive and run:

```bash
./opensnitch-tui
```

The YARA build requires the runtime libraries used by the build environment, including `libyara`.

## Build from source

Install Go and the optional YARA development package:

```bash
sudo apt-get update
sudo apt-get install --yes build-essential libyara-dev
make build
```

Run:

```bash
./bin/opensnitch-tui
```

Build a portable static binary without YARA:

```bash
CGO_ENABLED=0 go build -tags no_yara -o bin/opensnitch-tui-portable ./cmd/opensnitch-tui
```

## Connect the local daemon

1. Close the desktop OpenSnitch UI.
2. Start OpenSnitch TUI:

   ```bash
   ./bin/opensnitch-tui
   ```

3. If the node does not appear, restart the daemon:

   ```bash
   sudo systemctl restart opensnitch.service
   ```

4. Check the daemon:

   ```bash
   systemctl status opensnitch.service
   journalctl -u opensnitch.service -n 100 --no-pager
   ```

The daemon config should contain:

```json
{
  "Server": {
    "Address": "unix:///tmp/osui.sock"
  }
}
```

The packaged config is normally `/etc/opensnitchd/default-config.json`.

## Multi-node setup

Run the TUI on a trusted private host:

```bash
./bin/opensnitch-tui -listen 0.0.0.0:50051
```

Set each daemon's `Server.Address` to the private address of the TUI host:

```json
{
  "Server": {
    "Address": "10.0.0.5:50051"
  }
}
```

Restart each daemon after changing its configuration:

```bash
sudo systemctl restart opensnitch.service
```

> [!CAUTION]
> Do not expose the plain TCP listener to the public internet. Restrict it with host/cloud firewalls to the daemon nodes on a trusted private network.

The TUI keeps state, rules, tasks, firewall controls, and settings isolated by stable node identity. Reconnecting a daemon should update the existing node rather than create a duplicate.

## YARA

YARA support is optional and disabled by default.

```yaml
yara_enabled: true
yara_rule_dir: /opt/yara-rules
```

Accepted rule extensions are `.yar` and `.yara`.

If the binary was built with `no_yara`, inspection reports that YARA is unavailable rather than silently skipping scans.

## Tasks

Tasks are daemon-side monitor streams:

- Node monitor
- Sockets monitor
- PID monitor protocol support

Select a node and profile in the Tasks view, then press `s`. Updates appear in the selected profile's details after the configured five-second interval.

If the view remains in `WAITING` and times out:

1. Confirm the daemon is OpenSnitch v1.8.
2. Check its logs:

   ```bash
   journalctl -u opensnitch.service -n 100 --no-pager
   ```

3. Check the packaged task configuration under `/etc/opensnitchd/tasks/`.
4. Confirm the node remains connected in the Nodes view.

Press `x` to stop a running task.

## Archives

Rule archives:

```text
${XDG_DATA_HOME:-~/.local/share}/opensnitch-tui/rules/<node>/
```

Alert archives:

```text
${XDG_DATA_HOME:-~/.local/share}/opensnitch-tui/alerts/
```

Directories use mode `0700`; files use `0600`. Imports reject symlinks, oversized files, malformed rules, duplicate names, and v1.8-incompatible nested operator lists.

## Common errors

### `unix socket ... is already active`

Another UI server owns `/tmp/osui.sock`. Close the desktop OpenSnitch UI or the other TUI instance.

### Node count stays at zero

- Confirm the TUI started before the daemon reconnect attempt.
- Restart `opensnitch.service`.
- Check that `Server.Address` matches the TUI listener.
- Check socket/file permissions and daemon logs.

### `yara not available`

Use the YARA release/build and install `libyara`, or leave YARA disabled when using a portable build.

### Tasks show no updates

Wait for the first five-second interval. The TUI reports an actionable error after 15 seconds if no update arrives.

### Small terminal

The UI adapts to short and narrow terminals. A terminal around `80x24` or larger provides the best experience.

## Testing on a disposable machine

The real integration harness:

```bash
sudo make test-opensnitch-integration
```

It temporarily:

- Stops and starts `opensnitch.service`
- Owns `/tmp/osui.sock`
- Starts real daemon task streams
- Reloads the currently reported firewall rules
- Allows one request to `https://example.com`

Run it only on a disposable Linux VM. Normal `go test ./...` skips this test.
