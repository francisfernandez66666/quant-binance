// ── §AUDITFIX926-D1（20260926 审计修复批）实盘手动下单「价格偏离 ±15%」二次确认 ──
//
// 缺陷原文：后端 handleExecuteAction 对偏离现价超 ±15% 的委托直接 400 拒绝（人话文案），
//   前端只有一句话 toast「下单失败: …」了事——用户既看不到现价/偏离多少个百分点，也没有
//   任何"我确认就是要按这个价发"的通道；行情瞬时波动时合法委托被静默挡死，只能来回猜。
// 修法（本批已落）：后端 400 体带机器码 code=price_deviation + 原文案；前端 api.request()
//   本就把 err.code 透传出来（§FIX-9d 口径），Positions 提交核 runRealSubmit 按码分流——
//   price_deviation → 弹「价格偏离确认」Dialog，确认=带 confirm_deviation=true 原体重发，
//   取消=关框回执行弹窗改价；其余错误维持原 toast（旧后端不带 code 时行为逐字不变）。
//
// 本文件三把挂载锁（复用 §H-2 的整页挂载姿势，只替换本页触达的 api 端点）：
//   T1 偏离拒单 → 确认框出现（文案=后端原话）→ 确认重发体含 confirm_deviation:true 且成功；
//   T2 偏离拒单 → 点「返回改价」→ 不重发（调用次数停在 1），执行弹窗仍在（可改价再提交）；
//   T3 兼容负锁：无 code 的普通 400 → 只走原 toast，确认框绝不出现（防分流过宽）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'

// api 模块替身：保留真实导出，只覆盖本页触达的端点；state 里放可控的下单结果形态。
const { state } = vi.hoisted(() => ({
  state: {
    // executeRealAction 的行为开关：
    //   'deviation_once' = 首次抛 price_deviation（T1/T2），再次调用成功；
    //   'plain_err'      = 抛不带 code 的普通错误（T3）；
    //   'ok'             = 直接成功。
    execMode: 'ok',
    execCalls: [], // 每次调用的请求体快照（按序），断言重发体 confirm_deviation 用
  },
}))

vi.mock('../api/index.js', async () => {
  const actual = await vi.importActual('../api/index.js')
  return {
    ...actual,
    isAdmin: vi.fn(() => true),
    getAccount: vi.fn(() => 'admin'),
    fetchStatus: vi.fn(async () => ({ session: 'test' })),
    setLastSession: vi.fn(),
    fetchHoldings: vi.fn(async () => ({ holdings: [], available_balance: 100, total_realized_pnl: 0 })),
    // 实盘链路：启用态 + 一行 CN 持仓（现价为成本价，避免盈亏列脏数据）
    fetchQMTState: vi.fn(async () => ({ enabled: true, mode: 'manual', tripped: false, gateway_url: '' })),
    fetchRealPositions: vi.fn(async () => ({
      positions: [{ ts_code: '600000.SH', name: '浦发银行', qty: 1000, cost_price: 10, cur_price: 10, market: 'CN' }],
      account: null,
    })),
    fetchQMTTrades: vi.fn(async () => null),
    fetchRealAdvice: vi.fn(async () => ({ advices: [] })),
    // 下单核：按 state.execMode 决定首次/后续调用形态，并留请求体证据
    executeRealAction: vi.fn(async (body) => {
      state.execCalls.push({ ...body })
      const first = state.execCalls.length === 1
      if (state.execMode === 'deviation_once' && first) {
        throw Object.assign(
          new Error('委托价 15.00 偏离现价 10.00 超 ±15%（50.0%），请核对价格后确认提交'),
          { code: 'price_deviation', status: 400 }
        )
      }
      if (state.execMode === 'plain_err' && first) {
        throw Object.assign(new Error('资金闸余额不足'), { status: 400 })
      }
      return { status: 'accepted', order_id: 'UAT-1' }
    }),
  }
})

import Positions from '../pages/Positions.jsx'

// TDesign Dialog 关闭后节点保留在 DOM（仅隐藏），toBeInTheDocument 恒真——弹窗开合断言
// 一律走可见性判定：jest-dom 的 toBeVisible 会算上祖先 display:none，正好合本形态。
function expectDialogOpen(text, open) {
  const el = screen.queryByText(text)
  if (open) {
    expect(el, `弹窗「${text}」应存在`).toBeInTheDocument()
    expect(el).toBeVisible()
  } else if (el) {
    expect(el).not.toBeVisible()
  }
}

