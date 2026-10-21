// ── signals.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：策略信号读接口。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 策略信号 ──
// ── Strategy signals ──

/** 获取所有策略信号列表 */
/** Fetch the full list of strategy signals */
// 对应后端 GET /api/signals，返回策略信号数组（含 code、action、score 等字段）
// Maps to backend GET /api/signals; returns an array of strategy signals (code, action, score, etc.)
export async function fetchSignals() {
  return request('/api/signals', { timeout: 20000 })
}

/** 获取个股分时数据（分时价格 + 成交量 + MACD） */
/** Fetch a stock's intraday (分时) data: price line + volume + MACD */
// 对应 GET /api/minute；code 必填，scale 分钟数（默认 1），count 点数（默认 241）；
// Maps to GET /api/minute; code is required, scale defaults to 1 minute, count defaults to 241;
// 返回 { code, name, prev_close, points: [{ time, open, high, low, close, volume, amount, dif, dea, bar }] }
// returns { code, name, prev_close, points: [{ time, open, high, low, close, volume, amount, dif, dea, bar }] }
export async function fetchMinute(code, scale, count) {
  // 守卫：code 为空或字符串 "undefined" 时直接抛清晰错误，避免把 undefined 打到后端造成「for undefined」
  if (!code || code === 'undefined') throw new Error('缺少股票代码，无法获取分时')
  const s = scale || 1
  const c = count || 241
  return request('/api/minute?code=' + encodeURIComponent(code) + '&scale=' + encodeURIComponent(s) + '&count=' + c)
}

/** 获取个股盘口快照（买卖五档 + 派生因子） */
/** Fetch a stock's order-book snapshot (5 bid/ask levels + derived factors) */
// 对应 GET /api/depth/{code}；返回 { code, name, price, prev_close, time, source, bids, asks, levels, factors }
// Maps to GET /api/depth/{code}; returns { code, name, price, prev_close, time, source, bids, asks, levels, factors }
// bids/asks 下标 0 为最优档（买一/卖一），长度 levels（免费源填充前五档，其余零值）
// bids/asks index 0 is the best level (bid1/ask1), length is `levels` (free source fills five, rest zero)
export async function fetchDepth(code) {
  // 守卫：同 fetchMinute，防止把 undefined 代码传给盘口接口
  if (!code || code === 'undefined') throw new Error('缺少股票代码，无法获取盘口')
  return request('/api/depth/' + encodeURIComponent(code), { timeout: 15000 })
}
