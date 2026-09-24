// ── §市场分家-1 全局市场过滤回归 mkt_split_p1.test.jsx ──
// 文件职责：用户缺陷「顶部切到美股/加密货币后页面仍是 A股信息」的六把行为锁——
//   ① 命令面板代码识别放宽到三市场形态（stockLikeQuery 纯函数，裸字母 ticker 故意不算）；
//   ② Signals 非 CN tab 渲染去向提示卡（不再挂 CN 表格）；
//   ③ MsgCenter 提醒列表按市场过滤（CN 提醒不窜进 CRYPTO tab）；
//   ④ Watchlist 添加入口拒收非 CN 代码（BTCUSDT 不进 A股监控池）+ 非 CN tab 空态提示；
//   ⑤ Dashboard 非 CN tab 整页早返回（A股聚合链不冒充新市场仪表盘）；
//   ⑥ StockDetailDrawer 现价腿分轨：CN 走 /api/stock/lookup，US/CRYPTO 走 /api/binance/quote，
//      两腿互斥（负向锁：CRYPTO 代码绝不触发 fetchStockLookup）。
// English: §MKT-SPLIT-1 behavioral locks for the global market tab: palette code recognition,
// Signals/MsgCenter/Watchlist/Dashboard market gating, and the drawer's per-market quote leg.
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { MessagePlugin } from 'tdesign-react'
import { MarketProvider } from '../market.jsx'
import { stockLikeQuery } from '../components/CommandPalette.jsx'

// —— api/index.js 端点热换桩（同 §M-9 三态测试的 state.impl 模式）——
const { state } = vi.hoisted(() => ({ state: {} }))
function okPayloads() {
  return {
    isAdmin: () => false,
    getAccount: () => 'tester',
    fetchStatus: () => ({ session: 3, session_label: '交易中' }),
    isTradingSession: (s) => s === 1 || s === 3,
    setLastSession: () => {},
    connectSSE: () => {},
    fetchSignals: () => [],
    fetchPaperState: () => ({ enabled: false }),
    fetchShortStatus: () => ({ short_enabled: false }),
    fetchAlerts: () => [],
    fetchSnapshot: () => [],
    fetchWatchlist: () => ({ stocks: [] }),
    fetchEvaluations: () => [],
    fetchStockLookup: vi.fn(async () => null),
    addWatchlist: vi.fn(async () => ({})),
    removeWatchlist: vi.fn(async () => ({})),
    fetchNews: () => [],
    fetchSectorHot: () => [],
    fetchHotSnapshot: () => [],
    fetchIPOCalendar: () => [],
    fetchDashboard: () => ({}),
    fetchDataSourceHealth: () => ({}),
    fetchNewsSourceHealth: () => ({}),
    fetchEngineHealth: () => ({ engine: {} }),
    fetchEmotionHistory: () => ({ series: [] }),
    fetchEmotionStrategyMatrix: () => ({ rows: [] }),
  }
}
vi.mock('../api/index.js', async () => {
  const actual = await vi.importActual('../api/index.js')
  const stubs = {}
  for (const name of Object.keys(okPayloads())) {
    stubs[name] = vi.fn(async (...args) => state.impl[name](...args))
  }
  return { ...actual, ...stubs }
})

// —— 抽屉现价腿双端点桩 + lightweight-charts 全假（jsdom 无 canvas，同 P5 测试做法）——
const quoteCalls = []
vi.mock('../api/binance.js', async () => {
  const actual = await vi.importActual('../api/binance.js')
  return {
    ...actual,
    fetchBinanceQuote: vi.fn(async (market, code) => { quoteCalls.push([market, code]); return { ok: true, price: 50000.5, change_pct: 2.5 } }),
    fetchBinanceKline: vi.fn(async () => []),
  }
})
vi.mock('lightweight-charts', () => ({  createChart: () => ({
    addSeries: () => ({ setData: () => {}, applyOptions: () => {}, priceScale: () => ({ applyOptions: () => {} }) }),
    panes: () => [{ setHeight: null }],
    timeScale: () => ({ fitContent: () => {} }),
    priceScale: () => ({ applyOptions: () => {} }),
    applyOptions: () => {},
    remove: () => {},
  }),
  CandlestickSeries: { name: 'Candlestick' },
  HistogramSeries: { name: 'Histogram' },
  PriceScaleMode: { Normal: 0, Logarithmic: 1, Percentage: 2 },
}))
// CN 分支抽屉体会挂真 MinuteView（内部还有分时/盘口轮询），桩掉只留占位
vi.mock('../components/MinuteView.jsx', () => ({ default: ({ code }) => <div data-testid="minuteview-stub">{code}</div> }))

