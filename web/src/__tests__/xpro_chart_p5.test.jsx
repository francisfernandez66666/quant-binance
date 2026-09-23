// ── §BINANCE-P5 新市场专业 K 线图单测 xpro_chart_p5.test.jsx ──
// 文件职责（PLAN §12.2 测试金字塔·前端）：lightweight-charts 试点的可测面——
//   ① 纯函数：dateToUtcSeconds（日期→UTC 秒，禁宿主时区漂移）、toChartSeries
//      （rows→双序列：升序保障 / 缺字段行跳过计数 / 同日去重 / 空量不丢蜡烛）、
//      priceFormatFor（低价币 8 位精度、常规两位）；
//   ② 组件挂载冒烟 + 卸载清理：jsdom 无真 canvas 上下文，故整库 `vi.mock('lightweight-charts')`
//      桩掉，只验「建图→灌数→对数轴开关→remove」这条调用序列与空态不抛；
//   ③ 接页分轨锁：StockDetailDrawer 里 CN 仍走 MinuteView、US/CRYPTO 才挂 XProChart
//      （两链分轨 = GAP §G-8 的硬约束，谁把 CN 也切过去谁先撞红）。
// English: vitest for the new-market pro-chart pilot — pure adapters (date/series/precision),
// a mocked-library mount smoke (jsdom has no canvas), and the CN-vs-new-market routing lock.
// 不测真渲染（lightweight-charts 需 canvas 2D 上下文），渲染正确性归 Playwright E2E。
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react'

// ── 桩 lightweight-charts：只记录调用，不做任何绘制 ──
const calls = { createChart: [], addSeries: [], setData: [], applyOptions: [], remove: [], priceScale: [], fitContent: 0, setHeight: [] }

function makeSeries() {
  return {
    setData: vi.fn((d) => { calls.setData.push(d) }),
    applyOptions: vi.fn((o) => { calls.applyOptions.push(o) }),
    priceScale: () => ({ applyOptions: vi.fn((o) => { calls.priceScale.push(o) }) }),
  }
}

// lightweight-charts 全假实现：createChart/addSeries/panes 等调用逐参数记录进 calls，
// 断言只看"组件让库做了什么"，不依赖真渲染（jsdom 无 canvas）。
vi.mock('lightweight-charts', () => {
  const createChart = vi.fn((el, opts) => {
    calls.createChart.push({ el, opts })
    return {
      addSeries: vi.fn((def, seriesOpts, paneIndex) => {
        calls.addSeries.push({ def, seriesOpts, paneIndex })
        return makeSeries()
      }),
      panes: vi.fn(() => [{ setHeight: null }, { setHeight: (h) => calls.setHeight.push(h) }]),
      timeScale: () => ({ fitContent: () => { calls.fitContent += 1 } }),
      priceScale: () => ({ applyOptions: vi.fn((o) => { calls.priceScale.push(o) }) }),
      applyOptions: vi.fn(),
      remove: vi.fn(() => { calls.remove.push(1) }),
    }
  })
  return {
    createChart,
    CandlestickSeries: { name: 'Candlestick' },
    HistogramSeries: { name: 'Histogram' },
    PriceScaleMode: { Normal: 0, Logarithmic: 1, Percentage: 2 },
  }
})

// ── 桩 fetch 层：受控/自拉两条路都走它，URL 参数即端点契约 ──
const fetchBinanceKline = vi.fn()
vi.mock('../api/binance.js', () => ({ fetchBinanceKline: (...a) => fetchBinanceKline(...a) }))
// 抽屉的实时价轮询与本批无关，桩掉避免 jsdom 里真发请求
vi.mock('../api/index.js', () => ({ fetchStockLookup: () => Promise.resolve(null) }))
vi.mock('../components/MinuteView.jsx', () => ({ default: ({ code }) => <div data-testid="minuteview-stub">{code}</div> }))

import XProChart, { toChartSeries, dateToUtcSeconds, priceFormatFor, DAY_SECONDS } from '../components/XProChart.jsx'
import StockDetailDrawer from '../components/StockDetailDrawer.jsx'

