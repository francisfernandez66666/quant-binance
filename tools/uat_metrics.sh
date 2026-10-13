#!/usr/bin/env bash
# UAT 度量采集（2026-09-27）：vitest 用例数 / Go 全量覆盖率 / verify 剩余段
# 串行执行，避免并发 CPU 争用把测试喂成假红（本次已实测到该现象）。
set -u
export PATH="/usr/local/go/bin:/Users/zhangzifei/.workbuddy/binaries/node/versions/22.22.2-3/bin:/usr/bin:/bin:/usr/sbin:/sbin"
cd "$(dirname "$0")/.."

echo "===== 1) vitest 前端单测 ====="
( cd web && npx vitest run --reporter=json --outputFile=/tmp/vitest.json > /tmp/vitest.log 2>&1 )
echo "vitest exit=$?"
python3 - <<'PY'
import json
try:
    d = json.load(open('/tmp/vitest.json'))
    print("  numTotalTests  =", d.get('numTotalTests'))
    print("  numPassedTests =", d.get('numPassedTests'))
    print("  numFailedTests =", d.get('numFailedTests'))
    print("  numTotalTestSuites =", d.get('numTotalTestSuites'))
except Exception as e:
    print("  JSON 解析失败:", e)
PY
tail -8 /tmp/vitest.log

echo ""
echo "===== 2) Go 全量覆盖率 ====="
go test -count=1 -coverprofile=/tmp/cov.out -covermode=set ./... > /tmp/gocov.log 2>&1
echo "go test exit=$?"
go tool cover -func=/tmp/cov.out 2>/dev/null | tail -1
echo "  --- 逐包覆盖率（按覆盖升序，取最低 15 个）---"
go tool cover -func=/tmp/cov.out 2>/dev/null | awk -F'[ \t]+' '
  { p=$1; sub(/\..*/,"",p) }
  /%$/ { gsub(/%/,"",$NF); tot[p]+=1; if ($NF==0) zero[p]+=1 }
  END { for (k in tot) printf "%6.1f%% zero=%d %s\n", (zero[k]/tot[k])*100, zero[k], k }
' | sort -n | head -15
echo "  --- 失败包 ---"
/usr/bin/grep -E '^(FAIL|--- FAIL)' /tmp/gocov.log | head -20 || echo "  无失败"

echo ""
echo "===== 3) verify_changes.sh 剩余段（16/17，非 -full）====="
bash scripts/verify_changes.sh > /tmp/verify_rest.log 2>&1
echo "verify exit=$?"
echo "  段数 = $(/usr/bin/grep -c '^==>' /tmp/verify_rest.log)"
echo "  FAIL 命中 = $(/usr/bin/grep -c 'FAIL' /tmp/verify_rest.log)"
tail -6 /tmp/verify_rest.log
echo "DONE"