// 打开实盘 Tab → 点该行「清仓」→ 执行弹窗点「确认下单」，走到后端拒单分支
async function submitCloseOrder() {
  fireEvent.click(await screen.findByText('实盘持仓'))
  const closeBtn = await screen.findByText('清仓')
  fireEvent.click(closeBtn)
  const confirm = await screen.findByText('确认下单')
  fireEvent.click(confirm)
}

describe('§AUDITFIX926-D1 实盘下单价格偏离二次确认（弹窗分流 + confirm_deviation 重发）', () => {
  beforeEach(() => {
    cleanup()
    // TDesign toast 挂 document.body，cleanup 不清——先物理清场防残影
    document.querySelectorAll('.t-message').forEach((n) => n.remove())
    localStorage.clear()
    state.execMode = 'ok'
    state.execCalls = []
  })
  afterEach(() => {
    state.execMode = 'ok'
    state.execCalls = []
    cleanup()
  })

  // T1：偏离拒单 → 确认框出现且文案为后端原话 → 确认后原体重发并带 confirm_deviation=true
  it('T1 偏离拒单弹确认框，确认后带 confirm_deviation 重发成功', async () => {
    state.execMode = 'deviation_once'
    render(<Positions />)
    await submitCloseOrder()

    // ① 确认框出现（header + 后端人话文案原文）
    const dlg = await screen.findByText('价格偏离确认', {}, { timeout: 5000 })
    expect(dlg, '§AUDITFIX926-D1：price_deviation 必须走二次确认而非一句话拒单').toBeInTheDocument()
    await screen.findByText(/委托价 15\.00 偏离现价 10\.00 超 ±15%/, {}, { timeout: 5000 })

    // ② 确认重发：第二次调用的体 = 原体 + confirm_deviation:true
    fireEvent.click(await screen.findByText('确认按此价提交'))
    await waitFor(() => expect(state.execCalls.length).toBe(2), { timeout: 5000 })
    const resend = state.execCalls[1]
    expect(resend.confirm_deviation, '§AUDITFIX926-D1：重发必须带 confirm_deviation=true').toBe(true)
    expect(resend.code, '§AUDITFIX926-D1：重发体必须是原体（同 code/价/量/方向）').toBe('600000.SH')
    expect(resend.price).toBe(10)
    expect(resend.side).toBe('卖出')
    // 首次调用绝不得带 confirm_deviation（用户没确认前 deviation 闩必须保持闭合）
    expect(state.execCalls[0].confirm_deviation, '§AUDITFIX926-D1：未确认时不得偷带 confirm_deviation').toBeFalsy()

    // ③ 成功 toast + 两个弹窗都收口（TDesign 关闭仅隐藏，断言可见性）
    await screen.findByText(/委托已提交/, {}, { timeout: 5000 })
    await waitFor(() => {
      expectDialogOpen('价格偏离确认', false)
      expectDialogOpen('确认下单', false)
    })
  })

  // T2：偏离拒单 → 点「返回改价」→ 不产生第二次调用，执行弹窗留在原位可改价
  it('T2 返回改价：不重发、执行弹窗保留', async () => {
    state.execMode = 'deviation_once'
    render(<Positions />)
    await submitCloseOrder()

    await screen.findByText('价格偏离确认', {}, { timeout: 5000 })
    fireEvent.click(await screen.findByText('返回改价', {}, { timeout: 5000 }))

    await waitFor(() => expectDialogOpen('价格偏离确认', false))
    // 执行弹窗未关：「确认下单」仍可见（改价后再提交的入口在）
    expectDialogOpen('确认下单', true)
    expect(state.execCalls.length, '§AUDITFIX926-D1：取消=不重发').toBe(1)
  })

  // T3：兼容负锁——无 code 的普通 400 只走原 toast，确认框绝不出现（分流只认机器码）
  it('T3 普通错误不弹确认框，维持原 toast 语义', async () => {
    state.execMode = 'plain_err'
    render(<Positions />)
    await submitCloseOrder()

    await screen.findByText(/下单失败: 资金闸余额不足/, {}, { timeout: 5000 })
    expect(screen.queryByText('价格偏离确认'), '§AUDITFIX926-D1：分流必须按 code，不得兜底全弹').not.toBeInTheDocument()
    expect(state.execCalls.length).toBe(1)
  })
})
