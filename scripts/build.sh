#!/usr/bin/env bash
# 生产构建：前端 → 嵌入 → Go 单二进制
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== 前端构建 =="
(cd web && npm ci && npx tsc --noEmit && npm run build)

echo "== 嵌入产物 =="
mkdir -p internal/webdist/dist
# 保留占位文件逻辑：先清空再复制（不删除目录本身以保留 embed 目标）
find internal/webdist/dist -mindepth 1 -delete
cp -r web/dist/* internal/webdist/dist/

echo "== 后端测试与构建 =="
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/xiaozhang ./cmd/xiaozhang

echo "== 完成: build/xiaozhang =="
