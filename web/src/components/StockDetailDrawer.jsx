// ── 全局个股详情抽屉 StockDetailDrawer.jsx ──
// §F3 结构性增强：一个组件收敛 Signals/Positions/Paper/MsgCenter/Watchlist 五处"点开个股"的需求，
// 替代此前各页各做各的移动端面板。内容 = 实时价（fetchStockLookup，5s 轮询）+ 分时/盘口（复用 MinuteView）
// + 所属信号 / 持仓 / 相关消息三段关联信息（由调用方传入原始列表，本组件按代码过滤，缺失段自动隐藏）。
// 桌面为右侧滑入浮层，窄屏（≤640px）转为底部抽屉；Esc / 遮罩 / 关闭按钮均可收起。
//
// English: the §F3 global stock-detail drawer — one component shared by Signals/Positions/Paper/MsgCenter/
// Watchlist to replace each page's ad-hoc mobile panel. Body = live price (fetchStockLookup, 5s poll) +
// intraday/depth (reuses MinuteView) + associated signals / positions / related messages (raw lists passed
// in by the host, filtered by code here; empty sections hide themselves). Right slide-over on desktop,
// bottom sheet on narrow screens (≤640px); closes on Esc / overlay click / close button.
import React, { useState, useEffect } from 'react'
import * as api from '../api/index.js'
import MinuteView from './MinuteView.jsx'
// §BINANCE-P5（PLAN §11.2 新市场页面积试点）：US/CRYPTO 详情改挂 lightweight-charts 专业蜡烛图
// English: §BINANCE-P5 — new-market (US/CRYPTO) detail body uses the professional candle chart.
import XProChart from './XProChart.jsx'
// §市场分家-1：头部现价的非 CN 轨（/api/binance/quote，feed 优先+REST 回落）
import { fetchBinanceQuote } from '../api/binance.js'
// §BINANCE-P4：代码→市场解析单一来源（纯函数模块，无环依赖）
import { parseCode } from '../utils.market.js'

// codeEq 归一化比对：兼容 "600000" 与 "600000.SH/.SZ" 两种写法，任一前缀（6 位数字）相同即视为同一标的。
// §BINANCE-P4（PLAN §11.2）：6 位前缀假设改调 parseCode（与 Go InferMarketOf 同规则）——
// 同市场且裸码相等才算同一标的，BTCUSDT/AAPL 等新市场代码不再被 6 位截断误配；CN 行为等价。
// English: §BINANCE-P4 — parseCode-based match (same market + same bare symbol), replacing the
// 6-digit prefix heuristic so new-market tickers no longer truncate-match.
function codeEq(a, b) {
  if (!a || !b) return false
  if (String(a).toUpperCase() === String(b).toUpperCase()) return true
  const pa = parseCode(a)
  const pb = parseCode(b)
  return pa.market === pb.market && pa.symbol === pb.symbol
}

// fmtPct 涨跌幅带符号（红涨绿跌色由调用处决定）。English: signed change% string.
function fmtPct(v) {
  const n = Number(v)
  if (!Number.isFinite(n)) return ''
  return (n >= 0 ? '+' : '') + n.toFixed(2) + '%'
}

// RelList 抽屉内关联列表（板块/概念/同行业个股）通用渲染子件。
function RelList({ items, render }) {
  return (
    <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
      {items.map((it, i) => (
        <li key={i} style={{ fontSize: 12, lineHeight: 1.5, padding: '6px 8px', background: 'var(--app-surface-2)', borderRadius: 6 }}>
          {render(it)}
        </li>
      ))}
    </ul>
  )
}

/**
 * 全局个股详情抽屉。
 * @param {object} props
 * @param {boolean} props.open 是否展开
 * @param {string} props.code 标的代码
 * @param {string} [props.name] 标的名称（未传则用 lookup 返回名兜底）
 * @param {number} [props.price] 初始现价（行数据已有则先显示，避免打开瞬间空白）
 * @param {number} [props.changePct] 初始涨跌幅%
 * @param {{signals?:Array,positions?:Array,messages?:Array}} [props.related] 关联原始列表（内部按 code 过滤）
 * @param {() => void} props.onClose 关闭回调
 */
