// ── 币安接入 fetch 层 api/binance.js ──
// 文件职责（PLAN_BINANCE_MULTI_ASSET_20260922 §7 端点表）：全站唯一的币安/新市场 HTTP 出口，
// 路径集中在此文件——后端 /api/binance/* 家族由主链路并行建设，若最终路径/方法有出入，
// 只需改本模块，页面零改动。
// 已上线端点（internal/server/binance_config.go，镜像 /api/config/qmt）：
//   GET  /api/config/binance   → {enabled,mode,testnet,api_key_masked,api_secret_masked,
//                                 has_api_key,timeout_sec,miss_heartbeat_sec,halted,
//                                 cancel_stale_sec,disclaimer_signed_at,quote_asset,
//                                 stock,spot,risk_gate,paper_separate}
//   POST /api/config/binance   → 指针字段局部合并（stock/spot/risk_gate 整档案替换）
// 在建端点（PLAN §7 表，形状先行按 §6.2 契约消费，全部 fail-soft：404/未上线由调用处兜底）：
//   GET  /api/binance/state    → {connected,halted,mode,testnet,ws_subscriptions,
//                                 rate_limit_remaining,disclaimer_signed_at,...}
//   GET  /api/binance/orders   → {orders:[{order_id,market,code,side,price,qty,amount,status,created_at}]}
//   POST /api/binance/cancel/{id} / POST /api/binance/halt / POST /api/binance/exchange_info
// English: single fetch module for all Binance endpoints; when backend paths shift, fix them here only.
import { request } from './index.js'

// —— 配置（已上线）——
export async function fetchBinanceConfig() {
  return request('/api/config/binance')
}

// 局部更新：fields 里出现什么键改什么；掩码哨兵/空串后端自动忽略不覆盖真值
export async function updateBinanceConfig(fields) {
  return request('/api/config/binance', { method: 'POST', data: fields })
}

// —— 运行时（PLAN §7 在建端点，前端先行按契约消费）——
export async function fetchBinanceState() {
  return request('/api/binance/state')
}

// market 可选 'US'|'CRYPTO'：当日新市场委托（前端撤单数据源）
export async function fetchBinanceOrders(market) {
  const q = market && market !== 'ALL' ? '?market=' + encodeURIComponent(market) : ''
  return request('/api/binance/orders' + q)
}

export async function cancelBinanceOrder(orderId) {
  return request('/api/binance/cancel/' + encodeURIComponent(orderId), { method: 'POST' })
}

// kill-switch：镜像 /api/qmt/halt 语义（halted=true 置位撤在途，false 解除）
export async function binanceHalt(halted) {
  return request('/api/binance/halt', { method: 'POST', data: { halted: !!halted } })
}

// 运维：触发 exchangeInfo 规则缓存刷新（min_notional/stepSize 等）
export async function refreshBinanceExchangeInfo() {
  return request('/api/binance/exchange_info', { method: 'POST' })
}

// —— §BINANCE-P5 新市场历史 K 线（只读端点，路由挂 authMiddleware）——
// GET /api/binance/kline?market=US|CRYPTO&code=BTCUSDT|AAPL.US&count=N（count 缺省 180、上限 500）
//   → [{date:'2026-09-23',open,high,low,close,volume}] 时间升序；无数据如实 []（前端有轴无图）
// 数据源=研究库 daily 表离线归档（scripts/download_binance_klines.py），非实时、不兜底拉交易所。
// 分轨约束：CN 的 K 线/分时仍走 api/index.js 的 /api/kline——本口传 market=CN 后端直接 400。
export async function fetchBinanceKline(market, code, count) {
  const q = new URLSearchParams({ market: String(market || ''), code: String(code || '') })
  if (count) q.set('count', String(count))
  return request('/api/binance/kline?' + q.toString())
}
