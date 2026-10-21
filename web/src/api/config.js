// ── config.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：各类配置读写：LLM 配置、战法参数、做空开关、资讯显示开关、数据源健康。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'
import { LLM_PROBE_TIMEOUT } from './shared.js'

// ── LLM 配置 ──
// ── LLM config ──

/** 获取 LLM 配置 */
/** Fetch the LLM configuration */
// 对应 GET /api/config/llm，返回 { api_url, api_key, model } 等配置项
// Maps to GET /api/config/llm; returns { api_url, api_key, model } config items
export async function fetchLLMConfig() {
  return request('/api/config/llm')
}

/** 设置 LLM 配置（API URL / Key / 模型名） */
/** Set the LLM configuration (API URL / Key / model name) */
// 对应 POST /api/config/llm。
// 后端语义：先探测候选配置能否真的调通 → 通过才切换运行时并落库；探测确认配置有错则 409 拒绝
// （运行时与磁盘都不动，error 字段里是可显示的原因）。返回 { status, result }。
// Maps to POST /api/config/llm; the backend probes the candidate first and only swaps/persists when
// it works, returning 409 (with a human-readable reason) when the config is provably broken.
export async function setLLMConfig(cfg) {
  // timeout 必须显式给：后端会先探测候选配置（详见 LLM_PROBE_TIMEOUT 注释）。
  return request('/api/config/llm', { method: 'POST', data: cfg, timeout: LLM_PROBE_TIMEOUT })
}

/** 测试 LLM 连接（只探测，不改任何状态） */
/** Probe the LLM channel (read-only: changes nothing) */
// 对应 POST /api/config/llm/probe。不带 cfg 时探测**当前已生效**的配置；
// 带 cfg 时探测候选配置（与 setLLMConfig 同形，支持脱敏哨兵回填）。
// 返回 { status, result: { verified, probes[], api_url, model, ... } }。
// Maps to POST /api/config/llm/probe — probes the live config when cfg is omitted.
export async function probeLLMConfig(cfg) {
  // §2026-09-20：这里曾走通用默认 10s —— 后端要 45~90s，于是"测试连接"必然报「请求超时」。
  // 详见 LLM_PROBE_TIMEOUT 的注释。
  return request('/api/config/llm/probe', { method: 'POST', data: cfg || {}, timeout: LLM_PROBE_TIMEOUT })
}

/** 回滚到上一个已验证可用的 LLM 配置 */
/** Roll back to the last verified-working LLM config */
// 对应 POST /api/config/llm/rollback。热更新翻车（例如强制应用了不可用的配置）时的兜底动作，
// 不必重启、不必回忆上次填了什么。返回 { status, result }。
// Maps to POST /api/config/llm/rollback — the escape hatch after a bad hot update.
export async function rollbackLLMConfig() {
  // 回滚同样会重跑一次探测（handleRollbackLLMConfig → applyLLMSnapshot），所以也要长超时。
  return request('/api/config/llm/rollback', { method: 'POST', data: {}, timeout: LLM_PROBE_TIMEOUT })
}

// ── 战法参数配置 ──
// ── Strategy parameter config ──

/** 获取四个战法参数（dragon/double_bump/n_shape/dragon_return） */
/** Fetch the four strategy parameter sets (dragon/double_bump/n_shape/dragon_return) */
// 对应 GET /api/config/strategy，返回各战法的阈值参数
// Maps to GET /api/config/strategy; returns each strategy's threshold parameters
export async function fetchStrategyConfig() {
  return request('/api/config/strategy')
}

/** 保存四个战法参数并持久化到 config.json */
/** Save the four strategy parameter sets and persist them to config.json */
// 对应 POST /api/config/strategy，cfg 为四个战法的阈值参数对象
// Maps to POST /api/config/strategy; cfg is the threshold parameter object for the four strategies
export async function setStrategyConfig(cfg) {
  return request('/api/config/strategy', { method: 'POST', data: cfg })
}

/** 获取 LLM 诊断调试数据 */
/** Fetch LLM diagnostic/debug data */
// 对应 GET /api/llm-debug，返回最近一次 LLM 决策的输入 / 输出，供 LLM 诊断页分析
// Maps to GET /api/llm-debug; returns the input / output of the latest LLM decision for the debug page
export async function fetchLLMDebug() {
  return request('/api/llm-debug')
}

