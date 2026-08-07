#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${2:-$REPO_ROOT/rep/dist}"
SOURCE_DIR="$REPO_ROOT/rep"

mkdir -p "$OUTPUT_DIR"

declare -a TARGETS=(
    "linux:amd64:rep-linux-x64"
    "linux:arm64:rep-linux-arm64"
    "windows:amd64:rep-windows-x64.exe"
    "windows:arm64:rep-windows-arm64.exe"
    "darwin:amd64:rep-macos-x64"
    "darwin:arm64:rep-macos-arm64"
)

for target in "${TARGETS[@]}"; do
    IFS=':' read -r GOOS GOARCH ASSET <<< "$target"
    output="$OUTPUT_DIR/$ASSET"
    echo "Building $output..."
    GOOS="$GOOS" GOARCH="$GOARCH" go build -C "$SOURCE_DIR" \
        -ldflags "-X github.com/flcl42/notify/rep/internal/version.Version=$VERSION" \
        -o "$output" .
done

echo "Done. Assets in $OUTPUT_DIR"
