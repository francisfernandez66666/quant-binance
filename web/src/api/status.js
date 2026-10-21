// ── status.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：系统状态读接口。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 系统状态 ──
// ── System status ──

/** 获取服务端运行状态（含扫描统计、运行时长等） */
/** Fetch the server runtime status (scan stats, uptime, etc.) */
// 对应 GET /api/status，返回 { signal_count, in_trade_time, ... }，
// Maps to GET /api/status; returns { signal_count, in_trade_time, ... },
// 顶部状态栏与 60s 状态轮询（App.refreshStatus）均依赖该接口
// used by the top status bar and the 60s status poll (App.refreshStatus)
export async function fetchStatus() {
  return request('/api/status')
}

// 获取仪表盘汇总数据（含按战法分组的胜率统计）
// 对应 GET /api/dashboard，返回 { report_stats: { by_strategy: {...} }, ... }；
// by_strategy 按战法聚合胜率/平均盈亏/盈亏比，用于绩效归因展示。
/**
 * 获取仪表盘汇总数据 · 对应后端 GET /api/dashboard
 */
export async function fetchDashboard() {
  return request('/api/dashboard')
}

// 获取流程引擎子系统健康状况
// Fetch engine health status
// 对应 GET /api/engine_health，返回各子系统连通性
// Maps to GET /api/engine_health, returns connectivity of each subsystem
/**
 * 获取流程引擎子系统健康状况 · 对应后端 GET /api/engine_health
 */
export async function fetchEngineHealth() {
  return request('/api/engine_health')
}
