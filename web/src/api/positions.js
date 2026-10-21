// ── positions.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：持仓管理接口（含实盘/模拟盘仓位与对账，体量最大的一块）。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 持仓管理 ──
// ── Holdings ──

/** 获取当前持仓列表及可用资金 */
/** Fetch the current holdings list and available cash */
// 对应 GET /api/holdings，返回 { holdings: [...], available_cash } 等
// Maps to GET /api/holdings; returns { holdings: [...], available_cash } etc.
export async function fetchHoldings() {
  return request('/api/holdings')
}

/** 实盘（AUTO_TRADING_PLAN M1）：真实持仓（来自 QMT 网关回报的 real_positions） */
/** Live trading (AUTO_TRADING_PLAN M1): real holdings fed by the QMT gateway reports */
// 对应 GET /api/positions/real，返回 { positions:[...], enabled, tripped, mode }
export async function fetchRealPositions() {
  return request('/api/positions/real')
}

/** 实盘：持仓处理建议（加仓/减仓/止盈/止损） */
/** Live: position-handling advice (add/trim/take-profit/stop-loss) */
// 对应 GET /api/positions/advice，返回 { advices:[...], tripped }（主要数据走 SSE real_advice 实时推送）
export async function fetchRealAdvice() {
  return request('/api/positions/advice')
}

/** 实盘：执行 manual 下单（手动确认后的真实委托） */
/** Live: execute a manual order (real ticket after manual confirmation) */
// 对应 POST /api/positions/execute，body { code, side, action, qty, price, strategy, reason }
// §P2-15（2026-09-15）：封装层自动补 client_id 幂等键（UUID，后端 regClientID 校验格式）
// ——双击/网络重试同一次确认会带同一键，后端据此去重，不会重复下单；调用方显式传入时以调用方为准。
export async function executeRealAction(data) {
  const body = { ...data }
  if (!body.client_id && typeof crypto !== 'undefined' && crypto.randomUUID) {
    body.client_id = crypto.randomUUID()
  }
  return request('/api/positions/execute', { method: 'POST', data: body })
}

/** 实盘：互通健康快照（下行探测时延/上行回报新鲜度/熔断详情） */
/** Live: connectivity snapshot (downlink probe latency / uplink report freshness / breaker) */
// 对应 GET /api/qmt/state，返回 { enabled, mode, tripped, trip_reason, trip_at, gateway_url,
//   last_probe_at, last_probe_ok, last_latency_ms, last_report_at, last_report_kind }
export async function fetchQMTState() {
  return request('/api/qmt/state')
}

/** 实盘：网关 active 通道与双路径状态（§QMT-DUAL） */
/** Live: gateway active broker and dual-path liveness (miniQMT / QMT bridge) */
// 对应 GET /api/qmt/broker，返回 { ok, broker, broker_connected, xt_connected, queued_connected }
export async function fetchQMTBroker() {
  return request('/api/qmt/broker')
}

/** 实盘：切换网关 active 通道（仅 admin；xt=miniQMT 兼容 / queued=QMT 内置桥兜底） */
/** Live: switch gateway active broker (admin only) */
// 对应 POST /api/qmt/broker，body { broker: 'xt' | 'queued' }
export async function switchQMTBroker(broker) {
  return request('/api/qmt/broker', { method: 'POST', data: { broker } })
}

/** 实盘配置：当前账号的实盘参数与战法白名单（token 脱敏回显） */
/** Live config: account's trading params and strategy whitelist (token masked) */
// 对应 GET /api/config/qmt，返回 { enabled, mode, gateway_url, token_masked, price_type,
//   fixed_amount, max_positions, initial_capital, strategies, daily_max_buys,
//   daily_budget_amount, auto_sell, miss_heartbeat_sec, known_strategies, halted }
export async function fetchQMTConfig() {
  return request('/api/config/qmt')
}

/** 实盘配置保存：局部更新（仅传需要修改的字段，后端校验后进入待生效队列，交易时段自动生效；
 *  例外：显式携带 halted 属 kill-switch 语义（§U-3），立即生效不入队） */
