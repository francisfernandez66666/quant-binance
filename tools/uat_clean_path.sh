#!/usr/bin/env bash
# 干净 PATH 包装器（UAT 专用，2026-09-27）
#
# 背景：本机在命令执行环境下 PATH 被注入 WorkBuddy 的 shim 目录，其中的 grep 是
#   toybox 0.8.13（"is not GNU grep"）。toybox grep 不支持 BRE 的 `\|`、不支持 `\s`、
#   不支持 `-P`，且**静默返回 0 命中、不报错**——这会让"断言永远通过"或"永远失败"，
#   且现象与"被测代码坏了"完全一致。项目自带 verify_changes.sh 里有 441 处 grep -q 静态锁，
#   在 shim PATH 下**全部不可信**。
#
# 本包装器把 PATH 重置为可信路径（/usr/bin 优先于 shim），再 exec 目标命令。
set -u
export PATH="/usr/local/go/bin:/opt/homebrew/bin:/Users/zhangzifei/.workbuddy/binaries/node/versions/22.22.2-3/bin:/Users/zhangzifei/.workbuddy/binaries/python/versions/3.13.12/bin:/usr/local/node/bin:/usr/bin:/bin:/usr/sbin:/sbin"
cd "$(dirname "$0")/.."
if [ "${1:-}" = "--probe" ]; then
  echo "[clean] grep  = $(command -v grep) / $(grep --version 2>&1 | head -1)"
  echo "[clean] go    = $(command -v go) / $(go version 2>&1)"
  echo "[clean] node  = $(command -v node) / $(node -v 2>&1)"
  echo "[clean] python= $(command -v python3) / $(python3 -V 2>&1)"
  echo "[clean] curl  = $(command -v curl)"
  echo "[clean] 交替正则自检: $(printf 'a\nb\n' | grep -E 'a|b' | tr '\n' ',')"
  exit 0
fi
exec "$@"
