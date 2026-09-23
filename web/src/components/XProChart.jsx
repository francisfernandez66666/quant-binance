// ── 新市场专业 K 线图 XProChart.jsx（§BINANCE-P5 试点）──
// 文件职责（PLAN_BINANCE_MULTI_ASSET §11.2 新市场页面积 / GAP §G-8 两链分轨）：
// US/CRYPTO 标的的专业蜡烛图——lightweight-charts v5（Apache-2.0）单 chart 双 pane：
//   pane0 = CandlestickSeries（OHLC）、pane1 = HistogramSeries（成交量），UTC 日时间轴。
// 交互硬要求：滚轮缩放 / 拖拽平移 / 价格对数轴开关（工具栏按钮切 Normal↔Logarithmic）。
// 数据口径：rows = [{date:'YYYY-MM-DD', open, high, low, close, volume}]，
// 来自 GET /api/binance/kline（研究库 daily 表离线归档，**非实时**）——与 CN 分时链完全分轨：
// CN 仍用自研 canvas 组件 KLineChart.jsx，本文件不参与 A 股渲染（零改动、零共用代码）。
// 受控用法：传 `rows` 即由调用方供数（已缓存行情的宿主复用）；只传 `code`+`market` 时组件
// 自拉一次（count 缺省 180），失败降级为空态而不是崩页。
//
// 可测性约定（jsdom 无 canvas 上下文）：rows→series 适配、日期→UNIX 秒、精度建议全部下沉到
// 下方**纯函数**并导出（toChartSeries / dateToUtcSeconds / priceFormatFor），vitest 只测纯函数
// + 组件挂载冒烟（`vi.mock('lightweight-charts')` 桩掉库），不追求 jsdom 真渲染。
//
// English: professional candlestick + volume chart for the NEW markets (US/CRYPTO) on
// lightweight-charts v5 (two panes, UTC daily axis, wheel-zoom / drag-pan / log-price toggle).
// All data adaptation lives in exported pure functions so it stays unit-testable without a
// canvas; the CN intraday canvas chart (KLineChart.jsx) is untouched by design.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createChart, CandlestickSeries, HistogramSeries, PriceScaleMode } from 'lightweight-charts'
import * as bapi from '../api/binance.js'
import { subscribeTheme } from '../theme.js'
import { fmtMoneyM } from '../utils.market.js'

// 日线步长常量（导出供测试与调用处换算窗口）
export const DAY_SECONDS = 86400

// 缺省涨跌色：沿用全站「红涨绿跌」语义（A 股口径延续到新市场，避免同屏两套颜色语言）。
// 运行时会从 :root 的 --app-up / --app-down 取当前主题值（图表内部也是 canvas，吃不了 var()）。
const UP_FALLBACK = '#e34d59'
const DOWN_FALLBACK = '#00a870'

/**
 * 日期 → UTC 零点 UNIX 秒（lightweight-charts 的 time 刻度，天然按 UTC 日对齐）。
 * 接受 'YYYY-MM-DD' / 'YYYYMMDD' / 'YYYY-MM-DDTHH:mm:ssZ'（截前 10 位）；其余一律 NaN。
 * 刻意手工拆位而不是 new Date(str)：后者对 'YYYY-MM-DD' 的解析受宿主时区影响会漂一天。
 * @param {string|number} date 日键
 * @returns {number} UNIX 秒；非法为 NaN
 */
export function dateToUtcSeconds(date) {
  if (typeof date === 'number' && Number.isFinite(date)) return date
  const s = String(date == null ? '' : date).trim().replace(/^(\d{4}-\d{2}-\d{2})[T ].*$/, '$1')
  const m = /^(\d{4})(?:-)?(\d{2})(?:-)?(\d{2})$/.exec(s)
  if (!m) return NaN
  const y = Number(m[1])
  const mo = Number(m[2])
  const d = Number(m[3])
  if (mo < 1 || mo > 12 || d < 1 || d > 31) return NaN
  const ts = Date.UTC(y, mo - 1, d)
  if (!Number.isFinite(ts)) return NaN
  return Math.floor(ts / 1000)
}

// 数字归一：null/undefined/''/NaN/脏字符串统一 NaN，由调用处判是否丢行
function num(v) {
  if (v === null || v === undefined || v === '') return NaN
  const n = Number(v)
  return Number.isFinite(n) ? n : NaN
}

