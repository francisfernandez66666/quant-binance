// ── alerts.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：消息提醒读接口。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 消息提醒 ──
// ── Alerts ──

/** 获取所有提醒/告警消息 */
/** Fetch all reminder/alert messages */
// 对应 GET /api/alerts，返回消息数组；消息中心据此展示，未读数用于导航角标
// Maps to GET /api/alerts; returns an array of messages shown by the message center; unread count feeds the nav badge
export async function fetchAlerts() {
  return request('/api/alerts')
}

/** 清空消息中心全部消息 */
/** Clear all messages in the message center */
// 对应 DELETE /api/alerts，一次性删除所有提醒
// Maps to DELETE /api/alerts; deletes every alert at once
export async function clearAlerts() {
  return request('/api/alerts', { method: 'DELETE' })
}

/** 手工删除单条消息 */
/** Manually delete a single message */
// 对应 DELETE /api/alerts/{id}，id 经 encodeURIComponent 编码后拼入路径，
// Maps to DELETE /api/alerts/{id}; id is encoded with encodeURIComponent before being joined into the path,
// 防止特殊字符破坏 URL 结构
// so special characters cannot break the URL structure
export async function deleteAlert(id) {
  return request('/api/alerts/' + encodeURIComponent(id), { method: 'DELETE' })
}

/** §DAILY_REVIEW 手动触发当前账号盘后持仓 LLM 复盘，返回 {reviewed:N} */
/** §DAILY_REVIEW force-run the after-hours LLM position review for this account; returns {reviewed}. */
// 对应 POST /api/review/positions（同步执行，耗时数秒~数十秒；失败 502 抛错）。
export async function reviewPositions() {
  return request('/api/review/positions', { method: 'POST' })
}
