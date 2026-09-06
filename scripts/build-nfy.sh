#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${2:-$REPO_ROOT/rep/dist}"
SOURCE_DIR="$REPO_ROOT/rep"

mkdir -p "$OUTPUT_DIR"

declare -a TARGETS=(
    "linux:amd64:nfy-linux-x64"
    "linux:arm64:nfy-linux-arm64"
    "windows:amd64:nfy-windows-x64.exe"
    "windows:arm64:nfy-windows-arm64.exe"
    "darwin:amd64:nfy-macos-x64"
    "darwin:arm64:nfy-macos-arm64"
)

for target in "${TARGETS[@]}"; do
    IFS=':' read -r GOOS GOARCH ASSET <<< "$target"
    output="$OUTPUT_DIR/$ASSET"
    echo "Building $output..."
    # CGO_ENABLED=0 keeps the binaries free of any libc dependency, so a Linux
    # build does not inherit the glibc version of the machine that produced it.
    GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build -C "$SOURCE_DIR" -trimpath \
        -ldflags "-s -w -X github.com/flcl42/notify/rep/internal/version.Version=$VERSION" \
        -o "$output" .
done

for target in "amd64:notify-server-linux-x64" "arm64:notify-server-linux-arm64"; do
    IFS=':' read -r GOARCH ASSET <<< "$target"
    output="$OUTPUT_DIR/$ASSET"
    echo "Building $output..."
    GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build -C "$SOURCE_DIR" -trimpath \
        -ldflags "-s -w -X github.com/flcl42/notify/rep/internal/version.Version=$VERSION" \
        -o "$output" ./cmd/notify-server
done

echo "Done. Assets in $OUTPUT_DIR"
