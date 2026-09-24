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
// §QMT-FROZEN-Z4 增补（2026-09-25）：E4/E5 锁冻结态这一支——E4 管"冻结位已知"，E5 管
// "冻结位比首帧晚到"（生产真实时序），后者才钉得住 useMemo 少写一个依赖的静默失效。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, act } from '@testing-library/react'
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
// §QMT-FROZEN：冻结裁决模块缓存的生产写入方是 App.jsx 状态轮询；测试直接喂同一入口，
// 不 mock 判定逻辑本身（用例结束复位为 null，防跨用例泄漏）。
import { setCnMaster, FROZEN_HINT_SHORT } from '../gatewayLinkState'

describe('§LINTGATE 概览页「实盘链路」指示渲染', () => {
  beforeEach(async () => {
    cleanup()
    localStorage.clear()
    setCnMaster(null) // §QMT-FROZEN：裁决缓存复位为「未知」，冻结用例显式置 false
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
    // §QMT-FROZEN 措辞修正：熔断未触发如实说「从未触发」，不再用「正常」冒充熔断机制在运转
    expect(line.textContent).toMatch(/实盘链路：● 自动 从未触发/)
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

  // E4（§QMT-FROZEN）：cn_master=false → 即使 qmtState 从未拉到（网关侧 503/空），
  // 实盘链路行也必须强制渲染并明说「冻结维护」——旧写法此时返回 ''，
  // 「链路不存在」与「接口没数据」两种语义在界面上静默成同一种缺失。
  it('E4 cn_master=false → 实盘链路行强制渲染冻结文案（qmtState 为 null 也不例外）', async () => {
    setCnMaster(false)
    state.impl.fetchQMTState = () => { throw Object.assign(new Error('503'), { status: 503 }) }
    render(<MemoryRouter><Dashboard /></MemoryRouter>)
    const line = await screen.findByText(/实盘链路/, {}, { timeout: 5000 })
    expect(line.textContent).toContain('冻结维护')
    expect(line.textContent).toContain(FROZEN_HINT_SHORT)
  })

  // E5（§QMT-FROZEN-Z4）：真正的生产时序是 **cn_master 晚于首帧到达**——App.jsx 的 /api/status
  // 轮询在 Dashboard 挂载之后才把冻结位写进裁决模块缓存。旧写法把 cnMaster 当普通模块变量读、
  // useMemo 只依赖 [qmtState]，而网关 503 时 qmtState 永远停在 null 不再变化 ⇒ 缓存变了也没人重算
  // ⇒ 整行静默消失（正是 §LINTGATE 那条"链路行永不渲染"的事故形状，只是换了成因）。
  // E4 在渲染前就置好了冻结位，看不见这个形态；本用例把它按到达顺序钉住：
  // 首帧确实没有这一行 → 冻结位到达后必须自己冒出来。
  it('E5 冻结位在首帧之后才到达 → 实盘链路行必须随订阅自唤醒（旧 [qmtState] 依赖下永不出现）', async () => {
    state.impl.fetchQMTState = () => { throw Object.assign(new Error('503'), { status: 503 }) }
    const { container } = render(<MemoryRouter><Dashboard /></MemoryRouter>)
    // 等首个轮询 tick 落定：此时 cnMaster 仍是 null=未知，按口径整行不渲染（不是缺陷，是 E3 语义）
    await new Promise((r) => setTimeout(r, 120))
    expect(container.textContent).not.toMatch(/实盘链路/)
    // App 的状态轮询此刻才到达：只写缓存、不重新挂载组件，界面靠订阅腿自己醒来
    act(() => setCnMaster(false))
    const line = await screen.findByText(/实盘链路/, {}, { timeout: 5000 })
    expect(line.textContent).toContain(FROZEN_HINT_SHORT)
  })
})