// 每个用例前清空调用捕获，断言只看当次挂载/更新的真实调用序列
beforeEach(() => {
  calls.createChart.length = 0
  calls.addSeries.length = 0
  calls.setData.length = 0
  calls.applyOptions.length = 0
  calls.remove.length = 0
  calls.priceScale.length = 0
  calls.setHeight.length = 0
  calls.fitContent = 0
  fetchBinanceKline.mockReset()
})
afterEach(() => { cleanup() })

const day = (n) => new Date(Date.UTC(2026, 8, 1 + n)).toISOString().slice(0, 10)
const row = (n, base = 100) => ({
  date: day(n), open: base + n, high: base + n + 1, low: base + n - 1, close: base + n + 0.5, volume: 1000 + n,
})

describe('dateToUtcSeconds（日期→UTC 秒，禁时区漂移）', () => {
  it('YYYY-MM-DD 与 YYYYMMDD 同解，且恒为 UTC 零点', () => {
    expect(dateToUtcSeconds('2026-09-23')).toBe(Math.floor(Date.UTC(2026, 8, 23) / 1000))
    expect(dateToUtcSeconds('20260923')).toBe(dateToUtcSeconds('2026-09-23'))
    expect(dateToUtcSeconds(' 2026-09-23 ')).toBe(dateToUtcSeconds('2026-09-23'))
  })
  it('带时刻的 ISO 串截到日（同一天的所有时刻同刻度）', () => {
    expect(dateToUtcSeconds('2026-09-23T16:00:00Z')).toBe(dateToUtcSeconds('2026-09-23'))
    expect(dateToUtcSeconds('2026-09-23 09:30:00')).toBe(dateToUtcSeconds('2026-09-23'))
  })
  it('相邻两天差一个 DAY_SECONDS（UTC 轴步长锁）', () => {
    expect(dateToUtcSeconds('2026-09-24') - dateToUtcSeconds('2026-09-23')).toBe(DAY_SECONDS)
  })
  it('非法/缺失一律 NaN（交给 toChartSeries 丢行）', () => {
    for (const bad of ['', null, undefined, '2026-13-01', '2026-02-31x', 'not-a-date', '2026-9-3', {}]) {
      expect(Number.isNaN(dateToUtcSeconds(bad))).toBe(true)
    }
  })
})

describe('toChartSeries（rows→蜡烛+量柱，纯函数）', () => {
  it('正常序列：两根数组等长、字段齐、time 升序', () => {
    const { candles, volumes, skipped } = toChartSeries([row(0), row(1), row(2)])
    expect(skipped).toBe(0)
    expect(candles).toHaveLength(3)
    expect(volumes).toHaveLength(3)
    expect(candles[0]).toEqual({ time: dateToUtcSeconds(day(0)), open: 100, high: 101, low: 99, close: 100.5 })
    expect(volumes[0].value).toBe(1000)
    expect(candles[1].time).toBeGreaterThan(candles[0].time)
  })
  it('输入乱序也保证输出升序（库对乱序数据会直接 throw）', () => {
    const { candles } = toChartSeries([row(5), row(1), row(9), row(0)])
    const t = candles.map((c) => c.time)
    expect([...t].sort((a, b) => a - b)).toEqual(t)
  })
  it('字段缺失/非数字的行整行跳过并计数', () => {
    const bad = [
      row(0),
      { date: day(1), open: 1, high: 2, low: 0.5 },           // 缺 close
      { date: '', open: 1, high: 2, low: 0.5, close: 1.5 },   // 缺日期
      { date: day(3), open: null, high: 2, low: 0.5, close: 1.5 },
      { date: day(4), open: 'abc', high: 2, low: 0.5, close: 1.5 },
      null,
    ]
    const { candles, volumes, skipped } = toChartSeries(bad)
    expect(skipped).toBe(5)
    expect(candles).toHaveLength(1)
    expect(volumes).toHaveLength(1)
  })
  it('成交量缺失/为负记 0，但不丢整根蜡烛', () => {
    const { candles, volumes } = toChartSeries([
      { date: day(0), open: 10, high: 11, low: 9, close: 10.5 },
      { date: day(1), open: 10, high: 11, low: 9, close: 10.5, volume: -5 },
    ])
    expect(candles).toHaveLength(2)
    expect(volumes.map((v) => v.value)).toEqual([0, 0])
  })
  it('同日重复只留最后一条（补档取新，重复 time 会让库抛错）', () => {
    const { candles, volumes, skipped } = toChartSeries([
      row(0, 100), { ...row(0), close: 999 }, row(1),
    ])
    expect(candles).toHaveLength(2)
    expect(volumes).toHaveLength(2)
    expect(candles[0].close).toBe(999)
    expect(skipped).toBe(0)
  })
  it('涨跌方向决定量柱色（收≥开=涨色，否则跌色），主题色可注入', () => {
    const up = { date: day(0), open: 10, high: 11, low: 9, close: 12, volume: 1 }
    const down = { date: day(1), open: 10, high: 11, low: 5, close: 6, volume: 1 }
    const a = toChartSeries([up, down])
    expect(a.volumes[0].color).not.toBe(a.volumes[1].color)
    const b = toChartSeries([up, down], { up: '#111111', down: '#222222' })
    expect(b.volumes[0].color.startsWith('#111111')).toBe(true)
    expect(b.volumes[1].color.startsWith('#222222')).toBe(true)
  })
  it('空/非数组入参=空序列（空态：有轴无图）', () => {
    for (const empty of [[], null, undefined, {}]) {
      const r = toChartSeries(empty)
      expect(r.candles).toEqual([])
      expect(r.volumes).toEqual([])
    }
  })
})