export default function StockDetailDrawer({ open, code, name, price, changePct, related, onClose }) {
  const [quote, setQuote] = useState(null)

  useEffect(() => {
    if (!open || !code) { setQuote(null); return }
    let alive = true
    // §市场分家-1 头部现价按市场分轨（与下方 K 线体分轨同姿势）：
    // CN 走 /api/stock/lookup 四级链（一字不动）；US/CRYPTO 走 /api/binance/quote——
    // 此前非 CN 标的也打 lookup 链，新浪/腾讯对 BTCUSDT/AAPL 必然打空且静默回 {price:0}，
    // 抽屉头部靠行数据兜底、形同没接。ok=false 时保留入参兜底价，绝不显示 0。
    const mkt = parseCode(code).market
    const load = () => {
      if (mkt === 'CN') {
        api.fetchStockLookup(code)
          .then((r) => { if (alive && r) setQuote(r) })
          .catch(() => {})
        return
      }
      fetchBinanceQuote(mkt, code)
        .then((r) => { if (alive && r && r.ok) setQuote({ price: r.price, changePct: r.change_pct }) })
        .catch(() => {})
    }
    load()
    const t = setInterval(load, 5000)
    return () => { alive = false; clearInterval(t) }
  }, [open, code])

  useEffect(() => {
    if (!open) return
    // Esc 关闭抽屉
    const h = (e) => { if (e.key === 'Escape' && onClose) onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [open, onClose])

  if (!open || !code) return null

  // 展示名：行情返回优先，回落入参名称/代码
  const showName = (quote && quote.name) || name || code
  const showPrice = quote && Number.isFinite(Number(quote.price)) && Number(quote.price) > 0 ? Number(quote.price) : price
  // §市场分家-1 涨跌幅同价源：轮询回来的 changePct 优先，行数据入参兜底（CN lookup 不带则维持旧行为）
  const rawChg = (quote && (quote.changePct ?? quote.change_pct)) ?? changePct
  const chg = Number.isFinite(Number(rawChg)) ? Number(rawChg) : null
  const chgUp = chg != null && chg >= 0
  // §BINANCE-P5：本标的所属市场（决定详情体走哪条 K 线链）——CN=自研分时/盘口，US/CRYPTO=专业蜡烛图
  const codeMarket = parseCode(code).market

  const rel = related || {}
  // 关联数据按本股代码过滤（信号/持仓/消息）
  const mySignals = (rel.signals || []).filter((s) => codeEq(s.code, code))
  const myPositions = (rel.positions || []).filter((p) => codeEq(p.code, code))
  const myMessages = (rel.messages || []).filter((m) => codeEq(m.code, code))

  // 抽屉骨架：全屏遮罩（点击关闭）+ 右侧滑入面板，面板内依次为头部行情、明细与关联信息各段（见下方 JSX）
  return (
    <div
      className="sdd-mask"
      data-testid="stock-detail-overlay"
      onClick={onClose}
      style={{
        position: 'fixed', inset: 0, zIndex: 1000, background: 'rgba(0,0,0,0.45)',
        display: 'flex', justifyContent: 'flex-end',
      }}
    >
      <div
        className="sdd-panel"
        data-testid="stock-detail-panel"
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(520px, 100vw)', height: '100%', background: 'var(--app-surface)', display: 'flex', flexDirection: 'column',
          boxShadow: '-2px 0 12px rgba(0,0,0,0.18)', animation: 'sdd-slide-in .18s ease-out',
        }}
      >
        <style>{'@keyframes sdd-slide-in{from{transform:translateX(24px);opacity:.4}to{transform:none;opacity:1}}'
          + '@media(max-width:640px){.sdd-panel{width:100vw!important;height:auto!important;max-height:88vh;border-radius:14px 14px 0 0;margin-top:auto}'
          + '.sdd-mask{align-items:flex-end!important}}'}</style>
        {/* 头部：代码/名称/现价/涨跌幅 + 关闭 */}
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, padding: '14px 16px', borderBottom: '1px solid #eef0f3' }}>
          <span style={{ fontSize: 18, fontWeight: 700 }}>{showName}</span>
          <span style={{ fontSize: 12, color: 'var(--app-muted-2)' }}>{code}</span>
          {Number.isFinite(Number(showPrice)) && showPrice > 0 && (
            <span style={{ fontSize: 18, fontWeight: 700, color: chgUp ? 'var(--app-up)' : 'var(--app-down)' }}>
              {Number(showPrice).toFixed(2)}
            </span>
          )}
          {chg != null && (
            <span style={{ fontSize: 13, fontWeight: 600, color: chgUp ? 'var(--app-up)' : 'var(--app-down)' }}>{fmtPct(chg)}</span>
          )}
          <button aria-label="关闭" onClick={onClose}
            style={{ marginLeft: 'auto', border: 'none', background: 'transparent', fontSize: 20, lineHeight: 1, cursor: 'pointer', color: 'var(--app-text-2)' }}>×</button>
        </div>
        {/* 主体：分时/盘口 + 关联信息（可滚动） */}
        <div style={{ flex: 1, overflowY: 'auto', padding: 14 }}>
          {/* §BINANCE-P5 两链分轨（GAP §G-8）：US/CRYPTO 走专业 K 线（lightweight-charts +
              /api/binance/kline 研究库日 K），CN 仍走 MinuteView→KLineChart 自研 canvas 链，
              本行只加市场分支、不改 CN 分支一行代码。
              English: new markets render the pro candle chart; the CN intraday path is untouched. */}
          {codeMarket === 'CN'
            ? <MinuteView code={code} name={showName} />
            : <XProChart code={code} market={codeMarket} name={showName} />}

          {mySignals.length > 0 && (
            <section style={{ marginTop: 16 }}>
              <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 6 }}>相关信号 <span style={{ color: 'var(--app-muted-2)', fontWeight: 400 }}>({mySignals.length})</span></div>
              <RelList items={mySignals} render={(s) => (
                <span>
                  <b>{s.action || (s.direction === '做空' ? '卖出' : '买入')}</b> · {s.strategy}
                  {s.confidence != null && <span style={{ color: 'var(--app-text-2)' }}> 置信度 {(Number(s.confidence) <= 1 ? Number(s.confidence) * 100 : Number(s.confidence)).toFixed(0)}%</span>}
                  {s.reason && <span style={{ color: 'var(--app-muted-2)' }}> {s.reason}</span>}
                </span>
              )} />
            </section>
          )}

          {myPositions.length > 0 && (
            <section style={{ marginTop: 16 }}>
              <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 6 }}>我的持仓 <span style={{ color: 'var(--app-muted-2)', fontWeight: 400 }}>({myPositions.length})</span></div>
              <RelList items={myPositions} render={(p) => (
                <span>
                  {p.direction === '做空' ? '空' : '多'} {p.qty ?? p.quantity ?? ''} · 成本 {Number(p.cost ?? p.cost_price ?? p.entry_price ?? 0).toFixed(2)}
                  {(p.pnl != null || p.pnl_pct != null) && (
                    <span style={{ color: (p.pnl ?? p.pnl_pct) >= 0 ? 'var(--app-up)' : 'var(--app-down)' }}>
                      {p.pnl != null ? ` 浮盈 ${Number(p.pnl).toFixed(2)}` : ''}
                      {p.pnl_pct != null ? ` ${fmtPct(Number(p.pnl_pct))}` : ''}
                    </span>
                  )}
                </span>
              )} />
            </section>
          )}

          {myMessages.length > 0 && (
            <section style={{ marginTop: 16, marginBottom: 8 }}>
              <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 6 }}>相关消息 <span style={{ color: 'var(--app-muted-2)', fontWeight: 400 }}>({myMessages.length})</span></div>
              <RelList items={myMessages.slice(0, 20)} render={(m) => (
                <span>
                  <b>{m.level || '消息'}</b> {m.time ? <span style={{ color: 'var(--app-muted-2)' }}>{m.time}</span> : null} {m.body || m.title || ''}
                </span>
              )} />
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
