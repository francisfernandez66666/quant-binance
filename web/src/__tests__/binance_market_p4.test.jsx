// ── §BINANCE-P4 前端市场维度单测 ──
// 文件职责（PLAN §12.2 测试金字塔·前端）：fmtMoneyM/fmtQty/parseCode 纯函数断言 +
// 市场 Tab（MarketProvider/MarketTabs）切换夹具。parseCode 用例与 Go
// internal/config/broker.go TestInferMarketOf 对齐——两端规则漂移在此先撞红。
// English: vitest for the market-dimension pure functions and the tab-switch fixture,
// kept in lockstep with the Go InferMarketOf table test.
import React from 'react'
import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { fmtMoneyM, fmtQty, parseCode, marketOf, qtyUnit, roundQtyPreview } from '../utils.market.js'
import { MarketProvider, MarketTabs, useMarket, MARKET_ALL } from '../market.jsx'

describe('parseCode（镜像 Go InferMarketOf 判定顺序）', () => {
  it('A股带后缀 → CN，裸码剥离后缀', () => {
    expect(parseCode('600519.SH')).toEqual({ market: 'CN', symbol: '600519' })
    expect(parseCode('000001.SZ')).toEqual({ market: 'CN', symbol: '000001' })
    expect(parseCode('830799.BJ')).toEqual({ market: 'CN', symbol: '830799' })
  })
  it('币安计价币尾缀 → CRYPTO', () => {
    expect(parseCode('BTCUSDT').market).toBe('CRYPTO')
    expect(parseCode('ethusdc').market).toBe('CRYPTO') // 大小写不敏感（Go 先 ToUpper）
    expect(parseCode('BNBBTC').market).toBe('CRYPTO')  // 以 BTC 收尾也算币对
  })
  it('美股 ticker / 点分代码 → US', () => {
    expect(parseCode('AAPL')).toEqual({ market: 'US', symbol: 'AAPL' })
    expect(parseCode('BRK.B')).toEqual({ market: 'US', symbol: 'BRK.B' })
  })
  it('裸 6 位数字与空串缺省 CN（加市场不换默认）', () => {
    expect(parseCode('600000').market).toBe('CN')
    expect(parseCode('').market).toBe('CN')
    expect(marketOf('BTCUSDT')).toBe('CRYPTO')
  })
})

describe('fmtMoneyM 三市场货币口径', () => {
  it('CN ¥ 两位小数', () => {
    expect(fmtMoneyM(434.99999999999994, 'CN')).toBe('¥435.00')
    expect(fmtMoneyM(435)).toBe('¥435.00') // 缺省 CN
  })
  it('US $ 两位小数', () => {
    expect(fmtMoneyM(12.345, 'US')).toBe('$12.35') // 与 CN 同为 2 位：美分口径
  })
  it('CRYPTO $ 最多 8 位去尾零', () => {
    expect(fmtMoneyM(0.000123456, 'CRYPTO')).toBe('$0.00012346') // 第 9 位起四舍五入
    expect(fmtMoneyM(50000.5, 'CRYPTO')).toBe('$50000.5')        // 尾零剥除
    expect(fmtMoneyM(1, 'CRYPTO')).toBe('$1')                     // 整数不留小数点
  })
  it('非法值统一 "-"', () => {
    for (const m of ['CN', 'US', 'CRYPTO']) {
      expect(fmtMoneyM(null, m)).toBe('-')
      expect(fmtMoneyM(NaN, m)).toBe('-')
    }
  })
})

describe('fmtQty 三市场数量口径', () => {
  it('CN 整手取整（不出现小数）', () => {
    expect(fmtQty(100.6, 'CN')).toBe('101')
    expect(fmtQty(100, 'CN')).toBe('100')
    expect(fmtQty(100)).toBe('100') // 缺省 CN
  })
  it('US 整股直显 / 碎股 ≤4 位去尾零', () => {
    expect(fmtQty(7, 'US')).toBe('7')
    expect(fmtQty(0.5, 'US')).toBe('0.5')
    expect(fmtQty(0.123456, 'US')).toBe('0.1235')
  })
  it('CRYPTO 小数量 4 位有效 / 常规 ≤8 位去尾零', () => {
    expect(fmtQty(0.00123456, 'CRYPTO')).toBe('0.001235')
    expect(fmtQty(2.5, 'CRYPTO')).toBe('2.5')
    expect(fmtQty(0.1, 'CRYPTO')).toBe('0.1')
  })
  it('单位词与预览取整', () => {
    expect(qtyUnit('CN')).toBe('股')
    expect(qtyUnit('CRYPTO')).toBe('枚')
    expect(roundQtyPreview(250, 'CN')).toBe(200)   // 整手向下
    expect(roundQtyPreview(2.7, 'US')).toBe(2)     // 整股预览
    expect(roundQtyPreview(0.33, 'CRYPTO')).toBe(0.33) // 加密原样（stepSize 后端裁）
  })
})

// Tab 切换夹具：Provider 挂 MemoryRouter（模拟主应用 HashRouter 环境），
// 断言 1) 初值 ALL（不过滤，CN 零回归）2) 点击改市场 3) 消费端 useMarket/match 同步。
function Probe() {
  const { market, match } = useMarket()
  return (
    <div>
      <span data-testid="cur">{market}</span>
      <span data-testid="match-btc">{String(match(undefined, 'BTCUSDT'))}</span>
      <MarketTabs />
    </div>
  )
}

describe('市场 Tab 切换（§11.2 全局）', () => {
  it('缺省 ALL：不过滤任何市场', () => {
    render(<MemoryRouter><MarketProvider><Probe /></MarketProvider></MemoryRouter>)
    expect(screen.getByTestId('cur').textContent).toBe(MARKET_ALL)
    expect(screen.getByTestId('match-btc').textContent).toBe('true')
  })
  it('点「加密货币」后仅 CRYPTO 行通过过滤', () => {
    render(<MemoryRouter initialEntries={['/positions']}><MarketProvider><Probe /></MarketProvider></MemoryRouter>)
    fireEvent.click(screen.getByRole('tab', { name: '加密货币' }))
    expect(screen.getByTestId('cur').textContent).toBe('CRYPTO')
    expect(screen.getByTestId('match-btc').textContent).toBe('true')
    fireEvent.click(screen.getByRole('tab', { name: 'A股' }))
    expect(screen.getByTestId('cur').textContent).toBe('CN')
    expect(screen.getByTestId('match-btc').textContent).toBe('false')
  })
  it('URL ?m=US 初值恢复（可分享链接语义）', () => {
    render(<MemoryRouter initialEntries={['/positions?m=US']}><MarketProvider><Probe /></MarketProvider></MemoryRouter>)
    expect(screen.getByTestId('cur').textContent).toBe('US')
  })
})