/** Save live config: partial update — only send changed fields; backend validates then hot-reloads */
// 对应 POST /api/config/qmt；token 留空或传脱敏哨兵=保持原值
export async function updateQMTConfig(data) {
  return request('/api/config/qmt', { method: 'POST', data })
}

/** 实盘交易流水与整体盈亏（已实现/浮动/按战法归因） */
/** Live trade ledger and overall PnL (realized / unrealized / per-strategy attribution) */
// 对应 GET /api/qmt/trades，返回 { summary:{realized_pnl, unrealized_pnl, total_pnl,
//   trade_count, wins, losses}, by_strategy:[...], fills:[...最近100笔倒序] }
export async function fetchQMTTrades() {
  return request('/api/qmt/trades')
}

/** 实盘：当日委托列表（§U-2 撤单 UI 数据源；仅当日，含 order_id 供撤单） */
/** Live: today's order tickets (data source for the cancel UI) */
// 对应 GET /api/qmt/orders，返回 [{ order_id, signal_id, code, side, status, price, qty, created_at }]
export async function fetchQMTOrders() {
  return request('/api/qmt/orders')
}

/** 实盘 kill-switch：置位/解除人工紧急停止（§R4-1/§U-2；资损级，仅 admin） */
/** Live kill-switch: engage/release manual emergency halt (admin only) */
// 对应 POST /api/qmt/halt，body { halted: true|false }，返回 { ok, halted, cancelled }
export async function qmtHalt(halted) {
  return request('/api/qmt/halt', { method: 'POST', data: { halted } })
}

/** 实盘：手动撤单（§R4-1/§U-2，仅未成交委托可撤；已成交/已撤回 409） */
/** Live: manually cancel one order (unfilled only; terminal → 409) */
// 对应 POST /api/qmt/cancel/{order_id}
export async function qmtCancel(orderId) {
  return request('/api/qmt/cancel/' + encodeURIComponent(orderId), { method: 'POST', data: {} })
}

/** 实盘：触发交割单三方对账（§WS-B/§U-2，report_only 仅比对 / sync_fills 补记缺失） */
/** Live: trigger settlement reconciliation (report_only / sync_fills) */
// 对应 POST /api/qmt/settle，body { day?, mode? }，返回 { ok, day, mode, diff }
export async function qmtSettle(data = {}) {
  return request('/api/qmt/settle', { method: 'POST', data })
}

/** 实盘：交割单对账历史（§WS-B/§U-2，最近差异快照） */
/** Live: settlement reconciliation history (recent diffs) */
// 对应 GET /api/qmt/settle/history，返回 { ok, diffs:[{day,missing_in_local,extra_in_local,mismatch,...}] }
export async function fetchQMTSettleHistory() {
  return request('/api/qmt/settle/history')
}

/** 模拟盘：总开关与绩效/信号质量统计 */
/** Paper trading: master switch plus performance/signal-quality stats */
// 对应 GET /api/paper/state，返回 { enabled, stats:{...} }
// Maps to GET /api/paper/state; returns { enabled, stats:{...} }
export async function fetchPaperState() {
  return request('/api/paper/state')
}

/** 模拟盘：当前持仓（含实时估值价/浮盈/信号价参照） */
/** Paper trading: open positions (with live mark price, floating P/L and signal-price reference) */
// 对应 GET /api/paper/positions
export async function fetchPaperPositions() {
  return request('/api/paper/positions')
}

/** 模拟盘：成交记录（最新在前） */
/** Paper trading: fill records (newest first) */
// 对应 GET /api/paper/trades
export async function fetchPaperTrades() {
  return request('/api/paper/trades')
}

/** 模拟盘：订单生命周期记录（信号→订单→成交/拒绝 全留痕，最新在前） */
/** Paper order-lifecycle records (signal→order→outcome audit, newest first) */
// 对应 GET /api/paper/orders，返回当日全部订单生命周期留痕记录（最新在前）
export async function fetchPaperOrders() {
  return request('/api/paper/orders')
}

/** 模拟盘：净值曲线 */
/** Paper trading: equity curve */
// 对应 GET /api/paper/equity
export async function fetchPaperEquity() {
  return request('/api/paper/equity')
}

