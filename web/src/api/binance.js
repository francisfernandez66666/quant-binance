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
//   GET  /api/binance/orders   → 数组 [{...}]（非 {orders:[]}；market 可选过滤，CN 行不回流）
//   POST /api/binance/cancel   → body {market, order_id}（无路径参数）
//   POST /api/binance/halt     / GET /api/binance/exchange_info（刷新规则缓存）
// §AUDITFIX925-D2（2026-09-25 审计批）：本注释旧版写的是 "POST /api/binance/cancel/{id}" 与
// "POST /api/binance/exchange_info"——两处均与后端实注册路由不符（server.go:698-699），
// 照抄者必撞 404/405。现已按后端真实契约逐一对齐，并以 vitest 契约用例（binance_contract.test.js）钉死。
// English: header contract notes realigned to server routes (cancel takes body not path id; exchange_info is GET).
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

// market 可选 'US'|'CRYPTO'：当日新市场委托（前端撤单数据源）。
// §AUDITFIX925-D2 登记：后端契约正确（server.go:700 GET），但前端当前零页面消费——
// 保留尺寸待撤单面板接线（勿删；删除需 owner 确认）。
export async function fetchBinanceOrders(market) {
  const q = market && market !== 'ALL' ? '?market=' + encodeURIComponent(market) : ''
  return request('/api/binance/orders' + q)
}

// 撤单：POST /api/binance/cancel，body {market, order_id}（后端按市场路由撤单、未注册市场拒撤）。
// §AUDITFIX925-D2：旧实现把 order_id 拼进路径（后端无此路由，必 404）——已对齐真实契约。
// 注意：未接实盘钥匙时后端回 503 "binance live channel not wired"（binance_api.go:239-242），
// 属"通道未接线"不是故障，接 UI 时按 503 单独文案，勿混进网络错误。
export async function cancelBinanceOrder(market, orderId) {
  return request('/api/binance/cancel', { method: 'POST', data: { market, order_id: orderId } })
}

// kill-switch：镜像 /api/qmt/halt 语义（halted=true 置位撤在途，false 解除）
export async function binanceHalt(halted) {
  return request('/api/binance/halt', { method: 'POST', data: { halted: !!halted } })
}

// 交易规则查询：GET /api/binance/exchange_info?symbol=BTCUSDT → {symbol,found,...rules}。
// §AUDITFIX925-D2：旧实现写成 "POST 触发缓存刷新"——方法错（后端 GET，必 405）且语义错
// （这是步长/最小名义额的只读查询面，规则缓存由执行器自带 10min TTL，无需前端触发）。
// 未接钥匙回 503、CRYPTO 执行器未装配回 503，均按文案透出（server 路由挂 adminMiddleware）。
export async function fetchBinanceExchangeInfo(symbol) {
  return request('/api/binance/exchange_info?symbol=' + encodeURIComponent(String(symbol || '')))
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

// —— §市场分家-1 详情抽屉头部现价的非 CN 轨（GET /api/binance/quote，authMiddleware）——
// GET /api/binance/quote?market=US|CRYPTO&code=BTCUSDT|AAPL →
//   有价 {ok:true, market, code, price, prev_close, change_pct, high, low, volume, amount,
//         source:'feed'|'rest', age_ms}
//   无价 {ok:false, market, code, reason}   ← 币安链未装配/池外且 REST 失败，前端如实显示"无现价快照"
// 分轨铁律：CN 现价唯一通道是 api/index.js 的 /api/stock/lookup（新浪→东财四级链）；
// 本口传 market=CN 后端直接 400，前端 parseCode 分支绝不把 A股代码打到这里。
// English: the non-CN drawer quote leg — feed-first, REST-fallback; CN stays on stock/lookup.
export async function fetchBinanceQuote(market, code) {
  const q = new URLSearchParams({ market: String(market || ''), code: String(code || '') })
  return request('/api/binance/quote?' + q.toString())
}
