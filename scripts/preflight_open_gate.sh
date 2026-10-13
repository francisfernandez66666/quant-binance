#!/usr/bin/env bash
# ── §FIX1012-M2 开闸前置检查单（双发布闸：binance / cn）──────────────────────────
# 背景（审计 AUDIT_FULL_BYTELEVEL_UAT_20261012 重新定性的 P0）：系统开箱下不了一单真单——
# rules.binance.enabled 出厂 false + Testnet true（binance.go:144），rules.cn.enabled 出厂
# false（config.go:177，cn_master_test.go:4「用户指令：默认关闭 A股相关东西」）。这是优秀的
# fail-safe 设计（controller.go:205 四条件缺一即回退 NoopExecutor，真单结构上发不出），
# **不是缺陷**；但「没有一条市场在役」的商业含义是 P0——开闸必须是一次有检查单的灰度动作，
# 而不是改一行配置重启。
#
# 本脚本把开闸前置条件写成可执行检查：逐项 PASS/FAIL，任一 FAIL 即不允许开闸。
# 代码只做「可检查」，真开闸由人执行——本脚本永不改配置、永不重启服务。
#
# 用法：
#   bash scripts/preflight_open_gate.sh --market binance            # 开闸前强制门（有 FAIL 退出 1）
#   bash scripts/preflight_open_gate.sh --market binance --dry-run  # 只体检不拦截（列缺口，恒退出 0）
#   bash scripts/preflight_open_gate.sh --market cn --attest-e2e "2026-10-12 nightly #42"
#     --attest-e2e   testnet 全链 E2E 已通过的凭证（run id/日期）；缺它即 FAIL——脚本无法自证
#                   「e2e 跑过」，这一项必须人显式确认（可用 uat_bootstrap.sh run 复现）。
#     --mainnet     灰度结束后切主网的显式确认；不传时 Testnet 必须为 true（灰度期硬要求）。
#
# 数据目录解析与 cmd/quant/main.go getDataDir() 逐字对齐：QUANT_ENV=staging 强制
# ~/.quant-staging（忽略 QUANT_DATA_DIR），否则 QUANT_DATA_DIR 优先、默认 ~/.quant-trading-v2。
# English: pre-flight checklist before enabling a live market (binance/cn). Reports PASS/FAIL
# per item and exits 1 on any FAIL (unless --dry-run). Never mutates config or restarts anything.
set -euo pipefail
cd "$(dirname "$0")/.."

MARKET=""
DRY_RUN=0
ATTEST_E2E=""
ALLOW_MAINNET=0
while [ $# -gt 0 ]; do
	case "$1" in
		--market) MARKET="${2:-}"; shift 2 ;;
		--dry-run) DRY_RUN=1; shift ;;
		--attest-e2e) ATTEST_E2E="${2:-}"; shift 2 ;;
		--mainnet) ALLOW_MAINNET=1; shift ;;
		*) echo "用法: $0 --market binance|cn [--dry-run] [--attest-e2e <证据>] [--mainnet]"; exit 2 ;;
	esac
done
if [ "$MARKET" != "binance" ] && [ "$MARKET" != "cn" ]; then
	echo "--- FAIL: --market 必须为 binance 或 cn（双发布闸分列，防止一份检查单两市场混用）"; exit 2
fi

# ── 定位 config.json（与 getDataDir 同口径）────────────────────────────────────
if [ "${QUANT_ENV:-}" = "staging" ]; then
	DATA_DIR="$HOME/.quant-staging"
elif [ -n "${QUANT_DATA_DIR:-}" ]; then
	DATA_DIR="$QUANT_DATA_DIR"
else
	DATA_DIR="$HOME/.quant-trading-v2"
fi
CFG="$DATA_DIR/config.json"

PASS_N=0; FAIL_N=0
ok()  { echo "  [PASS] $1"; PASS_N=$((PASS_N+1)); }
bad() { echo "  [FAIL] $1"; FAIL_N=$((FAIL_N+1)); }
note(){ echo "  [NOTE] $1"; }
skip(){ echo "  [SKIP] $1（本市场不适用）"; }

if [ ! -f "$CFG" ]; then
	echo "--- FAIL: 配置文件不存在：$CFG（先跑起过一次服务生成 config.json，或用 QUANT_DATA_DIR 指到真实数据目录）"
	exit 1
fi
echo "==> §FIX1012-M2 开闸检查单 market=${MARKET} cfg=${CFG} dry_run=${DRY_RUN}"

# ── 用 python3 读 JSON（本机与 CI 都有；项目 py_tests 同款能力探测不适用——这里是硬依赖）──
read_cfg() { # read_cfg <python 表达式，变量 c 为解析后的 dict，json/len/str 可用>
	python3 - "$CFG" "$1" <<'PY'
import json, sys
cfg_path, expr = sys.argv[1], sys.argv[2]
with open(cfg_path, encoding='utf-8') as f:
    c = json.load(f)
print(eval(expr, {'c': c, 'json': json, 'len': len, 'str': str}))
PY
}

