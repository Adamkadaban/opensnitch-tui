#!/usr/bin/env bash
set -Eeuo pipefail

socket=/tmp/osui.sock
test_pid=
service=
service_was_active=0

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || fail "required tool not found: $1"
}

detect_service() {
	if [[ -n ${OPENSNITCH_SERVICE:-} ]]; then
		systemctl cat "$OPENSNITCH_SERVICE" >/dev/null 2>&1 ||
			fail "systemd service not found: $OPENSNITCH_SERVICE"
		printf '%s\n' "$OPENSNITCH_SERVICE"
		return
	fi

	local candidate
	for candidate in opensnitch.service opensnitchd.service; do
		if systemctl cat "$candidate" >/dev/null 2>&1; then
			printf '%s\n' "$candidate"
			return
		fi
	done
	fail "OpenSnitch systemd service not found (set OPENSNITCH_SERVICE to override)"
}

show_diagnostics() {
	printf '\nOpenSnitch service diagnostics:\n' >&2
	systemctl status "$service" --no-pager --full >&2 || true
	printf '\nRecent OpenSnitch journal:\n' >&2
	journalctl -u "$service" --since '-5 minutes' --no-pager -n 200 >&2 || true
}

cleanup() {
	local status=$?
	local restore_failed=0
	trap - EXIT INT TERM

	if [[ -n $test_pid ]] && kill -0 "$test_pid" 2>/dev/null; then
		kill "$test_pid" 2>/dev/null || true
		wait "$test_pid" 2>/dev/null || true
	fi

	systemctl stop "$service" >/dev/null 2>&1 || true
	if [[ -S $socket ]]; then
		rm -f -- "$socket"
	elif [[ -e $socket ]]; then
		printf 'warning: refusing to remove non-socket path %s\n' "$socket" >&2
		restore_failed=1
	fi

	if ((service_was_active)); then
		if ! systemctl start "$service" || ! systemctl is-active --quiet "$service"; then
			printf 'error: failed to restore %s to its initial active state\n' "$service" >&2
			restore_failed=1
		fi
	fi

	if ((restore_failed)) && ((status == 0)); then
		status=1
	fi
	exit "$status"
}

[[ $(id -u) -eq 0 ]] || fail "run this integration harness with sudo/root"
[[ $(uname -s) == Linux ]] || fail "this integration harness requires Linux"

go_bin=${GO:-go}
for tool in "$go_bin" curl systemctl journalctl timeout; do
	require_tool "$tool"
done

service=$(detect_service)
if systemctl is-active --quiet "$service"; then
	service_was_active=1
fi

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'WARNING: this test temporarily stops %s, starts the real OpenSnitch daemon,\n' "$service"
printf 'reloads its currently reported firewall rules, and permits one curl request.\n'
printf 'Use only on a disposable test VM.\n\n'

systemctl stop "$service"
if systemctl is-active --quiet "$service"; then
	fail "$service remained active after stop"
fi

if [[ -e $socket && ! -S $socket ]]; then
	fail "refusing to replace non-socket path $socket"
fi
if [[ -S $socket ]]; then
	rm -f -- "$socket"
fi

OPENSNITCH_INTEGRATION=1 \
	OPENSNITCH_INTEGRATION_ORCHESTRATED=1 \
	timeout --signal=TERM --kill-after=10s 150s \
	"$go_bin" test -v ./internal/daemon \
	-run '^TestOpenSnitchV18Integration$' \
	-count=1 \
	-timeout=2m &
test_pid=$!

socket_deadline=$((SECONDS + 20))
while [[ ! -S $socket ]]; do
	if ! kill -0 "$test_pid" 2>/dev/null; then
		set +e
		wait "$test_pid"
		status=$?
		set -e
		test_pid=
		fail "integration test exited before creating $socket (status $status)"
	fi
	if ((SECONDS >= socket_deadline)); then
		show_diagnostics
		fail "timed out waiting for test server socket $socket"
	fi
	sleep 0.1
done

systemctl start "$service"
if ! systemctl is-active --quiet "$service"; then
	show_diagnostics
	fail "$service did not become active"
fi

set +e
wait "$test_pid"
status=$?
set -e
test_pid=

if ((status != 0)); then
	show_diagnostics
	exit "$status"
fi