/** 模拟盘：自检诊断（持仓/成交/订单/净值是否为空 + paper.json 文件状态）
 *  对应 GET /api/paper/selfcheck，用于排查模拟盘"数据为空"的成因
 */
/** Paper trading: self-check diagnostics (positions/trades/orders/equity emptiness + paper.json file state).
 * 对应 GET /api/paper/selfcheck */
export async function fetchPaperSelfCheck() {
  return request('/api/paper/selfcheck')
}

/** §SIGNAL_CONTROLLER 模拟盘战法开关面板：GET /api/paper/strategies
 * 返回 { strategies, blacklist, shadow_blacklist, known_strategies:[{id,name,kind}] } */
export async function fetchPaperStrategies() {
  return request('/api/paper/strategies')
}

/** §SIGNAL_CONTROLLER 保存模拟盘战法白名单/黑名单：POST /api/paper/strategies
 * 空 strategies = 默认全集（内置四形态+已启用库规则；动量需显式开启） */
export async function updatePaperStrategies(strategies, blacklist) {
  return request('/api/paper/strategies', { method: 'POST', data: { strategies, blacklist } })
}

/** §F-4（20260917 缺陷修复批）模拟盘撮合配置（总开关/自动卖出/单笔资金/做空预算等，改后即热生效）
 * 对应 GET /api/paper/config：{enabled, auto_sell, fixed_amount, max_positions, initial_capital,
 *  short_enabled, short_capital, engine_enabled, engine_auto_sell} */
export async function fetchPaperConfig() {
  return request('/api/paper/config')
}

/** §F-4 局部更新撮合配置：POST /api/paper/config（只传要改的字段，未传字段保持原值） */
export async function updatePaperConfig(cfg) {
  return request('/api/paper/config', { method: 'POST', data: cfg })
}

/** §SIGNAL_CONTROLLER 信号控制器裁定留痕：GET /api/signalctl/verdicts?limit=N
 * 返回 { verdicts:[{channel,verdict,stage,code,strategy,reason,shadow,at}], count } */
export async function fetchSignalVerdicts(limit) {
  return request('/api/signalctl/verdicts?limit=' + (limit || 50))
}

/** §F-5（20260917 缺陷修复批）风控闸口状态：GET /api/risk/gates?day=YYYY-MM-DD（admin）。
 * 返回 { day, gates:[{user_id,gate,hits,last_reason,updated_at}], switches:{...}, time } */
export async function fetchRiskGates(day) {
  return request('/api/risk/gates' + (day ? '?day=' + encodeURIComponent(day) : ''))
}

/** §D-1（GAP_VERIFY_20260917_PM）夜间信号质量研究报告历史：GET /api/research/paper-reports?limit=N（admin）。
 * 返回 { reports:[{date,user_id,created_at,summary:{generated_at,trades,daily,attribution,emotion}}], count } */
export async function fetchPaperResearchReports(limit) {
  return request('/api/research/paper-reports?limit=' + (limit || 30))
}

/** §D-3（GAP_VERIFY_20260917_PM）规则配置快照列表：GET /api/config/history（admin）。
 * 返回 { snapshots:[{snapshot_ts,path}] }；?diff=TS 返回 { snapshot_ts, diff }（行级文本 diff） */
export async function fetchConfigHistory(diffTs) {
  return request('/api/config/history' + (diffTs ? '?diff=' + encodeURIComponent(diffTs) : ''))
}

/** §D-3 规则配置回滚：POST /api/config/rollback {snapshot_ts}（admin，原子恢复+审计） */
export async function rollbackConfig(snapshotTs) {
  return request('/api/config/rollback', { method: 'POST', data: { snapshot_ts: snapshotTs } })
}

/** §D-3 战法参数快照列表：GET /api/research/strategies/snapshots（auth）。
 * 返回 { snapshots:[{snapshot_ts,path}] } */
export async function fetchStrategySnapshots() {
  return request('/api/research/strategies/snapshots')
}

/** §D-3 战法参数回滚：POST /api/research/strategies/rollback {snapshot_ts}（admin） */
export async function rollbackStrategyParams(snapshotTs) {
  return request('/api/research/strategies/rollback', { method: 'POST', data: { snapshot_ts: snapshotTs } })
}

