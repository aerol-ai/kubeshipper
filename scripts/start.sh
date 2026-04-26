#!/bin/sh
# Launch helmd in the background and the bun API in the foreground.
# tini (PID 1) reaps the helmd process if bun exits.
set -eu

helmd --socket "${HELMD_SOCKET:-/tmp/helmd.sock}" &
HELMD_PID=$!

# Give helmd a moment to bind the socket before the API tries to dial it.
i=0
while [ ! -S "${HELMD_SOCKET:-/tmp/helmd.sock}" ] && [ "$i" -lt 30 ]; do
  i=$((i + 1))
  sleep 0.2
done

trap 'kill "$HELMD_PID" 2>/dev/null || true' EXIT
exec bun run src/index.ts