describe('priceFormatFor（精度建议：低价币不被压成 0.00 平线）', () => {
  it('常规价两位（BTC/AAPL 量级）', () => {
    expect(priceFormatFor([row(0, 60000)])).toEqual({ type: 'price', precision: 2, minMove: 0.01 })
    expect(priceFormatFor([])).toEqual({ type: 'price', precision: 2, minMove: 0.01 })
  })
  it('全序列 <1 → 8 位小数', () => {
    const micro = [{ date: day(0), open: 0.0000123, high: 0.0000131, low: 0.0000119, close: 0.0000128, volume: 1 }]
    expect(priceFormatFor(micro)).toEqual({ type: 'price', precision: 8, minMove: 0.00000001 })
  })
  it('混入一根大正价则回到两位（精度按可见尾部段判）', () => {
    const mixed = [{ date: day(0), open: 0.5, high: 1.2, low: 0.4, close: 1, volume: 1 }]
    expect(priceFormatFor(mixed).precision).toBe(2)
  })
})

describe('XProChart 挂载冒烟（lightweight-charts 已桩掉，不验真渲染）', () => {
  it('受控 rows：建一次图、双 pane 各一条序列、灌两次数据、卸载 remove', () => {
    const rows = [row(0), row(1), row(2)]
    const { unmount } = render(<XProChart rows={rows} market="CRYPTO" name="BTCUSDT" />)
    expect(calls.createChart).toHaveLength(1)
    expect(calls.addSeries).toHaveLength(2)
    expect(calls.addSeries[0].paneIndex).toBe(0)
    expect(calls.addSeries[1].paneIndex).toBe(1)
    // 交互硬要求：滚轮缩放 + 拖拽平移在创建项里显式打开
    const opts = calls.createChart[0].opts
    expect(opts.handleScroll.mouseWheel).toBe(true)
    expect(opts.handleScroll.pressedMouseMove).toBe(true)
    expect(opts.handleScale.mouseWheel).toBe(true)
    // 两次 setData = 蜡烛 + 量柱，各 3 根
    expect(calls.setData).toHaveLength(2)
    expect(calls.setData[0]).toHaveLength(3)
    expect(screen.getByTestId('xpro-chart')).toBeInTheDocument()
    unmount()
    expect(calls.remove).toHaveLength(1) // 泄漏锁：卸载必 remove
  })

  it('滚轮/平移/对数轴开关：点按钮后价格轴 mode 从 Normal 切 Logarithmic', () => {
    render(<XProChart rows={[row(0), row(1)]} market="US" name="AAPL.US" />)
    const btn = screen.getByTestId('xpro-log-toggle')
    expect(btn.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(btn)
    expect(btn.getAttribute('aria-pressed')).toBe('true')
    expect(calls.priceScale.some((o) => o.mode === 1)).toBe(true) // PriceScaleMode.Logarithmic=1
    fireEvent.click(btn)
    expect(calls.priceScale.filter((o) => o.mode === 0).length).toBeGreaterThan(0)
  })

  it('空态：rows=[] 只画坐标系，不抛错也不 fitContent', () => {
    expect(() => render(<XProChart rows={[]} market="CRYPTO" />)).not.toThrow()
    expect(calls.createChart).toHaveLength(1)
    expect(calls.setData.map((d) => d.length)).toEqual([0, 0])
    expect(calls.fitContent).toBe(0)
  })

  it('脏数据：全行缺字段时 skipped>0 仍不抛，序列为空', () => {
    expect(() => render(<XProChart rows={[{ date: 'x' }, { open: 1 }]} market="US" />)).not.toThrow()
    expect(calls.setData.map((d) => d.length)).toEqual([0, 0])
  })

  it('非受控：按 market+code 自拉 /api/binance/kline，回包灌图', async () => {
    fetchBinanceKline.mockResolvedValue([row(0), row(1)])
    render(<XProChart code="BTCUSDT" market="CRYPTO" count={180} />)
    await waitFor(() => expect(fetchBinanceKline).toHaveBeenCalledWith('CRYPTO', 'BTCUSDT', 180))
    await waitFor(() => expect(calls.setData.some((d) => d.length === 2)).toBe(true))
  })

  it('自拉失败降级空态（不崩页、不画假数据），并提示不可达', async () => {
    fetchBinanceKline.mockRejectedValue(new Error('boom'))
    render(<XProChart code="AAPL.US" market="US" />)
    await waitFor(() => expect(screen.getByTestId('xpro-chart')).toBeInTheDocument())
    expect(calls.createChart).toHaveLength(1)
    expect(calls.setData.every((d) => d.length === 0)).toBe(true)
    expect(document.body.textContent).toContain('/api/binance/kline')
  })

  it('量副图高度压到主图的 1/4（pane API 可用时）', () => {
    render(<XProChart rows={[row(0)]} market="CRYPTO" height={320} />)
    expect(calls.setHeight).toEqual([80])
  })
})

describe('接页分轨锁（详情抽屉：CN 走 MinuteView，US/CRYPTO 走 XProChart）', () => {
  it('CN 代码仍渲染 MinuteView，不创建专业图（CN 链零回归）', () => {
    render(<StockDetailDrawer open code="600000.SH" name="浦发" onClose={() => {}} />)
    expect(screen.getByTestId('minuteview-stub')).toBeInTheDocument()
    expect(calls.createChart).toHaveLength(0)
  })

  it('BTCUSDT 渲染专业图并自拉币安链日 K，不再挂 MinuteView', async () => {
    fetchBinanceKline.mockResolvedValue([row(0)])
    render(<StockDetailDrawer open code="BTCUSDT" name="比特币" onClose={() => {}} />)
    await waitFor(() => expect(fetchBinanceKline).toHaveBeenCalledWith('CRYPTO', 'BTCUSDT', 180))
    expect(screen.getByTestId('xpro-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('minuteview-stub')).not.toBeInTheDocument()
  })

  it('AAPL.US 属 US 市场（专业图 + market=US 出参）', async () => {
    fetchBinanceKline.mockResolvedValue([])
    render(<StockDetailDrawer open code="AAPL.US" onClose={() => {}} />)
    await waitFor(() => expect(fetchBinanceKline).toHaveBeenCalledWith('US', 'AAPL.US', 180))
    expect(screen.getByTestId('xpro-chart')).toBeInTheDocument()
  })
})
