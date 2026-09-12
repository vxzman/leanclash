#!/bin/bash
# 开发运行：守护进程使用 dev/ 下 fixture（不触碰 /opt/leanclash 与 /etc/mihomo）。
# 前端开发另开终端: cd web && npm install && npm run dev（vite 代理到 :8081）
set -e
cd "$(dirname "$0")/.."
export LEANCLASH_CONFIG="$PWD/dev/manager.yaml"
export MIHOMO_MANAGER_CONFIG="$PWD/dev/manager.yaml"
exec go run . serve