/** 模拟盘：手动买入（信号页"模拟买入"按钮触发）。qty>0 时按用户输入价格/数量成交（静态记账，
 *  qty 单位为股数——调用方负责手数×100 换算，见 §FIX-1(20260919)），price=0 回退实时价；
 *  qty<=0 回退固定金额整手（旧行为）。 */
/** Paper trading: manual buy (signal-page "paper buy" button). qty>0 fills the typed price/count in
 *  SHARES (callers convert lots ×100, see §FIX-1 20260919); price=0 falls back to the live quote;
 *  qty<=0 falls back to fixed-amount whole lots. */
// 对应 POST /api/paper/buy，data: { code, name, strategy, strategy_type, strategy_id, signal_price, price, qty }
// §C 归属字段：信号页模拟买入传原信号的 strategy_type/strategy_id，买入归入对应战法资金池；
// 纯手动（持仓页）不传 → 其他池（旧行为）。
export async function buyPaperPosition(code, name, strategy, signalPrice, price, qty, strategyType, strategyId) {
  return request('/api/paper/buy', {
    method: 'POST',
    data: {
      code, name, strategy,
      strategy_type: strategyType || '', strategy_id: strategyId || '',
      signal_price: signalPrice || 0, price: price || 0, qty: qty || 0,
    },
  })
}

/** 模拟盘：手动卖出。qty>0 时按指定数量减仓（股数，调用方手数×100 换算；price>0 用输入价，
 *  price=0 回退实时价；qty>=持仓=清仓），qty<=0 时按实时价清仓（旧行为）。 */
/** Paper trading: manual sell. qty>0 trims the typed count in SHARES (callers convert lots ×100;
 *  price>0 uses the typed price, price=0 falls back to the live quote; qty>=position closes it),
 *  qty<=0 closes at the live price (legacy). */
// 对应 POST /api/paper/sell，data: { code, price, qty }
export async function sellPaperPosition(code, price, qty) {
  return request('/api/paper/sell', { method: 'POST', data: { code, price: price || 0, qty: qty || 0 } })
}

/** §SHORT-4 模拟盘：融券买回平仓（price=0 走实时价）。返回 { ok, realized } */
// 对应 POST /api/paper/short_cover，data: { code, price }
export async function shortCoverPaper(code, price) {
  return request('/api/paper/short_cover', { method: 'POST', data: { code, price: price || 0 } })
}

/** 模拟盘：单池清盘（只清指定战法资金池的持仓与持久化表现，不影响其余池与全局净值/成交） */
/** Paper trading: reset a single strategy pool (clears only that pool's positions & persisted perf;
 *  other pools and the global equity/fill log are untouched) */
// 对应 POST /api/paper/pool/reset，data: { pool }
export async function resetPaperPool(pool) {
  return request('/api/paper/pool/reset', { method: 'POST', data: { pool: pool || '' } })
}

/** 模拟盘：分仓池级配置（全局持仓上限 + 每池持仓上限/资金分配，与全局资金/上限解耦可自定义）。
 *  总和守恒：Σ池资金=总现金，Σ池上限≤全局上限（前端校验）。 */
/** Paper trading: pool-level config (global position cap + per-pool caps/cash allocation, decoupled
 *  from the global capital/cap and customizable). Conservation: Σpool cash = total cash,
 *  Σpool caps ≤ the global cap (checked on the frontend). */
// 对应 POST /api/paper/pool/config，data: { max_positions, pool_caps, pool_rules, pool_allocs }
// §A3 新增 poolRules：每池买入纪律 {key:{max_daily_buys,cooldown_minutes,min_score,budget_pct_per_day}}；
// 传 {} 有语义（清空全部池规则）；null/undefined = 不触碰纪律设置。
export async function configPaperPools(maxPositions, poolCaps, poolAllocs, poolRules) {
  // §反馈解耦：各字段按"是否传入"独立生效——null=不触碰该类设置；
  // poolAllocs 传空对象 {} 是有语义的（显式清除自定义恢复均分），不能与 null 混淆。
  const data = {}
  if (maxPositions !== null && maxPositions !== undefined && maxPositions >= 0) {
    data.max_positions = maxPositions
  }
  if (poolCaps && Object.keys(poolCaps).length) data.pool_caps = poolCaps
  if (poolAllocs !== null && poolAllocs !== undefined) data.pool_allocs = poolAllocs
  if (poolRules !== null && poolRules !== undefined) data.pool_rules = poolRules
  return request('/api/paper/pool/config', { method: 'POST', data })
}

