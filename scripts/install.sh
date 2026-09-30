#!/bin/sh
# Switchyard install script — the evidence loop's front door.
#
# Downloads the latest release binary for this OS/arch to /usr/local/bin
# (or SWITCHYARD_BIN), verifies the SHA-256 checksum, then explains the
# deploy-registration signal (deploy ID + version only — never flag data).
# Registration is opt-out via SWITCHYARD_NO_TELEMETRY=1; docs/evidence.md
# documents exactly what is and is not sent.
#
# Usage:
#   curl -fsSL https://switchyard.sh/install.sh | sh
#   curl -fsSL https://switchyard.sh/install.sh | sh -s -- --no-telemetry
set -eu

NO_TELEMETRY="${SWITCHYARD_NO_TELEMETRY:-0}"
GITHUB_REPO="jonathanngiroux-star/switchyard"
BASE="https://github.com/${GITHUB_REPO}/releases/latest/download"
BIN="${SWITCHYARD_BIN:-/usr/local/bin/switchyard}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

for arg in "$@"; do
  case "$arg" in
    --no-telemetry) NO_TELEMETRY=1 ;;
  esac
done

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture $ARCH"; exit 1 ;;
esac
case "$OS" in
  linux|darwin) ;;
  *) echo "error: unsupported OS $OS (use the GitHub releases page for Windows)"; exit 1 ;;
esac

ASSET="switchyard-${OS}-${ARCH}"
echo "==> downloading ${ASSET}"
curl -fsSL -o "$TMP/$ASSET" "${BASE}/${ASSET}"
echo "==> verifying checksum"
curl -fsSL -o "$TMP/checksums.txt" "${BASE}/checksums.txt"
(cd "$TMP" && grep " ${ASSET}\$" checksums.txt | sha256sum -c -) \
  || { echo "error: checksum verification failed"; exit 1; }

echo "==> installing to $BIN"
chmod +x "$TMP/$ASSET"
mv "$TMP/$ASSET" "$BIN"

"$BIN" version

if [ "$NO_TELEMETRY" = "1" ]; then
  echo "==> telemetry opted out (SWITCHYARD_NO_TELEMETRY)"
else
  echo "==> Switchyard is self-hosted software: nothing phones home from"
  echo "    evaluation or serving. The only optional signal is a one-time"
  echo "    deploy registration (deploy ID + version — see docs/evidence.md)."
  echo "    To skip it: set SWITCHYARD_NO_TELEMETRY=1."
fi
echo "==> done: $BIN  (run bare for the TUI; 'switchyard serve' for the web UI)"
