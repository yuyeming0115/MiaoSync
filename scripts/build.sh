#!/usr/bin/env bash
# MiaoSync 跨平台构建脚本
# 用法: ./scripts/build.sh
# 产物输出到 dist/（已在 .gitignore 中忽略）
set -euo pipefail

# 版本号：优先取 git tag，否则用 dev
VERSION="$(git describe --tags --always 2>/dev/null || echo dev)"
export GOFLAGS=-mod=mod

mkdir -p dist

echo "Building MiaoSync ${VERSION} ..."

# Windows：使用 -H windowsgui 隐藏控制台黑窗（GUI 子系统）
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X main.version=${VERSION}" -o dist/miaosync-windows-amd64.exe ./cmd/miaosync
# macOS (amd64, Intel)
GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=${VERSION}" -o dist/miaosync-darwin-amd64 ./cmd/miaosync
# macOS (arm64, Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=${VERSION}" -o dist/miaosync-darwin-arm64 ./cmd/miaosync
# Linux (amd64, 可选，便于服务器/NAS 部署)
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=${VERSION}" -o dist/miaosync-linux-amd64 ./cmd/miaosync

echo "Done. 产物见 dist/"
ls -la dist/

cat <<'NOTE'

---
macOS 打包为 .dmg（需在 Mac 上执行）：
  # 1) 把二进制放进 .app 结构
  mkdir -p MiaoSync.app/Contents/MacOS
  cp dist/miaosync-darwin-arm64 MiaoSync.app/Contents/MacOS/MiaoSync
  chmod +x MiaoSync.app/Contents/MacOS/MiaoSync
  # 2) 制成 dmg
  hdiutil create -volname MiaoSync -srcfolder MiaoSync.app -ov -format UDZO dist/MiaoSync.dmg
NOTE
