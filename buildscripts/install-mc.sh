#!/usr/bin/env bash
#
# Builds the MinIO client (mc) from source and installs it at the given
# path. Prebuilt community binaries are no longer published on dl.min.io.
#
# Usage: install-mc.sh [destination] [version]

set -e

dest="${1:-./mc}"
version="${2:-v0.0.0-20251106162529-77f82e18b540}"

gobin="$(mktemp -d)"
trap 'rm -rf "$gobin"' EXIT
GOBIN="$gobin" CGO_ENABLED=0 go install "github.com/minio/mc@${version}"
mv "$gobin/mc" "$dest"
