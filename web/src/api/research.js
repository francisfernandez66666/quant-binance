// ── research.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：研究面接口：B5 研究候选、战法库（已应用因子战法管理 + 效果监测）。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── B5 研究候选 ──
// ── B5 research candidates ──

/** 获取研究处理进度（GET /api/research/progress） */
/** Fetch the research processing progress (GET /api/research/progress) */
// 返回 { stocks, ready_stocks, ready_pct, daily_rows, fin_rows, candidates, applied, proposed, db_attached }
// Returns { stocks, ready_stocks, ready_pct, daily_rows, fin_rows, candidates, applied, proposed, db_attached }
export async function fetchResearchProgress() {
  return request('/api/research/progress')
}

/** 获取研究调度可见性快照（GET /api/scheduler/status） */
/** Fetch the scheduler visibility snapshot (GET /api/scheduler/status) */
// 返回 researchd 每 30s 写入的调度状态：enabled / beijing_now / in_trading_window /
// mem_avail_mb / mem_gate_open / busy / reason，用于前端直接解释"为何卡排队"。
// Returns researchd's 30s visibility snapshot so the UI can explain why tasks are queued.
export async function getSchedulerStatus() {
  return request('/api/scheduler/status')
}

/** 获取某条研究/回测任务的运行日志（GET /api/research/task/{id}/log） */
/** Fetch a research/backtest task's run log (GET /api/research/task/{id}/log) */
// 返回 { exists: bool, log: string }——researchd 把子进程输出写到 QUANT_DATA_DIR/task_logs/task_<id>.log，
// 前端弹窗直接展示，免去 SSH 翻服务器。Returns the per-task log so the UI can show it without server access.
export async function getResearchTaskLog(id) {
  return request('/api/research/task/' + id + '/log')
}

/** 获取 §ENH-4 事件因子检验报告（GET /api/research/event-factor） */
/** Fetch the event-factor validation report (news_score@stock/@sector IC/layer studies) */
// 正常返回 FactorReport 数组；未生成时返回 { exists: false }（cmd/research event-layers 落盘后才有）。
export async function fetchResearchEventFactor() {
  return request('/api/research/event-factor')
}

/** 获取全部因子元数据（GET /api/research/factors） */
/** Fetch factor metadata (GET /api/research/factors) */
// 返回 { factors: [{ id, name, cat, desc }, ...] }，供自动研究页把因子规则渲染成中文可读文案
// Returns { factors: [{ id, name, cat, desc }, ...] } so the auto-research page can render factor rules in Chinese
export async function fetchResearchFactors() {
  return request('/api/research/factors')
}

/** 获取研究候选列表（可选按状态过滤） */
/** Fetch the research candidate list (optionally filtered by status) */
// 对应 GET /api/research/candidates?status=...，返回 { candidates: [...] }
// Maps to GET /api/research/candidates?status=...; returns { candidates: [...] }
export async function fetchResearchCandidates(status) {
  const q = status ? '?status=' + encodeURIComponent(status) : ''
  return request('/api/research/candidates' + q)
}

/** 审批通过候选并应用权重（POST /api/research/candidates/{id}/approve） */
/** Approve a candidate and apply its weights (POST /api/research/candidates/{id}/approve) */
export async function approveResearchCandidate(id) {
  return request('/api/research/candidates/' + encodeURIComponent(id) + '/approve', { method: 'POST' })
}

/** 候选进入灰度观察（POST /api/research/candidates/{id}/grayscale，仅 factor/pattern） */
/** Grayscale a candidate for paper observation (POST /api/research/candidates/{id}/grayscale) */
export async function grayscaleResearchCandidate(id) {
  return request('/api/research/candidates/' + encodeURIComponent(id) + '/grayscale', { method: 'POST' })
}

/** 驳回候选（POST /api/research/candidates/{id}/reject） */
/** Reject a candidate (POST /api/research/candidates/{id}/reject) */
export async function rejectResearchCandidate(id) {
  return request('/api/research/candidates/' + encodeURIComponent(id) + '/reject', { method: 'POST' })
}

/** 对指定候选发起一次全量回测（POST /api/research/candidates/{id}/backtest，异步）。
 *  队列化改造（docs/RESEARCH_TASK_QUEUE_PLAN.md）：本接口只把 high 优先级任务写入
 *  research_tasks 队列即返回，由 researchd worker 在盘后窗口唯一执行——盘中提交会保持
 *  "queued" 状态排队，绝不进入交易时段。同候选已有排队/运行任务时幂等返回现态。
 *  params 可选 {start,end,top_k,min_stocks}：自定义回测时长与选股数（阶段3.3，透传执行参数）。
 *  English: enqueues a high-priority candidate backtest; the researchd worker runs it after hours.
 *  Same-ref duplicates return the existing task idempotently. Optional params pass through. */
// 对研究候选发起回测（可选参数覆盖）
export async function backtestResearchCandidate(id, params) {
  const qs = new URLSearchParams()
  if (params) {
    if (params.start) qs.set('start', params.start)
    if (params.end) qs.set('end', params.end)
    if (params.top_k) qs.set('top_k', params.top_k)
    if (params.min_stocks) qs.set('min_stocks', params.min_stocks)
  }
  const q = qs.toString()
  return request('/api/research/candidates/' + encodeURIComponent(id) + '/backtest' + (q ? '?' + q : ''), { method: 'POST' })
}

