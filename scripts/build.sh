#!/usr/bin/env bash
# MiaoSync 跨平台构建脚本（骨架）
# 用法: ./scripts/build.sh
set -euo pipefail

VERSION="0.0.0"
LDFLAGS="-X main.version=${VERSION}"

echo "Building MiaoSync ${VERSION} ..."

# Windows (amd64)
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/miaosync-windows-amd64.exe ./cmd/miaosync
# macOS (amd64, Intel)
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/miaosync-darwin-amd64 ./cmd/miaosync
# macOS (arm64, Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o dist/miaosync-darwin-arm64 ./cmd/miaosync

echo "Done. 产物见 dist/"
