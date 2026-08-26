#!/bin/sh
# 确保调试前端口 5500 已有静态服务器在监听。
# - 若端口已被占用（如 Live Server 正在运行）则直接复用，不做任何事。
# - 否则用 no_cache_server.py（响应带 Cache-Control: no-store）从工作区根目录(src)启动后台静态服务器。
# 用法: sh serve.sh [port]

PORT="${1:-5500}"
# 脚本位于 src/.vscode/scripts/serve.sh，向上两级即工作区根(src)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# 1) 端口已被占用 → 直接复用现有服务器（通常是 Live Server）
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "[cclawchat] port $PORT already in use - reusing existing server"
    exit 0
fi

# 2) 找不到 python → 提示先手动启动 Live Server
PY=""
for c in python3 python; do
    if command -v "$c" >/dev/null 2>&1; then
        PY="$c"
        break
    fi
done
if [ -z "$PY" ]; then
    echo "[cclawchat] python not found - cannot start static server. Start Live Server first (right-click index.html -> Open with Live Server)." >&2
    exit 1
fi

# 3) 后台启动静态服务器（与 VS Code 生命周期解耦，调试结束后仍可复用）
#    绑定 :: （IPv6 双栈）同时服务 IPv4/IPv6 的 localhost。
#    否则只绑 127.0.0.1 时，Safari 解析 localhost 优先走 IPv6 ::1 会连接失败一直转圈。
#    使用 no_cache_server.py：响应带 Cache-Control: no-store，避免浏览器缓存旧模块
#    （曾因缓存旧 api.js 导致聊天报 nonce invalid）。
nohup "$PY" "$SCRIPT_DIR/no_cache_server.py" "$PORT" >/dev/null 2>&1 &

# 4) 等待端口就绪（最多约 8 秒）
i=0
while [ "$i" -lt 80 ]; do
    if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
        echo "[cclawchat] static server ready on http://localhost:$PORT/cclawchat/index.html"
        exit 0
    fi
    sleep 0.1
    i=$((i + 1))
done

echo "[cclawchat] failed to start server on port $PORT" >&2
exit 1