/** 取消回测任务（阶段3.2；队列化改造后语义扩展）：
 *  运行中 → worker kill 子进程并标 interrupted（断点缓存有效，可续跑）；
 *  排队中(queued) → 直接置 cancelled 终态（尚未开始执行，无断点概念）。
 *  English: cancel — running rows are killed+interrupted (checkpoints stay valid); queued rows are
 *  cancelled outright (never started). */
// 取消运行中的回测任务
export async function cancelBacktest(id) {
  return request('/api/research/backtest/' + encodeURIComponent(id) + '/cancel', { method: 'POST' })
}

/** 暂停回测子进程（SIGSTOP，任务标 paused）（阶段3.2） */
/** Pause a backtest child process (SIGSTOP; job marked paused) */
export async function pauseBacktest(id) {
  return request('/api/research/backtest/' + encodeURIComponent(id) + '/pause', { method: 'POST' })
}

/** 恢复已暂停的回测（SIGCONT，任务回到 running）（阶段3.2） */
/** Resume a paused backtest (SIGCONT; job back to running) */
export async function resumeBacktest(id) {
  return request('/api/research/backtest/' + encodeURIComponent(id) + '/resume', { method: 'POST' })
}

/** 战法库回测：对一条已应用规则（fac_<n>/pat_<n>）跑历史回放回测（阶段3.4，异步，结果进回测 tab）。
 *  params 可选 {start,end,maxstocks,quality}；quality=true 用质控池（剔 ST/退市/连亏/地量股） */
/** Library-rule backtest: replay one applied rule over history (async; result lands in the backtest tab) */
export async function backtestLibraryRule(id, params) {
  const qs = new URLSearchParams()
  if (params) {
    if (params.start) qs.set('start', params.start)
    if (params.end) qs.set('end', params.end)
    if (params.maxstocks) qs.set('maxstocks', params.maxstocks)
    if (params.quality) qs.set('quality', params.quality ? 'true' : 'false')
  }
  const q = qs.toString()
  return request('/api/research/library/' + encodeURIComponent(id) + '/backtest' + (q ? '?' + q : ''), { method: 'POST' })
}

/** 查询回测任务状态（GET /api/research/backtest/{id}） */
/** Query a backtest job's status (GET /api/research/backtest/{id}) */
export async function fetchBacktestStatus(id) {
  return request('/api/research/backtest/' + encodeURIComponent(id))
}

/** 查询运行中的回测任务列表（GET /api/research/backtest/running，页面刷新后恢复轮询） */
/** Fetch running backtest jobs (GET /api/research/backtest/running; used to resume polling after a refresh) */
export async function fetchRunningBacktests() {
  return request('/api/research/backtest/running')
}

/** 查询全部回测任务列表（GET /api/research/backtest/list，回测 tab 进度查看，含夜间全量） */
/** Fetch all backtest jobs (GET /api/research/backtest/list; backtest-tab progress view, includes nightly runs) */
export async function fetchAllBacktests() {
  return request('/api/research/backtest/list')
}

// ── 战法库（已应用因子战法管理 + 效果监测）──
// Strategy library: applied factor-strategy management + effectiveness monitoring

/** 获取战法库（GET /api/research/library） */
/** Fetch the strategy library (GET /api/research/library) */
export async function fetchResearchLibrary() {
  return request('/api/research/library')
}

/** 启用/禁用战法库某条（POST /api/research/library/{id}/enable|disable） */
/** Enable/disable a library strategy (POST .../{id}/enable|disable) */
export async function setResearchLibraryEnabled(id, enabled) {
  return request('/api/research/library/' + encodeURIComponent(id) + (enabled ? '/enable' : '/disable'), { method: 'POST' })
}

/** 删除战法库某条（POST /api/research/library/{id}/delete） */
/** Delete a library strategy (POST /api/research/library/{id}/delete) */
export async function deleteResearchLibrary(id) {
  return request('/api/research/library/' + encodeURIComponent(id) + '/delete', { method: 'POST' })
}

/** 重命名战法库某条（POST /api/research/library/{id}/rename，body {name}） */
/** Rename a library strategy (POST /api/research/library/{id}/rename, body {name}) */
export async function renameResearchLibrary(id, name) {
  return request('/api/research/library/' + encodeURIComponent(id) + '/rename', { method: 'POST', data: { name } })
}

/** 查询全量回测全局开关（GET /api/research/backtest-toggle） */
/** Get the global full-backtest toggle (GET /api/research/backtest-toggle) */
export async function fetchBacktestToggle() {
  return request('/api/research/backtest-toggle')
}

/** 设置全量回测全局开关（POST /api/research/backtest-toggle，body {enabled}） */
/** Set the global full-backtest toggle (POST /api/research/backtest-toggle, body {enabled}) */
export async function setBacktestToggle(enabled) {
  return request('/api/research/backtest-toggle', { method: 'POST', data: { enabled } })
}