// 十六进制色加透明度（'#e34d59'+'99'）；非 6 位 hex（rgb()/命名色）原样返回不加料
function withAlpha(hex, alpha) {
  return typeof hex === 'string' && /^#[0-9a-fA-F]{6}$/.test(hex) ? hex + alpha : hex
}

/**
 * rows → 蜡烛序列 + 量柱序列（纯函数：组件与单测共用同一份适配逻辑）。
 * 兜住三件会让 lightweight-charts 抛错或画歪的事：
 *   1) 时间严格升序（库对乱序数据直接 throw），同日去重保留最后一条（补档取新）；
 *   2) 字段缺失/非数字（OHLC 任一非有限值或日期非法）整行跳过并计入 skipped，供空态提示；
 *   3) 成交量缺失记 0——量柱可空，不能因此丢整根蜡烛。
 * @param {Array<{date:string,open:number,high:number,low:number,close:number,volume:number}>} rows
 * @param {{up?:string,down?:string}} [palette] 量柱涨跌色（主题注入；缺省走红涨绿跌基线）
 * @returns {{candles: Array, volumes: Array, skipped: number}}
 */
export function toChartSeries(rows, palette) {
  const up = withAlpha((palette && palette.up) || UP_FALLBACK, '99')
  const down = withAlpha((palette && palette.down) || DOWN_FALLBACK, '99')
  const list = Array.isArray(rows) ? rows : []
  const byTime = new Map() // 时间戳 → 行（同日后来覆盖）
  let skipped = 0
  for (const r of list) {
    const time = dateToUtcSeconds(r && r.date)
    const o = num(r && r.open)
    const h = num(r && r.high)
    const l = num(r && r.low)
    const c = num(r && r.close)
    if (!Number.isFinite(time) || !Number.isFinite(o) || !Number.isFinite(h) || !Number.isFinite(l) || !Number.isFinite(c)) {
      skipped += 1
      continue
    }
    const v = num(r && r.volume)
    byTime.set(time, { time, open: o, high: h, low: l, close: c, volume: v > 0 ? v : 0, isUp: c >= o })
  }
  const merged = [...byTime.values()].sort((a, b) => a.time - b.time)
  return {
    candles: merged.map(({ time, open, high, low, close }) => ({ time, open, high, low, close })),
    volumes: merged.map(({ time, volume, isUp }) => ({ time, value: volume, color: isUp ? up : down })),
    skipped,
  }
}

/**
 * 价格刻度精度建议（纯函数）：整段最大绝对价 < 1 的低价币用 8 位小数，否则两位。
 * 精度给不够时低位币蜡烛会被压成 0.00 平线——图就没得读了。
 * @param {Array} rows 后端 rows
 * @param {number} [tail] 参与判断的尾部根数（默认 400，够覆盖可见区）
 * @returns {{type:'price', precision:number, minMove:number}}
 */
export function priceFormatFor(rows, tail = 400) {
  const list = (Array.isArray(rows) ? rows : []).slice(-tail)
  let max = 0
  for (const r of list) {
    for (const k of ['high', 'low', 'open', 'close']) {
      const v = num(r && r[k])
      if (Number.isFinite(v) && Math.abs(v) > max) max = Math.abs(v)
    }
  }
  if (max > 0 && max < 1) return { type: 'price', precision: 8, minMove: 0.00000001 }
  return { type: 'price', precision: 2, minMove: 0.01 }
}

// 读当前主题涨跌色（缺失回落基线）；document 缺席（SSR/纯测）也走基线不抛。
// English: resolve live theme up/down colors, falling back to the light baseline.
function readPalette() {
  if (typeof document === 'undefined' || typeof getComputedStyle !== 'function') {
    return { up: UP_FALLBACK, down: DOWN_FALLBACK }
  }
  try {
    const s = getComputedStyle(document.documentElement)
    const g = (n, fb) => ((s.getPropertyValue(n) || '').trim() || fb)
    return { up: g('--app-up', g('--app-chart-vol-up', UP_FALLBACK)), down: g('--app-down', g('--app-chart-vol-down', DOWN_FALLBACK)) }
  } catch (_) {
    return { up: UP_FALLBACK, down: DOWN_FALLBACK }
  }
}

