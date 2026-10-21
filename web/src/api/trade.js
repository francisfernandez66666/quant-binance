// ── trade.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：交易动作接口 + 市场时段追踪（非交易时段仅首次加载）。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 交易动作 ──
// ── Trade actions ──

/** 对某信号执行买入 / 忽略操作 */
/** Perform a buy / ignore action on a signal */
// 对应 POST /api/action，请求体 { code, action, strategy }；
// Maps to POST /api/action; the body is { code, action, strategy };
// action 取 'buy'（买入）或 'ignore'（忽略，§F-1：带战法定位墓碑，绝不触达实盘下单通道）
// action is 'buy' or 'ignore' (§F-1: ignore carries the strategy key for tombstoning and can never place orders)
export async function actionSignal(code, action, strategy) {
  return request('/api/action', { method: 'POST', data: { code, action, strategy: strategy || '' } })
}

// ── 市场时段追踪（非交易时段仅首次加载） ──
// ── Market session tracking (first load only in non-trading hours) ──
// 原理：后端按市场会话（session）推送数据，前端以模块级变量 _lastSession
// How it works: the backend pushes data per market session; the frontend keeps the last consumed
// 记录最近一次消费的会话期号。当会话号发生变化（进入新的一天或新交易时段）时，
// session number in the module-level _lastSession. When the session number changes (a new day or new trading session),
// 视为“新会话”，用于控制在非交易时段内只做一次数据加载，避免重复请求。
// it counts as a "new session" and limits non-trading hours to a single load, avoiding duplicate requests.

// 模块级会话期号缓存：-1 表示尚未记录任何会话
// Module-level cached session number: -1 means no session recorded yet
let _lastSession = -1

/** 获取上次记录的会话期号 */
/** Get the previously recorded session number */
export function getLastSession() { return _lastSession }

/** 设置当前会话期号 */
/** Set the current session number */
export function setLastSession(s) { _lastSession = s }

/** 判断当前会话是否为不同于上次的新会话 */
/** Whether the given session is new compared with the last one */
// 原理：与会话号不一致则为新会话，并同步更新记录；相同则返回 false
// How it works: a different session number is a new session and updates the record; identical numbers return false
export function isNewSession(session) {
  if (session === _lastSession) return false
  _lastSession = session
  return true
}

/** 判断某会话是否为交易时段（早盘=1，午盘=3） */
/** Whether a session is a trading session (morning=1, afternoon=3) */
// 说明：session 为后端约定的枚举值，1 表示早盘交易时段、3 表示午盘交易时段
// Note: session is a backend-defined enum; 1 = morning trading session, 3 = afternoon trading session
export function isTradingSession(session) {
  return session === 1 || session === 3 // SessionMorningTrade=1, SessionAfternoonTrade=3
}
