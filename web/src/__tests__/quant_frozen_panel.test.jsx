// ── §QMT-FROZEN 量化页冻结态渲染回归 quant_frozen_panel.test.jsx ──
// 文件职责：A股总开关 cn_master=false（2026-09-24 裁决：广州执行机不再是部署目标）时，
// 量化交易页链路状态卡必须说"冻结/未生效"，而不是任何看起来像运行中的值：
//   1) 冻结横幅出现且 /api/qmt/state 从未拉到（503）时不再无限「加载中」；
//   2) 「休市未探测」「暂无回报（非交易时段属正常）」「当前：miniQMT兼容」三类错误借口绝迹；
//   3) 执行表面板与控制保留（owner 规则：实现而非删除），但网关切换按钮禁用并带 tooltip。
// mock 方式沿用同目录 quant.test.jsx（整页 API mock + 直接 setCnMaster 喂裁决模块缓存，
// 生产链路由 App.jsx 的状态轮询写入，见 gatewayLinkState.js 头注释）。
// English: the Quant link-status card must render the frozen verdict honestly.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

vi.mock('../api/index.js', () => ({
  getAccount: () => 'admin',
  isForbidden: (e) => !!(e && e.status === 403),
  fetchQMTConfig: vi.fn(async () => ({
    enabled: true, mode: 'auto', price_type: 'market', auto_sell: false,
    gateway_url: 'http://gw-guangzhou.invalid:8789', token_masked: '****', // 广州时代残留配置（假主机名：真实内网/公网地址不进测试夹具）
    fixed_amount: 10000, max_positions: 10, initial_capital: 100000,
    max_order_amount: 150000,
    daily_max_buys: 20, daily_budget_amount: 100000, miss_heartbeat_sec: 120,
    known_strategies: [], strategies: [], strategy_amounts: {},
  })),
  // 冻结后网关侧端点常态：state 503（executor Noop）、broker 拿不到
  fetchQMTState: vi.fn(async () => { throw Object.assign(new Error('503'), { status: 503 }) }),
  fetchQMTBroker: vi.fn(async () => { throw new Error('unreachable') }),
  fetchQMTTrades: vi.fn(async () => ({})),
  fetchQMTOrders: vi.fn(async () => { throw Object.assign(new Error('503'), { status: 503 }) }),
  fetchQMTSettleHistory: vi.fn(async () => ({ items: [] })),
  fetchSignalVerdicts: vi.fn(async () => ({ verdicts: [] })),
  fetchRiskGates: vi.fn(async () => { throw Object.assign(new Error('503'), { status: 503 }) }),
  fetchShortStatus: vi.fn(async () => ({ short_enabled: false })),
  fetchPaperState: vi.fn(async () => ({ short_book: { enabled: false } })),
  switchQMTBroker: vi.fn(async () => ({})),
  updateQMTConfig: vi.fn(async () => ({})),
  toggleShort: vi.fn(async () => ({ short_enabled: true })),
}))

import Quant from '../pages/Quant.jsx'
import * as api from '../api/index.js'
import { setCnMaster, FROZEN_HINT_SHORT } from '../gatewayLinkState'

describe('Quant 链路状态卡 · 冻结态（§QMT-FROZEN）', () => {
  afterEach(() => setCnMaster(null)) // 复位模块缓存，防冻结态泄漏进其他用例

  it('cn_master=false：渲染冻结横幅 + 未生效占位，且不再出现三类假活借口', async () => {
    setCnMaster(false)
    render(<Quant />)
    // 冻结横幅每页一次（完整裁决句，含"广州执行机不再是部署目标"）
    await screen.findByText(/A股实盘链路已冻结维护/)
    // state 503 时旧版永远停在「链路状态加载中…」占位——冻结必须出卡
    expect(screen.queryByText(/加载中…/)).toBeNull()
    // 错误借口绝迹：休市/非交易时段/默认 miniQMT 都不许再冒充"等一会儿就好"
    expect(screen.queryByText('休市未探测')).toBeNull()
    expect(screen.queryByText(/暂无回报（非交易时段属正常）/)).toBeNull()
    expect(screen.queryByText(/当前：miniQMT兼容/)).toBeNull()
    expect(screen.queryByText(/gw-guangzhou\.invalid/)).toBeNull() // 陈旧网关 URL 不再回显
    // 未生效/无数据占位如实出现
    expect(screen.getAllByText(/未生效/).length).toBeGreaterThan(0)
    expect(screen.getByText(/无数据（网关不再上报）/)).toBeInTheDocument()
    // 熔断时间戳为零时如实「从未触发」——冻结页此位由未生效接管
    expect(screen.getByText(/未生效（链路冻结，健康探测不再运行）/)).toBeInTheDocument()
  })

  it('冻结时网关切换按钮保留但禁用，tooltip 说清原因（实现而非删除）', async () => {
    setCnMaster(false)
    const { container } = render(<Quant />)
    await screen.findByText(/A股实盘链路已冻结维护/)
    // TDesign 把 disabled 按钮渲染成 <div class="t-button t-is-disabled">（不再是 <button>），
    // 所以按 .t-button 类名定位宿主；禁用态 = t-is-disabled 类，tooltip = title 属性。
    const hostOf = (t) => Array.from(container.querySelectorAll('.t-button')).find((b) => b.textContent.includes(t))
    for (const label of ['切到 miniQMT', '切到 QMT桥']) {
      const b = hostOf(label)
      expect(b, `按钮「${label}」必须保留（隐藏可以、删除不行）`).toBeTruthy()
      expect(b.className).toContain('t-is-disabled')
      expect(b.getAttribute('title')).toContain(FROZEN_HINT_SHORT)
    }
  })

  /* 「禁用」不能只是看起来禁用：TDesign 的禁用态是 <div class="t-button t-is-disabled">，
     点击到底还走不走 onClick（进而弹「切换执行通道」确认框、发后端请求）只有真点一次才知道。
     断言取两个可观察量：确认框文案不得出现 + switchQMTBroker 不得被调用。
     注意别把这条写成恒绿——"没调后端"单独看不算数（弹了确认框没点确认同样不调），
     所以必须连确认框一起断言。 */
  it('冻结时真的点不动：点击不弹切换确认框、不发后端请求', async () => {
    setCnMaster(false)
    const { container } = render(<Quant />)
    await screen.findByText(/A股实盘链路已冻结维护/)
    const hostOf = (t) => Array.from(container.querySelectorAll('.t-button')).find((b) => b.textContent.includes(t))
    for (const label of ['切到 miniQMT', '切到 QMT桥']) {
      fireEvent.click(hostOf(label))
    }
    await new Promise((r) => setTimeout(r, 50)) // 给异步确认框/请求一个落地窗口
    expect(screen.queryByText(/切换执行通道/)).toBeNull()
    expect(api.switchQMTBroker).not.toHaveBeenCalled()
  })
})