# 占位/弱凭证词表：长度够但内容是样例的一律算未配置（防 "testtesttesttesttest" 混过长度闸）
is_placeholder() {
	case "$1" in
		""|xxx*|XXX*|test*|Test*|dummy*|Dummy*|placeholder*|change*|Change*|your_*|YOUR_*|sk-*|"<"*) return 0 ;;
	esac
	return 1
}

# ── 检查 1：目标市场总开关已置 true ────────────────────────────────────────────
if [ "$MARKET" = "binance" ]; then
	ENABLED=$(read_cfg "bool(c.get('rules', {}).get('binance', {}).get('enabled', False))")
	if [ "$ENABLED" = "True" ]; then
		ok "rules.binance.enabled = true（总开关已开）"
	else
		bad "rules.binance.enabled 仍为 false——开闸第一步就是把总开关置 true 并重启生效"
	fi
else
	ENABLED=$(read_cfg "bool(c.get('rules', {}).get('cn', {}).get('enabled', False))")
	if [ "$ENABLED" = "True" ]; then
		ok "rules.cn.enabled = true（A股总开关已开）"
	else
		bad "rules.cn.enabled 仍为 false——注意 §CN-FREEZE 是 owner 冻结维护裁决，开闸需显式解冻"
	fi
fi

# ── 检查 2：凭证齐备且非占位 ─────────────────────────────────────────────────
if [ "$MARKET" = "binance" ]; then
	API_KEY=$(read_cfg "str(c.get('rules', {}).get('binance', {}).get('api_key', '') or '')")
	API_SECRET=$(read_cfg "str(c.get('rules', {}).get('binance', {}).get('api_secret', '') or '')")
	CRED_NAME="binance api_key/api_secret"
else
	API_KEY=$(read_cfg "str(c.get('rules', {}).get('qmt', {}).get('token', '') or '')")
	API_SECRET=$(read_cfg "str(c.get('rules', {}).get('qmt', {}).get('gateway_url', '') or '')")
	CRED_NAME="qmt token/gateway_url"
fi
if [ "${#API_KEY}" -lt 16 ] || is_placeholder "$API_KEY"; then
	bad "${CRED_NAME} 缺失或是占位符（长度 ${#API_KEY} <16 或命中样例词表）——真执行器构造硬条件，缺一即 NoopExecutor"
elif [ "${#API_SECRET}" -lt 16 ] || is_placeholder "$API_SECRET"; then
	bad "${CRED_NAME} 第二值缺失或是占位符（长度 ${#API_SECRET} <16 或命中样例词表）"
else
	ok "${CRED_NAME} 齐备且非占位（key 长度 ${#API_KEY} / 第二值长度 ${#API_SECRET}）"
fi

# ── 检查 3：Testnet 切换状态（灰度期必须 true；切主网需显式 --mainnet）──────────
if [ "$MARKET" = "binance" ]; then
	TESTNET=$(read_cfg "bool(c.get('rules', {}).get('binance', {}).get('testnet', False))")
	if [ "$TESTNET" = "True" ]; then
		ok "rules.binance.testnet = true（灰度期口径正确）"
	elif [ "$ALLOW_MAINNET" -eq 1 ]; then
		ok "rules.binance.testnet = false 且显式传了 --mainnet（主网确认已签署）"
	else
		bad "rules.binance.testnet = false 但未传 --mainnet——灰度期硬要求 testnet=true；确认切主网请显式加 --mainnet"
	fi
else
	skip "A股无 testnet 概念（QMT 链路为准生产通道，凭证检查已覆盖）"
fi

# ── 检查 4：风控闸非零（AnyEnabled 三件套均已设）─────────────────────────────
# 与 config.go:598 AnyEnabled 同口径；另按施工单硬性要求 max_order_amount /
# day_loss_limit_pct / single_stock_value_pct 三件套都必须显式设值（只开一道不算配齐）。
RISK_JSON=$(read_cfg "json.dumps(c.get('rules', {}).get('qmt', {}).get('risk_gate', {}) or {}, ensure_ascii=False)")
RISK_SUM=$(python3 - "$RISK_JSON" <<'PY'
import json, sys
r = json.loads(sys.argv[1])
any_enabled = (r.get('day_loss_limit_pct', 0) > 0 or r.get('single_stock_value_pct', 0) > 0
               or r.get('stale_quote_ms', 0) > 0 or r.get('limit_up_block_buy', False)
               or r.get('limit_down_block_sell', False) or r.get('max_order_amount', 0) > 0
               or r.get('cross_check_pct', 0) > 0)
trio = (r.get('max_order_amount', 0) > 0, r.get('day_loss_limit_pct', 0) > 0, r.get('single_stock_value_pct', 0) > 0)
print(('ANY=1' if any_enabled else 'ANY=0'), ('TRIO=1' if all(trio) else 'TRIO=0'), 'MAX=%s' % r.get('max_order_amount', 0),
      'DAY=%s' % r.get('day_loss_limit_pct', 0), 'STK=%s' % r.get('single_stock_value_pct', 0))
PY
)
ANY_ON=$(echo "$RISK_SUM" | /usr/bin/sed -n 's/.*ANY=\([01]\).*/\1/p')
TRIO_ON=$(echo "$RISK_SUM" | /usr/bin/sed -n 's/.*TRIO=\([01]\).*/\1/p')
if [ "$ANY_ON" != "1" ]; then
	bad "风控闸全关（rules.qmt.risk_gate 零值）——开闸前至少开一道（AnyEnabled=false）"