/** 模拟盘：清盘重置（按最后估值价平仓，重置现金/成交/净值；initialCapital>0 时自定义初始资金，
 *  maxPositions>0 时自定义持仓上限，0=不设限） */
/** Paper trading: liquidate and reset (liquidate at last mark, reset cash/trades/equity; a positive
 *  initialCapital also customizes the starting capital; a positive maxPositions sets a position cap,
 *  0 = unlimited) */
// 对应 POST /api/paper/reset
export async function resetPaper(initialCapital, maxPositions) {
  const data = {}
  if (initialCapital > 0) data.initial_capital = initialCapital
  if (maxPositions > 0) data.max_positions = maxPositions
  return request('/api/paper/reset', { method: 'POST', data })
}

/** 更新持仓数据（含可用资金） */
/** Update holdings data (including available cash) */
// 对应 POST /api/holdings，data 为完整持仓快照，整体覆盖保存
// Maps to POST /api/holdings; data is a full holdings snapshot saved as an overwrite
export async function updateHoldings(data) {
  return request('/api/holdings', { method: 'POST', data })
}

/** §P1-11（2026-09-15）仅更新可用资金 */
/** §P1-11: update available cash only — narrow endpoint, never touches the holdings list */
// 对应 POST /api/holdings/balance；与整表 saveHoldings 解耦，避免并发整表覆盖
// Maps to POST /api/holdings/balance; decoupled from the full-replace holdings sync
export async function updateHoldingsBalance(availableBalance) {
  return request('/api/holdings/balance', { method: 'POST', data: { available_balance: availableBalance } })
}

/** 增量买入/加仓：追加一笔(价格,数量)，后端按加权平均重算成本与累计数量 */
/** Incremental buy / add: append a lot (price, quantity); backend recalculates weighted-average cost and total quantity */
// 对应 POST /api/holdings/{code}/add，返回 { holding: {...} }
// Maps to POST /api/holdings/{code}/add; returns { holding: {...} }
export async function addHoldingLot(code, price, quantity) {
  return request('/api/holdings/' + encodeURIComponent(code) + '/add', { method: 'POST', data: { price, quantity } })
}

/** 更新成本价：直接设置该持仓成本（批次明细重建为一条合成批次） */
/** Update the cost price: set the holding cost directly (lot details are rebuilt as one synthetic lot) */
// 对应 POST /api/holdings/{code}/cost，返回 { holding: {...} }
// Maps to POST /api/holdings/{code}/cost; returns { holding: {...} }
export async function setHoldingCost(code, price) {
  return request('/api/holdings/' + encodeURIComponent(code) + '/cost', { method: 'POST', data: { price } })
}

/** 清仓：按清仓价卖出该持仓，后端计算并记录盈亏 */
/** Close out: sell the holding at the close price; backend computes and records the P&L */
// 对应 POST /api/holdings/{code}/close，返回 { profit_pct, profit_amount, ... }
// Maps to POST /api/holdings/{code}/close; returns { profit_pct, profit_amount, ... }
export async function closeHolding(code, price) {
  return request('/api/holdings/' + encodeURIComponent(code) + '/close', { method: 'POST', data: { price } })
}

/** 减仓：按卖出价卖出部分数量（FIFO 扣减批次，重算加权成本） */
/** Trim position: sell a partial quantity at the sell price (FIFO lot deduction, recalculated weighted cost) */
// 对应 POST /api/holdings/{code}/sell，返回 { holding: {...} }；全部卖出时返回 { closed: true }
// Maps to POST /api/holdings/{code}/sell; returns { holding: {...} }, or { closed: true } when fully sold
export async function sellHoldingLot(code, price, quantity) {
  return request('/api/holdings/' + encodeURIComponent(code) + '/sell', { method: 'POST', data: { price, quantity } })
}
