#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
output_dir=${1:-"${root_dir}/artifacts/tui-captures"}
session="opensnitch-tui-capture-$$"
temp_dir=$(mktemp -d)
binary="${temp_dir}/opensnitch-tui"

cleanup() {
	tmux kill-session -t "${session}" 2>/dev/null || true
	rm -rf -- "${temp_dir}"
}
trap cleanup EXIT

for command in go tmux freeze; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		printf 'required command not found: %s\n' "${command}" >&2
		exit 1
	fi
done

mkdir -p -- "${output_dir}"
cd -- "${root_dir}"
go build -o "${binary}" ./cmd/opensnitch-tui

tmux new-session -d -x 120 -y 40 -s "${session}" \
	"env TERM=xterm-256color '${binary}' -config '${temp_dir}/config.yaml' -listen 127.0.0.1:0"

for _ in $(seq 1 50); do
	if tmux capture-pane -p -t "${session}" | grep -q 'OpenSnitch TUI'; then
		break
	fi
	sleep 0.1
done

if ! tmux capture-pane -p -t "${session}" | grep -q 'OpenSnitch TUI'; then
	printf 'TUI did not become ready\n' >&2
	exit 1
fi

capture() {
	local name=$1
	tmux capture-pane -p -t "${session}" > "${output_dir}/${name}.txt"
	tmux capture-pane -p -e -t "${session}" | freeze -c full -o "${output_dir}/${name}.png"
}

capture 01-dashboard-120x40

tmux send-keys -t "${session}" Tab
sleep 0.2
capture 02-events-120x40
grep -q 'No events yet.' "${output_dir}/02-events-120x40.txt"

tmux resize-window -t "${session}" -x 80 -y 40
sleep 0.2
capture 03-events-80x40

tmux send-keys -t "${session}" BTab
sleep 0.2
capture 04-dashboard-80x40
grep -q 'Traffic mix' "${output_dir}/04-dashboard-80x40.txt"

printf 'TUI captures written to %s\n' "${output_dir}"
