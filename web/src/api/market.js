// ── market.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：行情与资讯读接口：情绪历史/矩阵、板块热点、行情快照、个股评分、IPO 日历、个股查询、资讯、自选股。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

export async function fetchEmotionHistory(days) {
  return request('/api/market/emotion/history?days=' + (days || 30))
}

/** §Dashboard 情绪面板 B：情绪×战法回测矩阵（backtest_event_results 分相聚合）。
 *  返回 { phases, min_events, rows:[{candidate_id, kind, name, horizons, cells:[{phase,events,thin,avg_excess,hit_rate,avg_limit_up}], total_events}] }
 *  English: emotion × strategy backtest matrix, bucketed per-candidate by daily sentiment phase. */
export async function fetchEmotionStrategyMatrix() {
  return request('/api/research/emotion-strategy-matrix')
}

/**
 * 检查当前是否存在有效的登录令牌
 * Checks whether a login token currently exists
 * 说明：只判断 token 是否存在（非空），不校验其真实有效性；
 * Note: only checks the token's presence (non-empty), not its real validity;
 *       令牌是否过期由后端返回 401 时统一判定（见 request()）。
 *       actual expiry is judged when the backend returns 401 (see request()).
 * @returns {boolean} true 表示已登录
 * @returns {boolean} true if logged in
 */
// 是否已登录（存在 token 即视为已登录）
// ── 板块热点 ──
// ── Sector hotspots ──

/** 获取热门板块数据 */
/** Fetch hot sector data */
// 对应 GET /api/sector/hot，返回当前热门板块及其涨幅等
// Maps to GET /api/sector/hot; returns the current hot sectors and their gains etc.
export async function fetchSectorHot() {
  return request('/api/sector/hot')
}

/** 获取当日热点板块轮次记录（历史快照） */
/** Fetch today's hot sector rotation records (historical snapshots) */
// 对应 GET /api/sector/hot/records，返回各轮次板块快照，用于复盘
// Maps to GET /api/sector/hot/records; returns per-round sector snapshots for review
export async function fetchSectorHotRecords() {
  return request('/api/sector/hot/records')
}

// ── 行情快照 ──
// ── Market snapshot ──

/** 获取全市场行情快照 */
/** Fetch the whole-market quote snapshot */
// 对应 GET /api/snapshot，返回全市场标的的最新行情
// Maps to GET /api/snapshot; returns the latest quotes for all instruments
export async function fetchSnapshot() {
  return request('/api/snapshot')
}

/** 获取热门个股快照 */
/** Fetch the hot-stock snapshot */
// 对应 GET /api/snapshot/hot，返回热度较高的个股行情子集
// Maps to GET /api/snapshot/hot; returns a subset of quotes for high-turnover stocks
export async function fetchHotSnapshot() {
  return request('/api/snapshot/hot')
}

// ── 个股评分 ──
// ── Stock evaluations ──

/** 获取全市场个股多维度评分（N形/龙头/双凸/回头/动量） */
/** Fetch whole-market multi-dimension stock scores (N-shape / dragon / double-bump / dragon-return / momentum) */
// 对应 GET /api/evaluations，返回个股在各战法维度下的评分结果
// Maps to GET /api/evaluations; returns per-stock scores for each strategy dimension
export async function fetchEvaluations() {
  return request('/api/evaluations')
}

// ── IPO 日历 ──
// ── IPO calendar ──

// 本地缓存键：用于按天缓存 IPO 日历，减少不必要的后端请求
// Local cache key for caching the IPO calendar per day to reduce unnecessary backend requests
const IPO_CACHE_KEY = 'ipo_calendar_cache_v1'

/**
 * 获取 IPO 日历数据（按日缓存：同一天内首次调用才请求后端，其余直接读缓存）
 * Fetch IPO calendar data (cached per day: only the first call of a day hits the backend, later reads use the cache)
 * 原理：
 * How it works:
 *  - 以当天日期（YYYY-MM-DD，UTC 日期）作为缓存标识；
 *  - the current date (YYYY-MM-DD, UTC) is used as the cache key;
 *  - 命中缓存且数据结构合法则直接返回本地数据，避免重复请求；
 *  - a valid cached structure returns the local data directly, avoiding duplicate requests;
 *  - 未命中则请求 GET /api/ipo/calendar，成功后把当天日期与数据一并写回 localStorage。
 *  - otherwise it requests GET /api/ipo/calendar and writes back both the date and data to localStorage on success.
 */