elif [ "$TRIO_ON" != "1" ]; then
	bad "风控三件套未配齐：max_order_amount / day_loss_limit_pct / single_stock_value_pct 必须均 >0（当前 ${RISK_SUM#* }）"
else
	ok "风控闸在位：${RISK_SUM#* }"
fi

# ── 检查 5：杠杆确认签署（enabled 且存在 leverage>1 时必须先签）────────────────
if [ "$MARKET" = "binance" ]; then
	LEV_JSON=$(read_cfg "json.dumps({'ack': c.get('rules', {}).get('binance', {}).get('leverage_acknowledged_at', '') or '', 'stock': c.get('rules', {}).get('binance', {}).get('stock', {}).get('leverage', 0) or 0, 'spot': c.get('rules', {}).get('binance', {}).get('spot', {}).get('leverage', 0) or 0}, ensure_ascii=False)")
	LEV_SUM=$(python3 - "$LEV_JSON" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
need = d['stock'] > 1 or d['spot'] > 1
print('NEED=1' if need else 'NEED=0', 'ACK=%s' % ('yes' if d['ack'] else 'no'),
      'stock_lev=%s spot_lev=%s' % (d['stock'], d['spot']))
PY
)
	NEED_ACK=$(echo "$LEV_SUM" | /usr/bin/sed -n 's/.*NEED=\([01]\).*/\1/p')
	if [ "$NEED_ACK" = "1" ]; then
		if echo "$LEV_SUM" | /usr/bin/grep -q 'ACK=yes'; then
			ok "杠杆确认已签署（${LEV_SUM#* }）"
		else
			bad "存在 leverage>1 但 leverage_acknowledged_at 为空（§MR-4B 红线：合约权限/爆仓风险须人工显式确认后才准开）"
		fi
	else
		note "无 leverage>1 的 profile（${LEV_SUM#* }），杠杆确认签署不适用"
	fi
else
	skip "A股无杠杆配置面（两融走券商侧，不在本仓配置域）"
fi

# ── 检查 6：披露签署 ─────────────────────────────────────────────────────────
if [ "$MARKET" = "binance" ]; then
	DISC=$(read_cfg "str(c.get('rules', {}).get('binance', {}).get('disclaimer_signed_at', '') or '')")
	if [ -n "$DISC" ]; then
		ok "disclaimer_signed_at 已签（$DISC）"
	else
		bad "disclaimer_signed_at 为空——美股/加密披露签署是开闸前置（与 leverage 确认同族语义）"
	fi
else
	skip "A股披露签署位在 binance 段；CN 开闸以 qmt.token 检查与灰度记录为准"
fi

# ── 检查 7：testnet 全链 E2E 已通过（必须人显式举证，脚本无法自证）─────────────
if [ -n "$ATTEST_E2E" ]; then
	ok "testnet 全链 E2E 通过已举证：$ATTEST_E2E"
else
	bad "缺 --attest-e2e 证据——先跑 bash scripts/uat_bootstrap.sh run（mock 保真五修口径）并把 run id/日期回传； scripts 无法自证「e2e 跑过」"
fi

# ── 汇总 ─────────────────────────────────────────────────────────────────────
echo "==> 检查单汇总：PASS=${PASS_N} FAIL=${FAIL_N} SKIP/NOTE 见上"
if [ "$FAIL_N" -gt 0 ]; then
	if [ "$DRY_RUN" -eq 1 ]; then
		echo "--- DRY-RUN：${FAIL_N} 项未过（不拦截）。逐项补齐后重跑（去掉 --dry-run 即成为开闸强制门）。"
		exit 0
	fi
	echo "--- FAIL: 开闸检查单 ${FAIL_N} 项未过——按上表逐项补齐前不允许开闸。"
	exit 1
fi
echo "ok - 开闸检查单全部通过（market=${MARKET}）"
if [ "$DRY_RUN" -eq 1 ]; then note "dry-run 模式：即使全部通过也未执行任何开闸动作（本脚本永不改配置）"; fi
note "灰度门禁（施工单步骤 2）：开闸后首周只允许 fixed_amount 最小档 + max_positions=1 + daily_max_buys=1，观察 settlement_diff 三方对账费用差腿为零再放宽。"
exit 0
