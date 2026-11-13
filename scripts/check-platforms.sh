#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
for target in darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do
  echo "Compile gate: $target (not runtime/hardware verification)"
  CGO_ENABLED=0 GOOS="${target%/*}" GOARCH="${target#*/}" go build ./...
done