// 打新日历：近两周新股（外部数据源，失败返回空）
export async function fetchIPOCalendar() {
  const today = new Date().toISOString().slice(0, 10)
  try {
    const raw = localStorage.getItem(IPO_CACHE_KEY)
    if (raw) {
      const d = JSON.parse(raw)
      // 命中当日缓存则直接返回，避免重复请求
      // Return the cached data when it matches today, avoiding a duplicate request
      if (d.date === today && Array.isArray(d.data)) return d.data
    }
  } catch (_) {}
  const data = await request('/api/ipo/calendar')
  try {
    // 拉取成功后按当天日期写入缓存
    // Write the cache under today's date after a successful fetch
    localStorage.setItem(IPO_CACHE_KEY, JSON.stringify({ date: today, data }))
  } catch (_) {}
  return data
}

// ── 个股查询 ──
// ── Stock lookup ──

/**
 * 根据代码查询个股信息（名称、现价等）
 * Look up stock info by code (name, current price, etc.)
 * 说明：code 经 encodeURIComponent 编码后作为查询参数拼入 URL，
 * Note: code is encoded with encodeURIComponent and appended as a query parameter,
 *       避免股票代码中的特殊字符干扰请求。
 *       so special characters in the code do not break the request.
 * @param {string} code - 股票代码
 * @param {string} code - the stock code
 * 对应后端 GET /api/stock/lookup?code=...，返回个股名称/现价等基础信息
 */
// 个股速查：行情快照（抽屉头部数据源）
export async function fetchStockLookup(code) {
  return request('/api/stock/lookup?code=' + encodeURIComponent(code))
}

// ── 资讯 ──
// ── News ──

/**
 * 获取新闻资讯
 * Fetch news
 * @param {boolean} [all] - 是否获取全部（含历史）资讯；true 时追加 ?all=true 查询参数
 * @param {boolean} [all] - whether to fetch all (including historical) news; appends ?all=true when true
 * 对应后端 GET /api/news（all=true 时追加 ?all=true，返回含历史的全部资讯）
 */
// 资讯列表（all=true 含未过滤条目）
export async function fetchNews(all) {
  return request(all ? '/api/news?all=true' : '/api/news')
}

/**
 * 手动 LLM 补推：强制重新拉取最近新闻并重跑 Stage0+Stage2 分析。
 * Manual LLM re-push: force a refetch of recent news and rerun the Stage0+Stage2 analysis.
 * 用于早盘/盘中 LLM 上游抖动导致整批新闻未被分析时的补救。
 * A remedy for when upstream LLM flakiness during the morning/intraday caused a whole batch to be left unanalyzed.
 * 异步执行，返回 202 表示已触发。
 * Runs asynchronously; a 202 response means it has been triggered.
 * 对应后端 POST /api/news/reanalyze
 */
// 手动触发一次新闻重新归因
export async function reanalyzeNews() {
  return request('/api/news/reanalyze', { method: 'POST' })
}

// ── 自选股 ──
// ── Watchlist ──

/** 获取自选股列表 */
/** Fetch the watchlist */
// 对应 GET /api/watchlist，返回自选股数组
// Maps to GET /api/watchlist; returns an array of watchlist stocks
export async function fetchWatchlist() {
  return request('/api/watchlist')
}

/** 添加自选股 */
/** Add a watchlist stock */
// 对应 POST /api/watchlist，请求体 { code } 传入股票代码
// Maps to POST /api/watchlist; the body { code } carries the stock code
export async function addWatchlist(code) {
  return request('/api/watchlist', { method: 'POST', data: { code } })
}

/** 移除自选股 */
/** Remove a watchlist stock */
// 对应 DELETE /api/watchlist，请求体 { code } 传入股票代码
// Maps to DELETE /api/watchlist; the body { code } carries the stock code
export async function removeWatchlist(code) {
  return request('/api/watchlist', { method: 'DELETE', data: { code } })
}
