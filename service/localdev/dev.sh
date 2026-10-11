#!/usr/bin/env bash
# 本地联调脚本：只连本机 Docker 里的 MySQL / Redis，绝不会连线上。
#
#   ./localdev/dev.sh bootstrap 一键：up → migrate → start → seed（首次使用，顺序已排好）
#   ./localdev/dev.sh up        启动 MySQL + Redis 容器并等待就绪
#   ./localdev/dev.sh migrate   执行授权表 / CRM 来源接入表迁移（必须在首次 start 之前，可重复执行）
#   ./localdev/dev.sh start     编译并后台启动后端（按天日志：service/APPlog/，stdout：/tmp/jdlg-local-service.log）
#   ./localdev/dev.sh seed      写入虚构的管理账号 / 用户 / 订单 / 模板（需先 start；拒绝非本机库）
#   ./localdev/dev.sh stop      停止后端
#   ./localdev/dev.sh down      停止容器（保留数据）
#   ./localdev/dev.sh reset     停止容器并删除本地数据卷
#
# 隔离措施：后端以 env -i 启动（不继承当前 shell 的任何环境变量），
# 并通过 JDLG_LOCAL_ENV_FILE 只加载 localdev/local.env，不会读取 service/.env.local。
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE=(docker compose -f localdev/docker-compose.yml)
BIN=/tmp/jdlg-local-service
LOG=/tmp/jdlg-local-service.log
PIDFILE=/tmp/jdlg-local-service.pid
ENVFILE="$PWD/localdev/local.env"
ISOLATED=(env -i HOME="$HOME" PATH="$PATH" CGO_ENABLED=0 JDLG_LOCAL_ENV_FILE="$ENVFILE")

case "${1:-}" in
  up)
    "${COMPOSE[@]}" up -d --wait
    echo "MySQL 127.0.0.1:13306 / Redis 127.0.0.1:16379 已就绪"
    ;;
  start)
    CGO_ENABLED=0 go build -o "$BIN" .
    [ -f "$PIDFILE" ] && kill "$(cat "$PIDFILE")" 2>/dev/null || true
    nohup "${ISOLATED[@]}" "$BIN" >"$LOG" 2>&1 &
    echo $! >"$PIDFILE"
    for _ in $(seq 1 60); do
      if curl -fsS http://127.0.0.1:28180/ping >/dev/null 2>&1; then
        echo "后端已就绪：http://127.0.0.1:28180（pid $(cat "$PIDFILE")）"
        exit 0
      fi
      sleep 1
    done
    echo "后端未能在 60 秒内就绪。服务把日志写在 APPlog/ 下的按天文件里（stdout 为空是正常的），末尾：" >&2
    tail -15 "$(ls -t APPlog/*.log 2>/dev/null | head -1)" >&2 || true
    exit 1
    ;;
  migrate)
    "${ISOLATED[@]}" go run . migrate customer_management_bridge_up.sql customer_lead_crm_delivery_up.sql
    ;;
  seed)
    "${ISOLATED[@]}" go run ./cmd/localseed
    ;;
  stop)
    [ -f "$PIDFILE" ] && kill "$(cat "$PIDFILE")" 2>/dev/null && rm -f "$PIDFILE" && echo "后端已停止" || echo "后端未在运行"
    ;;
  down)
    "${COMPOSE[@]}" down
    ;;
  reset)
    "${COMPOSE[@]}" down -v
    ;;
  bootstrap)
    "$0" up
    "$0" migrate
    "$0" start
    "$0" seed
    ;;
  *)
    sed -n 2,14p "$0"
    exit 2
    ;;
esac
