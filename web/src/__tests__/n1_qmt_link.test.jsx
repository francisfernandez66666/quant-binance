// ── §LINTGATE（2026-09-24 抄母仓可抄榜项 8）概览页「实盘链路」指示行为锁 ──
//
// 缺陷回音壁：Dashboard.jsx:178 曾把 setQmtState 误写成 setQMTState（未定义符号），
// ReferenceError 被同函数 :179 的空 catch 吞掉 → qmtState 恒 null → qmtLine 恒 '' →
// 「实盘链路」行**永不渲染**，且 15s 轮询每个 tick 静默抛一次。
// 为什么必须有这条：类型检查不覆盖 .jsx、vitest 此前也未渲染该卡片 ⇒ 只有静态 lint（no-undef）
// 能抓拼写型未定义符号，而"整行不渲染"这种缺失型缺陷光有 lint 门又没人实证它真在渲染——
// 所以 lint 门防再犯，本用例防静默回退（两者配对，缺一即只剩一半保障）。
// 载荷：mock fetchQMTState 回 {enabled:true,last_probe_ok:true,mode:"auto"} → 概览页必须出现
// 文本「实盘链路」（改个变量名不重要，这条断言才是锁）。
// English: behavior lock for the §LINTGATE batch — with a healthy QMT status payload the overview
// page MUST render the「实盘链路」indicator; guards against another silently-swallowed
// undefined-symbol regression that no type checker would have caught.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const { state } = vi.hoisted(() => ({ state: {} }))

// 默认载荷=正常空态（与本仓 m9_error_tri_state 的 Dashboard 载荷表同口径），
// 用例只覆盖 QMT 状态这一个面，其余端点回零值以免挂载报错。
function okPayloads() {
  return {
    isAdmin: () => false,
    getAccount: () => 'admin',
    fetchStatus: () => ({ session: 'test', signal_count: 0 }),
    setLastSession: () => {},
    connectSSE: () => {},
    fetchSignals: () => [],
    fetchPaperState: () => ({ enabled: false }),
    fetchShortStatus: () => ({ short_enabled: false }),
    fetchAlerts: () => [],
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
    // §LINTGATE 锁眼载荷：实盘链路启用 + 最近探测成功 + 自动模式
    fetchQMTState: () => ({ enabled: true, last_probe_ok: true, mode: 'auto', tripped: false }),
  }
}

// 名单内端点全部转发到 state.impl：用例运行期热换实现即可制造启用/熔断/未启用三态
vi.mock('../api/index.js', async () => {
  const actual = await vi.importActual('../api/index.js')
  const stubs = {}
  for (const name of Object.keys(okPayloads())) {
    stubs[name] = vi.fn(async (...args) => state.impl[name](...args))
  }
  return { ...actual, ...stubs }
})

import Dashboard from '../pages/Dashboard.jsx'

describe('§LINTGATE 概览页「实盘链路」指示渲染', () => {
  beforeEach(async () => {
    cleanup()
    localStorage.clear()
    state.impl = okPayloads()
    const api = await import('../api/index.js')
    for (const name of Object.keys(state.impl)) {
      if (typeof api[name]?.mockClear === 'function') api[name].mockClear()
    }
  })
  afterEach(() => cleanup())

  // E1：健康载荷 → 必须出现「实盘链路」，且摘要含 ●（探测成功）与「自动」（mode=auto）。
  // 旧 ReferenceError 形态下这一行永不渲染，故本断言即该缺陷的行为锁。
  it('E1 qmt/status 健康 → 概览页渲染「实盘链路」（旧未定义符号形态下此处永不渲染）', async () => {
    render(<MemoryRouter><Dashboard /></MemoryRouter>)
    const line = await screen.findByText(/实盘链路/, {}, { timeout: 5000 })
    expect(line).toBeInTheDocument()
    expect(line.textContent).toMatch(/实盘链路：● 自动 正常/)
  })

  // E2：熔断载荷 → 同一指示必须如实带「⚠熔断」（防有人把 qmtLine 改回常量占位伪健康）。
  it('E2 qmt/status 熔断 → 实盘链路行带熔断原因', async () => {
    state.impl.fetchQMTState = () => ({
      enabled: true, last_probe_ok: false, mode: 'auto', tripped: true, trip_reason: '连续失败',
    })
    render(<MemoryRouter><Dashboard /></MemoryRouter>)
    const line = await screen.findByText(/实盘链路/, {}, { timeout: 5000 })
    expect(line.textContent).toMatch(/○.*⚠熔断:连续失败/)
  })

  // E3：链路未启用 → 整行不渲染（保持既有语义，防改成永远占位的伪指示）。
  it('E3 qmt/status enabled=false → 不渲染实盘链路行', async () => {
    state.impl.fetchQMTState = () => ({ enabled: false })
    const { container } = render(<MemoryRouter><Dashboard /></MemoryRouter>)
    // 等首个轮询 tick 落定后再断言缺席（findBy 超时会直接判红，故用短延时）
    await new Promise((r) => setTimeout(r, 120))
    expect(container.textContent).not.toMatch(/实盘链路/)
  })
})