/** 获取当日全量 LLM/Stage 轮次记录（固化到磁盘，供复盘） */
/** Fetch today's full LLM/Stage round records (persisted to disk, for review) */
// 对应 GET /api/stage-records，返回当日全部轮次快照，用于事后复盘
// Maps to GET /api/stage-records; returns all of today's round snapshots for later review
// §FIX-0921b 超时放宽到 30s（2026-09-01 实录）：LLM 修好后单轮含 110 条原始标题 + 数十条
// Stage2 事件（含完整理由），20 轮响应可达 ~700KB——弱网下 10s 默认超时会杀掉正常响应，
// 页面表现为「暂无数据」白板。复盘类接口放宽超时，宁可多等也不误判为空。
export async function fetchStageRecords() {
  return request('/api/stage-records', { timeout: 30000 })
}

/** 获取当日全量信号批次记录（固化到磁盘，供复盘） */
/** Fetch today's full signal batch records (persisted to disk, for review) */
// 对应 GET /api/signal-logs，返回当日各轮信号批次快照（做多/做空/提醒），用于信号日志弹窗
// Maps to GET /api/signal-logs; returns today's per-round signal batch snapshots (long/short/alerts) for the log modal
export async function fetchSignalLogs() {
  // §FIX-0921b 复盘类接口同样放宽 30s（信号批次含全量信号明细，弱网同理）
  return request('/api/signal-logs', { timeout: 30000 })
}

// ── 做空开关 ──
// ── Shorting toggle ──

/** 查询当前做空状态 */
/** Query the current shorting status */
// 对应 GET /api/short/status，返回 { short_enabled } 布尔值
// Maps to GET /api/short/status; returns the boolean { short_enabled }
export async function fetchShortStatus() {
  return request('/api/short/status')
}

/** 切换做空开关 */
/** Toggle the shorting switch */
// 对应 POST /api/short/toggle，请求体 { enabled } 传入目标开关状态，后端持久化
// Maps to POST /api/short/toggle; the body { enabled } carries the target state, persisted by the backend
export async function toggleShort(enabled) {
  return request('/api/short/toggle', { method: 'POST', data: { enabled } })
}

// ── 资讯显示全部开关 ──
// ── "Show all news" toggle ──

/** 查询"资讯显示全部"开关状态 */
/** Query the "show all news" toggle state */
// 对应 GET /api/news/showall，返回 { news_show_all } 布尔值
// Maps to GET /api/news/showall; returns the boolean { news_show_all }
export async function fetchNewsShowAllStatus() {
  return request('/api/news/showall')
}

/** 切换"资讯显示全部"开关 */
/** Toggle the "show all news" switch */
// 对应 POST /api/news/showall，请求体 { enabled }；开启时弱档/中性资讯也出现在 /api/news
// Maps to POST /api/news/showall; body { enabled }; when enabled, weak/neutral news also appears in /api/news
export async function toggleNewsShowAll(enabled) {
  return request('/api/news/showall', { method: 'POST', data: { enabled } })
}

// ── 数据源健康状况 ──
// ── Data source health ──
// 对应 GET /api/data_source_health，返回各数据源健康探测结果
// Maps to GET /api/data_source_health
// 返回 { eastmoney: true|false, sina: true|false, tencent: true|false, ths: true|false }
// (由 DataCoordinator.HealthCheck 返回，包含东财/新浪/腾讯/同花顺四大数据源的健康探测结果)
/**
 * 获取行情数据源健康探测结果 · 对应后端 GET /api/data_source_health
 */
export async function fetchDataSourceHealth() {
  return request('/api/data_source_health')
}

// 对应 GET /api/news_source_health，返回新闻源健康探测结果
// Maps to GET /api/news_source_health
// §M3（2026-09-22）后端已从布尔表升级为真实探测结构体，键名修正 cainanshe→cailanshe：
// 返回 { cailanshe|kuaixun|sina: { status: 'ok'|'down'|'unknown', last_success_at?,
// consecutive_errors, total_errors } }——unknown=从未探测，消费端不得当作健康布尔用。
// （Since M3 the payload is a per-source struct with a tri-state status; 'unknown' means
// never probed and must not be rendered as healthy.）
/**
 * 获取新闻源健康探测结果 · 对应后端 GET /api/news_source_health
 */
export async function fetchNewsSourceHealth() {
  return request('/api/news_source_health')
}
