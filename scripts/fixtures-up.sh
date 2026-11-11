#!/usr/bin/env bash
# 启动三个合成夹具服务（旧界面 / 新界面 / 登录页），用于视觉巡检截图。
cd "$(dirname "$0")/.."
pkill -f "scripts/fixture-server.mjs" 2>/dev/null
sleep 0.3
nohup env DIST_DIR="$PWD/apps/web/dist" PORT=7301 FIXTURE_MODE=new node scripts/fixture-server.mjs > /tmp/fx-new.log 2>&1 &
nohup env DIST_DIR=/tmp/taboo-old/apps/web/dist-old PORT=7302 FIXTURE_MODE=old node scripts/fixture-server.mjs > /tmp/fx-old.log 2>&1 &
nohup env DIST_DIR="$PWD/apps/web/dist" PORT=7303 FIXTURE_MODE=login node scripts/fixture-server.mjs > /tmp/fx-login.log 2>&1 &
sleep 1
for p in 7301 7302 7303; do curl -s -o /dev/null -w "$p %{http_code}\n" "http://127.0.0.1:$p/"; done
