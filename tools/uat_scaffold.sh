#!/usr/bin/env bash
# UAT 探活脚手架（2026-09-27）
# 目的：在任何断言之前先证明「工具链真身 / 服务健康 / 数据可读写」三件事成立。
# 若这一步不通，后面所有 PASS/FAIL 都不可信。
set -u
export PATH="/usr/local/go/bin:/opt/homebrew/bin:/Users/zhangzifei/.workbuddy/binaries/node/versions/22.22.2-3/bin:/Users/zhangzifei/.workbuddy/binaries/python/versions/3.13.12/bin:/usr/bin:/bin:/usr/sbin:/sbin"
cd "$(dirname "$0")/.."

B="${E2E_API:-http://127.0.0.1:18080}"
F="${E2E_BASE_URL:-http://127.0.0.1:5173}"
M="${E2E_MOCK_URL:-http://127.0.0.1:18789}"

echo "[scaffold] grep = $(command -v grep) / $(grep --version 2>&1 | head -1)"
echo "[scaffold] go   = $(go version 2>&1)"
echo "[scaffold] 交替正则自检 = $(printf 'a\nb\n' | grep -E 'a|b' | tr '\n' ',')"
echo "[scaffold] -- 服务健康 --"
for n in "backend:$B/api/health" "frontend:$F/" "mock:$M/health"; do
  name="${n%%:*}"; url="${n#*:}"
  code=$(curl --noproxy '*' -s -m 10 -o /dev/null -w '%{http_code}' "$url" 2>/dev/null)
  echo "[scaffold]   $name -> http=$code"
done

echo "[scaffold] -- 登录与令牌 --"
LOGIN=$(curl --noproxy '*' -s -m 10 -X POST "$B/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Nightly!2026"}' 2>/dev/null)
echo "[scaffold]   login 响应前 200 字节: $(printf '%s' "$LOGIN" | head -c 200)"
TOK=$(printf '%s' "$LOGIN" | python3 -c "import sys,json
try:
    d=json.load(sys.stdin)
    print(d.get('token') or d.get('data',{}).get('token') or '')
except Exception: print('')")
echo "[scaffold]   token 长度 = ${#TOK}"
if [ -n "$TOK" ]; then
  echo "[scaffold]   /api/auth/me -> $(curl --noproxy '*' -s -m 10 -o /dev/null -w '%{http_code}' "$B/api/auth/me" -H "Authorization: Bearer $TOK")"
  echo "[scaffold]   /api/watchlist -> $(curl --noproxy '*' -s -m 10 -o /dev/null -w '%{http_code}' "$B/api/watchlist" -H "Authorization: Bearer $TOK")"
  echo "[scaffold]   /api/config/strategy -> $(curl --noproxy '*' -s -m 10 -o /dev/null -w '%{http_code}' "$B/api/config/strategy" -H "Authorization: Bearer $TOK")"
fi

echo "[scaffold] -- 数据库可直读 --"
for db in .uat-data/trading.db .uat-data/live.db; do
  if [ -f "$db" ]; then
    n=$(python3 -c "
import sqlite3,sys
try:
    c=sqlite3.connect('file:$db?mode=ro',uri=True)
    t=[r[0] for r in c.execute(\"SELECT name FROM sqlite_master WHERE type='table' ORDER BY name\")]
    print(f'{len(t)} tables: '+','.join(t[:12]))
except Exception as e: print('ERR '+str(e))
")
    echo "[scaffold]   $db -> $n"
  else
    echo "[scaffold]   $db -> 缺失"
  fi
done
echo "[scaffold] DONE"
