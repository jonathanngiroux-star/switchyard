#!/bin/sh
# Switchyard install script — the evidence loop's front door.
#
# Installs the latest release binary to /usr/local/bin (or SWITCHYARD_BIN),
# then offers to register the deployment (deploy ID + version only — never
# flag data). Registration is opt-out via SWITCHYARD_NO_TELEMETRY=1 or
# --no-telemetry; docs/evidence.md documents exactly what is sent.
#
# Usage:
#   curl -fsSL https://switchyard.example/install.sh | sh
#   curl -fsSL https://switchyard.example/install.sh | sh -s -- --no-telemetry
set -eu

NO_TELEMETRY="${SWITCHYARD_NO_TELEMETRY:-0}"
REGISTRY="${SWITCHYARD_REGISTRY:-https://switchyard.example}"
BIN="${SWITCHYARD_BIN:-/usr/local/bin/switchyard}"

for arg in "$@"; do
  case "$arg" in
    --no-telemetry) NO_TELEMETRY=1 ;;
  esac
done

echo "==> downloading switchyard"
# TODO(release): real download URL + checksum verification once the first
# release ships. Until then this script documents the loop and fails
# loudly rather than installing something unverifiable.
echo "error: no release channel configured yet — build from source:"
echo "  git clone https://github.com/jonathanngiroux-star/switchyard"
echo "  cd switchyard && go build -o $BIN ./cmd/switchyard"
exit 1