import Signals from '../pages/Signals.jsx'
import MsgCenter from '../pages/MsgCenter.jsx'
import Watchlist from '../pages/Watchlist.jsx'
import Dashboard from '../pages/Dashboard.jsx'
import StockDetailDrawer from '../components/StockDetailDrawer.jsx'

// withTab 在指定 ?m= 市场下挂载页面（MarketProvider 读 URL 初值，与主应用语义一致）
function withTab(node, m) {
  return render(
    <MemoryRouter initialEntries={['/x?m=' + m]}>
      <MarketProvider>{node}</MarketProvider>
    </MemoryRouter>
  )
}

describe('§市场分家-1 全局市场过滤', () => {
  beforeEach(async () => {
    cleanup()
    localStorage.clear()
    quoteCalls.length = 0
    state.impl = okPayloads()
    const api = await import('../api/index.js')
    api.fetchStockLookup.mockImplementation(async () => null)
    api.addWatchlist.mockClear()
  })
  afterEach(() => cleanup())

  // T1 命令面板代码形态识别：三市场正例 + 自由文本/裸字母 ticker 反例
  it('T1 stockLikeQuery 三市场形态', () => {
    expect(stockLikeQuery('600519')).toBe('600519')
    expect(stockLikeQuery('600519.sh')).toBe('600519.SH')
    expect(stockLikeQuery('aapl.us')).toBe('AAPL.US')
    expect(stockLikeQuery('BRK.B')).toBe('BRK.B')
    expect(stockLikeQuery('btcusdt')).toBe('BTCUSDT')
    expect(stockLikeQuery('ETHUSDT')).toBe('ETHUSDT')
    expect(stockLikeQuery('AAPL')).toBeNull()   // 裸字母 ticker 与页面搜索词冲突，故意不算
    expect(stockLikeQuery('help')).toBeNull()
    expect(stockLikeQuery('设置')).toBeNull()
    expect(stockLikeQuery('ABBTC')).toBeNull()  // 币对基座 <3 字母
    expect(stockLikeQuery('')).toBeNull()
  })

  // T2 Signals：CRYPTO tab 渲染去向提示卡，CN 信号一列都不出现
  it('T2 Signals 非 CN tab → 去向提示卡（不挂 CN 表格）', async () => {
    state.impl.fetchSignals = async () => ([{ code: '600519', name: '贵州茅台', strategy: 'N形', action: 'buy', date: '2026-09-24' }])
    withTab(<Signals />, 'CRYPTO')
    const card = await screen.findByTestId('signals-market-empty', {}, { timeout: 5000 })
    expect(card.textContent).toContain('加密货币')
    expect(screen.queryByText('贵州茅台'), '§市场分家：CRYPTO tab 不得渲染 A股信号').not.toBeInTheDocument()
  })

  // T3 MsgCenter：提醒列表按市场过滤——CRYPTO tab 只剩币安链提醒
  it('T3 MsgCenter 提醒按市场过滤（CN 提醒不窜进 CRYPTO tab）', async () => {
    state.impl.fetchAlerts = async () => ([
      { code: '600519', name: '贵州茅台', level: '持仓提示', body: 'CN提醒', time: '2026-09-24 10:00' },
      { code: 'BTCUSDT', level: '交易信号', body: '币安成交回报', strategy: 'xasset', time: '2026-09-24 10:01' },
    ])
    withTab(<MsgCenter />, 'CRYPTO')
    expect(await screen.findByText('币安成交回报', {}, { timeout: 5000 })).toBeInTheDocument()
    expect(screen.queryByText('CN提醒'), '§市场分家：CN 提醒不得出现在 CRYPTO tab').not.toBeInTheDocument()
  })

  // T4 Watchlist 添加入口拒收非 CN（正防锁：addWatchlist 零调用 + 中文报错，无静默入池）
  it('T4 Watchlist 添加 BTCUSDT → 拒收且不落后端', async () => {
    const errSpy = vi.spyOn(MessagePlugin, 'error').mockImplementation(() => {})
    render(<Watchlist />)
    const input = screen.getByPlaceholderText(/输入代码/)
    fireEvent.change(input, { target: { value: 'BTCUSDT' } })
    fireEvent.click(screen.getByRole('button', { name: /添加/ }))
    await waitFor(() => expect(errSpy).toHaveBeenCalled())
    const api = await import('../api/index.js')
    expect(api.addWatchlist, '§市场分家：非 CN 代码不得进自选池后端').not.toHaveBeenCalled()
    expect(errSpy.mock.calls[0][0]).toContain('仅支持 A股')
    errSpy.mockRestore()
  })

  // T5 Watchlist 非 CN tab：列表清空并给去向提示（缓存里有 A股自选也不渲染表格）
  it('T5 Watchlist CRYPTO tab → 去向提示卡（A股自选不渲染）', async () => {
    state.impl.fetchWatchlist = async () => ({ stocks: [{ code: '600519', name: '贵州茅台', price: 1700 }] })
    state.impl.fetchSnapshot = async () => ([{ code: '600519', name: '贵州茅台', price: 1700, change_pct: 1 }])
    withTab(<Watchlist />, 'CRYPTO')
    const hint = await screen.findByTestId('watchlist-market-empty', {}, { timeout: 5000 })
    expect(hint.textContent).toContain('加密货币')
    expect(screen.queryByText('贵州茅台'), '§市场分家：CRYPTO tab 不得渲染 A股自选').not.toBeInTheDocument()
  })

  // T6 Dashboard 非 CN tab 整页早返回（A股聚合链不冒充新市场仪表盘）
  it('T6 Dashboard US tab → 整页去向提示', async () => {
    withTab(<Dashboard />, 'US')
    const hint = await screen.findByTestId('dashboard-market-empty', {}, { timeout: 5000 })
    expect(hint.textContent).toContain('美股')
    expect(screen.queryByText('热门个股'), '§市场分家：US tab 不得渲染 A股看板').not.toBeInTheDocument()
  })

  // T7/T8 抽屉现价腿分轨（双向互斥锁）
  it('T7 抽屉打开 BTCUSDT → 只打 /api/binance/quote，绝不碰 CN lookup', async () => {
    const api = await import('../api/index.js')
    render(<StockDetailDrawer open code="BTCUSDT" name="BTC" onClose={() => {}} />)
    await waitFor(() => expect(quoteCalls.length).toBeGreaterThan(0), { timeout: 5000 })
    expect(quoteCalls[0]).toEqual(['CRYPTO', 'BTCUSDT'])
    expect(await screen.findByText('50000.50')).toBeInTheDocument() // ok 载荷回填头部现价
    expect(api.fetchStockLookup, '§市场分家：新市场不得回退 CN 四级链').not.toHaveBeenCalled()
  })
  it('T8 抽屉打开 600519 → 只打 CN lookup，绝不碰 binance quote', async () => {
    const api = await import('../api/index.js')
    render(<StockDetailDrawer open code="600519" name="贵州茅台" onClose={() => {}} />)
    await waitFor(() => expect(api.fetchStockLookup.mock.calls.length).toBeGreaterThan(0), { timeout: 5000 })
    expect(quoteCalls, '§市场分家：CN 代码不得打币安现价端点').toHaveLength(0)
  })
})