/**
 * 新市场专业 K 线图容器。
 * @param {object} props
 * @param {string} [props.code] 标的键（BTCUSDT / AAPL.US）；未传 rows 时据此自拉数据
 * @param {'US'|'CRYPTO'} [props.market] 市场（决定价格展示口径，见 utils.market.fmtMoneyM）
 * @param {string} [props.name] 标题展示名
 * @param {Array} [props.rows] 受控数据（宿主已持有则直传，组件不再请求）
 * @param {number} [props.count] 自拉根数（缺省 180，后端硬上限 500）
 * @param {number} [props.height] 图体高度（px）
 * @returns {JSX.Element}
 */
export default function XProChart({ code = '', market = 'US', name = '', rows = null, count = 180, height = 320 }) {
  const wrapRef = useRef(null)   // 图表挂载容器
  const chartRef = useRef(null)  // IChartApi 实例
  const candleRef = useRef(null) // 主 pane 蜡烛序列
  const volRef = useRef(null)    // 副 pane 量柱序列
  const [selfRows, setSelfRows] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [logPrice, setLogPrice] = useState(false) // 对数轴开关（长周期涨幅/低价币看形态用）
  const [themeTick, setThemeTick] = useState(0)
  useEffect(() => subscribeTheme(() => setThemeTick((n) => n + 1)), [])

  // 受控 rows 优先；未受控用自拉缓存
  const controlled = Array.isArray(rows)
  const data = controlled ? rows : selfRows

  // ── 自拉（受控模式不发请求；换标的先清旧数据，避免上一根的曲线被误读成当前标的）──
  useEffect(() => {
    if (controlled || !code) return undefined
    let dead = false
    setLoading(true)
    setError('')
    setSelfRows([])
    bapi.fetchBinanceKline(market, code, count)
      .then((r) => { if (!dead) setSelfRows(Array.isArray(r) ? r : []) })
      .catch(() => { if (!dead) { setSelfRows([]); setError('历史 K 线读取失败（/api/binance/kline 不可达）') } })
      .finally(() => { if (!dead) setLoading(false) })
    return () => { dead = true }
  }, [controlled, code, market, count])

  // ── 建图（挂载一次）：双 pane + 缩放/平移；卸载 chart.remove() 防实例泄漏 ──
  useEffect(() => {
    const el = wrapRef.current
    if (!el) return undefined
    const palette = readPalette()
    const chart = createChart(el, {
      autoSize: true, // 容器宽高超界时自动跟随（ResizeObserver 不可用时回落 height）
      height,
      layout: {
        background: { color: 'transparent' },
        textColor: '#909399',
        panes: { paneSpacing: 2 },
      },
      grid: {
        vertLines: { color: 'rgba(128,128,128,0.12)' },
        horzLines: { color: 'rgba(128,128,128,0.12)' },
      },
      rightPriceScale: { borderColor: 'rgba(128,128,128,0.25)' },
      // 交互硬需求：滚轮缩放 + 拖拽平移（触屏拖动/双指缩放一并放开）
      handleScroll: { mouseWheel: true, pressedMouseMove: true, horzTouchDrag: true, vertTouchDrag: false },
      handleScale: { axisPressedMouseMove: true, mouseWheel: true, pinch: true },
      timeScale: {
        timeVisible: false, // 日线只看日期刻度
        secondsVisible: false,
        borderColor: 'rgba(128,128,128,0.25)',
        rightOffset: 4,
      },
      crosshair: { mode: 1 /* CrosshairMode.Normal */ },
    })
    const candles = chart.addSeries(CandlestickSeries, {
      upColor: palette.up,
      downColor: palette.down,
      borderUpColor: palette.up,
      borderDownColor: palette.down,
      wickUpColor: palette.up,
      wickDownColor: palette.down,
      priceFormat: priceFormatFor(data),
    }, 0)
    const volumes = chart.addSeries(HistogramSeries, {
      priceFormat: { type: 'volume' },
      priceLineVisible: false,
      lastValueVisible: false,
      scaleMargins: { top: 0.15, bottom: 0 },
    }, 1)
    // 量副图压到 1/4 高：pane API 不可用（旧版/桩环境）时静默跳过，主图不受影响
    try {
      const panes = typeof chart.panes === 'function' ? chart.panes() : null
      if (panes && panes[1] && typeof panes[1].setHeight === 'function') {
        panes[1].setHeight(Math.max(60, Math.round(height * 0.25)))
      }
    } catch (_) { /* 忽略：高度分配属锦上添花 */ }

    chartRef.current = chart
    candleRef.current = candles
    volRef.current = volumes
    return () => {
      chart.remove()
      chartRef.current = null
      candleRef.current = null
      volRef.current = null
    }
    // height 只决定初建尺寸（autoSize 后续跟随容器），不入依赖以免改宽重建丢缩放状态
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ── 灌数据（rows/主题变化）：走纯函数适配后 setData；空数组=保留坐标系（有轴无图）──
  useEffect(() => {
    const candles = candleRef.current
    const volumes = volRef.current
    if (!candles || !volumes) return
    const series = toChartSeries(data, readPalette())
    candles.applyOptions({ priceFormat: priceFormatFor(data) })
    candles.setData(series.candles)
    volumes.setData(series.volumes)
    const chart = chartRef.current
    if (chart && series.candles.length) chart.timeScale().fitContent()
  }, [data, themeTick])

  // ── 对数轴开关：作用在蜡烛所在的价格轴 ──
  useEffect(() => {
    const chart = chartRef.current
    const candles = candleRef.current
    if (!chart || !candles) return
    const scale = typeof candles.priceScale === 'function' ? candles.priceScale() : chart.priceScale('right')
    if (scale && typeof scale.applyOptions === 'function') {
      scale.applyOptions({ mode: logPrice ? PriceScaleMode.Logarithmic : PriceScaleMode.Normal })
    }
  }, [logPrice, data])

  // 概要读数：末根收盘 + 整段区间涨跌（与 KLineChart 工具栏同语义，换库不丢既有信息）
  const summary = useMemo(() => {
    const list = Array.isArray(data) ? data : []
    if (!list.length) return null
    const close = num(list[list.length - 1] && list[list.length - 1].close)
    const base = num(list[0] && list[0].close)
    const pct = Number.isFinite(close) && Number.isFinite(base) && base > 0 ? ((close - base) / base) * 100 : NaN
    return { close, pct }
  }, [data])

  const pctUp = summary && Number.isFinite(summary.pct) ? summary.pct >= 0 : true

  return (
    <div style={{ width: '100%' }}>
      {/* 工具栏：标题 + 末价/区间涨跌 + 对数轴开关 */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', marginBottom: 6, fontSize: 12 }}>
        <span style={{ fontWeight: 700 }}>{name || code}</span>
        {summary && Number.isFinite(summary.close) && (
          <span style={{ fontWeight: 700, color: pctUp ? 'var(--app-up)' : 'var(--app-down)' }}>
            {fmtMoneyM(summary.close, market)}
          </span>
        )}
        {summary && Number.isFinite(summary.pct) && (
          <span style={{ color: 'var(--app-text-2)' }}>{(summary.pct >= 0 ? '+' : '') + summary.pct.toFixed(2)}%</span>
        )}
        <button
          type="button"
          aria-pressed={logPrice}
          data-testid="xpro-log-toggle"
          onClick={() => setLogPrice((v) => !v)}
          style={{
            marginLeft: 'auto', cursor: 'pointer', fontSize: 12, lineHeight: '20px', padding: '0 8px', borderRadius: 4,
            border: '1px solid var(--app-border)', background: logPrice ? 'var(--app-accent, #0052d9)' : 'transparent',
            color: logPrice ? '#fff' : 'var(--app-text-2, inherit)',
          }}>
          对数轴
        </button>
      </div>
      {/* 图体：lightweight-charts 挂载位（空数据=有轴无图，不出错误页） */}
      <div ref={(node) => { wrapRef.current = node }} data-testid="xpro-chart" style={{ width: '100%', height }} />
      {/* 状态注记：加载中 / 读取失败 / 空态（都只占一行文字，绝不影响坐标系渲染） */}
      <div style={{ fontSize: 11, color: 'var(--app-muted-2, #909399)', marginTop: 4 }}>
        {loading && '读取历史 K 线…'}
        {!loading && error && <span style={{ color: 'var(--app-down)' }}>{error}</span>}
        {!loading && !error && !data.length && (code || controlled) && '暂无历史日线（研究库未落该标的档，可跑 scripts/download_binance_klines.py 补档）'}
        {!loading && !error && !!data.length && '日 K·UTC 轴｜滚轮缩放 / 拖拽平移｜数据源：研究库 daily（离线归档，非实时）'}
      </div>
    </div>
  )
}
