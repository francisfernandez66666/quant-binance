// ── §BINANCE-P5 XProChart（lightweight-charts 试点）单测 ──
// 文件职责（PLAN_ENHANCE §4 验收）：数据适配纯函数三锁——
//   ① dateToUtcSeconds：YYYY-MM-DD/YYYYMMDD/带时刻 ISO 手工拆位归 UTC 零点秒（禁宿主时区漂移），
//      非法月/日/串一律 NaN；
//   ② toChartSeries：时间严格升序 + 同日去重留档取新、脏行整行跳过并计数、缺量记 0 不丢蜡烛、
//      量柱颜色跟涨跌；
//   ③ priceFormatFor：<1 低价币 8 位精度、常规两位、空序列两位。
// 组件侧只做挂载冒烟（vi.mock 桩掉 lightweight-charts，jsdom 无 canvas 上下文）：受控 rows
// 走完 setData、空态注记「有轴无图」不弹错误页。
// English: vitest for XProChart pure adapters (timezone-free day keys, ascending/dedup/skip
// series adaptation, micro-price precision) plus a mocked-library mount smoke.
import React from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

// vi.hoisted：vi.mock 工厂被提升到模块顶层之前，桩实现引用的 fn 必须同批提升（TDZ 铁律）。
const mocks = vi.hoisted(() => ({
  setDataCandles: vi.fn(),
  setDataVolumes: vi.fn(),
  addSeries: vi.fn(),
  createChart: vi.fn(),
}))
const { setDataCandles, setDataVolumes, addSeries } = mocks
vi.mock('lightweight-charts', () => ({
  createChart: mocks.createChart.mockImplementation(() => ({
    addSeries: mocks.addSeries,
    priceScale: () => ({ applyOptions: vi.fn() }),
    timeScale: () => ({ fitContent: vi.fn() }),
    panes: () => [{ setHeight: vi.fn() }, { setHeight: vi.fn() }],
    remove: vi.fn(),
  })),
  CandlestickSeries: { c: 1 },
  HistogramSeries: { h: 1 },
  PriceScaleMode: { Normal: 0, Logarithmic: 1 },
}))

import XProChart, {
  toChartSeries, dateToUtcSeconds, priceFormatFor, DAY_SECONDS,
} from '../components/XProChart.jsx'

// 每个用例前复位假序列：按系列类型（candlestick/volume）路由到各自的 setData 捕获器
beforeEach(() => {
  addSeries.mockReset()
  addSeries.mockImplementation((series) => ({
    setData: series && series.c ? setDataCandles : setDataVolumes,
    applyOptions: vi.fn(),
    priceScale: () => ({ applyOptions: vi.fn() }),
  }))
  setDataCandles.mockReset()
  setDataVolumes.mockReset()
})

describe('dateToUtcSeconds（UTC 日键手工拆位，禁时区漂移）', () => {
  it('三种合法形态归同一 UTC 零点秒', () => {
    const want = Date.UTC(2026, 0, 5) / 1000
    expect(dateToUtcSeconds('2026-01-05')).toBe(want)
    expect(dateToUtcSeconds('20260105')).toBe(want)
    expect(dateToUtcSeconds('2026-01-05T16:40:00Z')).toBe(want)
  })
  it('非法形态 NaN（月越界/串脏/空）', () => {
    expect(dateToUtcSeconds('2026-13-01')).toBeNaN()
    expect(dateToUtcSeconds('2026-01-32')).toBeNaN()
    expect(dateToUtcSeconds('')).toBeNaN()
    expect(dateToUtcSeconds(null)).toBeNaN()
  })
  it('数字入参直传（已是秒的宿主数据不再换算）', () => {
    expect(dateToUtcSeconds(1767571200)).toBe(1767571200)
  })
  it('DAY_SECONDS 常量=86400（窗口换算基准）', () => {
    expect(DAY_SECONDS).toBe(86400)
  })
})

describe('toChartSeries（升序硬约束 + 脏行诚实丢弃）', () => {
  const row = (date, close, extra = {}) => ({ date, open: 100, high: 101, low: 99, close, volume: 10, ...extra })
  it('乱序输入 → 输出严格按时间升序（lightweight-charts 对乱序直接 throw）', () => {
    const { candles } = toChartSeries([row('2026-01-07', 105), row('2026-01-05', 102), row('2026-01-06', 103)])
    expect(candles.map((c) => c.time)).toEqual([
      dateToUtcSeconds('2026-01-05'), dateToUtcSeconds('2026-01-06'), dateToUtcSeconds('2026-01-07'),
    ])
  })
  it('同日重复 → 只留最后一条（补档取新）', () => {
    const { candles, skipped } = toChartSeries([row('2026-01-05', 102), row('2026-01-05', 109)])
    expect(candles).toHaveLength(1)
    expect(candles[0].close).toBe(109)
    expect(skipped).toBe(0)
  })
  it('OHLC 任一脏 → 整行跳过并计数；缺量记 0 不丢蜡烛', () => {
    const { candles, volumes, skipped } = toChartSeries([
      row('2026-01-05', 102, { high: 'x' }),
      row('2026-01-06', 103, { volume: null }),
      { date: '坏日期' }, null,
    ])
    expect(skipped).toBe(3) // high 脏 1 + 坏日期 1 + null 1
    expect(candles).toHaveLength(1)
    expect(volumes[0].value).toBe(0) // 缺量记 0，蜡烛保留
  })
  it('量柱色随涨(收≥开)/跌(收<开)分档', () => {
    const { volumes } = toChartSeries([row('2026-01-05', 98), row('2026-01-06', 105)])
    expect(volumes[0].color).not.toBe(volumes[1].color)
  })
  it('空/非数组输入 → 空序列零抛错（受控宿主可直传 undefined）', () => {
    expect(toChartSeries(undefined).candles).toEqual([])
  })
})

describe('priceFormatFor（低价币 8 位精度，常规两位）', () => {
  it('尾部最大绝对价 <1 → precision 8', () => {
    expect(priceFormatFor([{ high: 0.9, low: 0.0001, open: 0.5, close: 0.6 }]).precision).toBe(8)
  })
  it('常规价与空序列 → precision 2', () => {
    expect(priceFormatFor([{ high: 45000, low: 43000, open: 44000, close: 44500 }]).precision).toBe(2)
    expect(priceFormatFor([]).precision).toBe(2)
  })
})

describe('XProChart 挂载冒烟（vi.mock 桩库，jsdom 无 canvas）', () => {
  it('受控 rows → 双序列 setData 被喂；空态只注记不弹错', () => {
    const rows = [
      { date: '2026-01-05', open: 100, high: 101, low: 99, close: 102, volume: 7 },
      { date: '2026-01-06', open: 102, high: 103, low: 101, close: 101, volume: 9 },
    ]
    render(<XProChart code="BTCUSDT" market="CRYPTO" rows={rows} />)
    expect(screen.getByTestId('xpro-chart')).toBeInTheDocument()
    expect(setDataCandles).toHaveBeenCalledWith([
      { time: dateToUtcSeconds('2026-01-05'), open: 100, high: 101, low: 99, close: 102 },
      { time: dateToUtcSeconds('2026-01-06'), open: 102, high: 103, low: 101, close: 101 },
    ])
    expect(setDataVolumes).toHaveBeenCalled()
    expect(screen.queryByText(/失败/)).not.toBeInTheDocument()
  })
  it('空 rows → 「暂无历史日线」注记在位（有轴无图惯例）', () => {
    render(<XProChart code="AAPL.US" market="US" rows={[]} />)
    expect(screen.getByText(/暂无历史日线/)).toBeInTheDocument()
  })
})
