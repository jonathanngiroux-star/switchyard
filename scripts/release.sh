#!/bin/bash
# scripts/release.sh — build + checksum the release matrix for a tag.
# Usage: bash scripts/release.sh <version> [outdir]
#
# Default binaries are CGO_ENABLED=0 (run anywhere: Docker, scratch, CI).
# The desktop GUI (wails: desktop,production,webkit2_41 tags) needs cgo +
# webkit2gtk-4.1 and is built only for the host platform here; CI release
# jobs build it per-OS.
set -eu
VERSION="${1:?usage: release.sh <version> [outdir]}"
OUT="${2:-dist}"
mkdir -p "$OUT"

build_default() {
  local os="$1" arch="$2"
  local name="switchyard-${os}-${arch}"
  echo "==> ${name} (cgo-free)"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT}/${name}" ./cmd/switchyard
}

for os in linux darwin windows; do
  for arch in amd64 arm64; do
    build_default "$os" "$arch"
  done
done

# Desktop GUI: wails tags (desktop,production,webkit2_41), cgo, host
# platform only. Needs webkit2gtk-4.1 (see docs/gui-setup.md).
if command -v gcc >/dev/null 2>&1; then
  echo "==> switchyard-desktop-$(go env GOOS)-$(go env GOARCH) (wails, cgo)"
  CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT}/switchyard-desktop-$(go env GOOS)-$(go env GOARCH)" ./cmd/switchyard
else
  echo "==> skipping desktop build (no gcc)"
fi

echo "==> checksums"
cd "$OUT" && sha256sum switchyard-* > checksums.txt && cd ..
ls -la "$OUT"
