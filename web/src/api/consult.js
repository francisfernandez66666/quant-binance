// ── consult.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：股票咨询（多轮对话）。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 股票咨询（多轮对话）──
// ── Stock consultation (multi-turn chat) ──

/** 发送咨询消息，获取 LLM 多轮对话回复 */
/** Send a consultation message and get the LLM multi-turn reply */
// 对应 POST /api/consult，请求体 { message }；返回 { reply }
// Maps to POST /api/consult; body { message }; returns { reply }
// LLM 推理耗时较长，单独使用 120s 超时避免提前中止
// LLM inference takes a while, so a dedicated 120s timeout avoids premature aborts
export async function consultChat(message) {
  return request('/api/consult', { method: 'POST', data: { message }, timeout: 120000 })
}

/** 获取当日咨询对话历史 */
/** Fetch today's consultation chat history */
// 对应 GET /api/consult/history，返回咨询消息数组
// Maps to GET /api/consult/history; returns an array of consultation messages
export async function fetchConsultHistory() {
  return request('/api/consult/history')
}

/** 清空当日咨询对话历史 · 对应后端 DELETE /api/consult/history
 *  Clear today's consultation chat history */
// 对应 DELETE /api/consult/history，清空当日咨询对话
// Maps to DELETE /api/consult/history; clears today's consultation messages
export async function clearConsultHistory() {
  return request('/api/consult/history', { method: 'DELETE' })
}

/** 获取专业模式开关状态 */
/** Fetch the pro-mode toggle state */
// 对应 GET /api/consult/pro-mode，返回 { enabled } 布尔值
// Maps to GET /api/consult/pro-mode; returns the boolean { enabled }
export async function fetchConsultProMode() {
  return request('/api/consult/pro-mode')
}

/** 切换专业模式开关 */
/** Toggle pro mode */
// 对应 PUT /api/consult/pro-mode，请求体 { enabled }；开启后咨询会注入全部实时行情。
// Maps to PUT /api/consult/pro-mode; body { enabled }; when enabled, consultations inject full realtime quotes.
// §FIX-9a(2026-09-19)：删除"盘中每 15 分钟限流一次"承诺——该限流已于 §生产 20260916
// 整体移除（带数据咨询改默认能力），请求路径仅保留 12 次/分钟的用户频控。
export async function setConsultProMode(enabled) {
  return request('/api/consult/pro-mode', { method: 'PUT', data: { enabled } })
}
