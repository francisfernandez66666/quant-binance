// ── §战法批-5 前端配置与展示行为锁 ──
// 文件职责：①BinanceStatusCard 消费 /api/binance/state 新增 "dispatch" 节——有摘要的市场
//   渲染台别徽标（真面/纸面/无台）+ 评估/信号/受理/拒单/退出计数，ok=false 呈「未派发过」，
//   节点整节缺席（旧后端）时页面零变化；纸面台不再误标「Noop 凭证缺失」。
//   ②BinanceConfigPanel 保存语义——dispatch 七键整档回传、events 按 GET 派生视图全量往返、
//   两把密钥留空不携带（后端「留空=保持」哨兵的前端对偶锁）。
// English: vitest locks for the dispatch node rendering and the settings form round-trip
// (full dispatch keys, events derived-view round-trip, blank secrets are never submitted).
import React from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import * as bapi from '../api/binance.js'
import BinanceStatusCard from '../components/BinanceStatusCard.jsx'
import BinanceConfigPanel from '../components/BinanceConfigPanel.jsx'

vi.mock('../api/binance.js', () => ({
  fetchBinanceState: vi.fn(),
  fetchBinanceConfig: vi.fn(),
  updateBinanceConfig: vi.fn(),
  binanceHalt: vi.fn(),
}))

beforeEach(() => { vi.clearAllMocks() })

const stateWithDispatch = {
  enabled: false, mode: 'auto', testnet: false, halted: false,
  controllers: {
    US: { executor: false, snapshot: {} },
    CRYPTO: { executor: true, snapshot: { last_probe_ok: true } },
  },
  reporters: {}, feeds: [],
  dispatch: {
    US: { ok: true, report: { at: '2026-09-23T22:00:00Z', universe: 4, signals: 2, placed: 1, rejected: 0, exit_placed: 1, desk: 'paper', notes: ['事件腿跳过'] } },
    CRYPTO: { ok: false },
  },
}

describe('BinanceStatusCard dispatch 节', () => {
  it('有摘要：台别徽标+计数；纸面台不标 Noop；未派发市场如实呈现', async () => {
    bapi.fetchBinanceState.mockResolvedValue(stateWithDispatch)
    render(<BinanceStatusCard />)
    await waitFor(() => expect(screen.getByText('纸面柜台（模拟成交，无密钥四方向）')).toBeTruthy())
    // 派发行整行断言：标签 span 的父级即 row() 的行容器
    const dspRow = screen.getByText('US 派发').parentElement
    expect(dspRow.textContent).toContain('评估 4')
    expect(dspRow.textContent).toContain('受理 1')
    expect(dspRow.textContent).toContain('退出 1')
    expect(dspRow.textContent).toContain('纸面')
    expect(screen.getByText('未派发过')).toBeTruthy()
    expect(screen.getByText(/备注 1/)).toBeTruthy()
    // CRYPTO 有真执行器：走探测态文案，不被派发节干扰
    expect(screen.getByText('探测正常 ●')).toBeTruthy()
  })

  it('旧后端无 dispatch 节：不渲染任何派发行（零变化）', async () => {
    const legacy = { ...stateWithDispatch }
    delete legacy.dispatch
    bapi.fetchBinanceState.mockResolvedValue(legacy)
    render(<BinanceStatusCard />)
    await waitFor(() => expect(screen.getByText('未熔断')).toBeTruthy())
    expect(screen.queryByText('US 派发')).toBeNull()
    expect(screen.queryByText(/评估 \d+ · 信号/)).toBeNull()
    // 无派发摘要时 executor=false 保持旧文案
    expect(screen.getByText('Noop（凭证缺失，记账不真下）')).toBeTruthy()
  })
})

const cfgBase = {
  enabled: false, data_plane: true, mode: 'manual', testnet: true, quote_asset: 'USDC',
  timeout_sec: 10, miss_heartbeat_sec: 120, cancel_stale_sec: 120, paper_separate: true, halted: false,
  api_key_masked: 'ab…cd', api_secret_masked: 'ef…gh', has_api_key: true,
  stock: { enabled: true, fixed_amount: 500, paper: false },
  spot: { enabled: true, fixed_amount: 100, paper: true, paper_cash: 20000 },
  risk_gate: { max_order_amount: 0 },
  dispatch: { enabled: false, bear_enabled: false, min_confidence: 0, take_profit_pct: 0, stop_loss_pct: 0, every_sec: 0, max_live_orders: 0 },
  events: {
    edgar_user_agent: 'QuantBot a@b.c', cryptopanic_token_masked: 'sk-…9', has_cryptopanic_token: true,
    cryptopanic_currencies: ['BTC'], llm_base_url: 'https://api.deepseek.com/v1', llm_model: 'deepseek-chat',
    llm_timeout_sec: 0, llm_api_key_masked: '', has_llm_api_key: false,
  },
}

// 按行标签定位该行的 role=switch 开关（表单行数多，索引定位脆）
function rowSwitch(labelText) {
  const el = screen.getByText(labelText)
  const sw = el.parentElement.querySelector('[role="switch"]')
  if (!sw) throw new Error('行内未找到开关: ' + labelText)
  return sw
}

describe('BinanceConfigPanel 派发/事件保存语义', () => {
  it('开派发总闸保存：dispatch 七键整档、events 全量往返、空密钥不携带', async () => {
    bapi.fetchBinanceConfig.mockResolvedValue(cfgBase)
    bapi.updateBinanceConfig.mockResolvedValue(cfgBase)
    render(<BinanceConfigPanel />)
    await waitFor(() => expect(screen.getByText('保存币安配置')).toBeTruthy())
    fireEvent.click(rowSwitch('派发总闸 enabled'))
    fireEvent.click(screen.getByText('保存币安配置'))
    await waitFor(() => expect(bapi.updateBinanceConfig).toHaveBeenCalled())
    const body = bapi.updateBinanceConfig.mock.calls[0][0]
    expect(body.dispatch.enabled).toBe(true)
    expect(body.dispatch.bear_enabled).toBe(false)
    expect(Object.keys(body.dispatch).sort()).toEqual(
      ['bear_enabled', 'enabled', 'every_sec', 'max_live_orders', 'min_confidence', 'stop_loss_pct', 'take_profit_pct'])
    // events 派生视图往返：非密钥键原样带回（整档替换语义防清零）
    expect(body.events.edgar_user_agent).toBe('QuantBot a@b.c')
    expect(body.events.cryptopanic_currencies).toEqual(['BTC'])
    expect(body.events.llm_base_url).toBe('https://api.deepseek.com/v1')
    // 留空=保持：两把密钥与 api_key/api_secret 一样绝不以空串形态下发
    expect('cryptopanic_token' in body.events).toBe(false)
    expect('llm_api_key' in body.events).toBe(false)
    expect('api_key' in body).toBe(false)
    // 纸面柜台位随档案整档流转（spot.paper 原样回传）
    expect(body.spot.paper).toBe(true)
    expect(body.spot.paper_cash).toBe(20000)
  })

  it('纸面柜台开关可编辑并进入提交体', async () => {
    bapi.fetchBinanceConfig.mockResolvedValue(cfgBase)
    bapi.updateBinanceConfig.mockResolvedValue(cfgBase)
    render(<BinanceConfigPanel />)
    await waitFor(() => expect(screen.getByText('保存币安配置')).toBeTruthy())
    // 两处「纸面柜台 paper」行（stock/spot）：第一处=stock，当前为关，点开
    const labels = screen.getAllByText('纸面柜台 paper')
    const stockRow = labels[0].parentElement
    const sw = stockRow.querySelector('[role="switch"]')
    expect(sw.getAttribute('aria-checked')).toBe('false')
    fireEvent.click(sw)
    fireEvent.click(screen.getByText('保存币安配置'))
    await waitFor(() => expect(bapi.updateBinanceConfig).toHaveBeenCalled())
    const body = bapi.updateBinanceConfig.mock.calls[0][0]
    expect(body.stock.paper).toBe(true)
    // spot 未动：基线 true/20000 原样往返
    expect(body.spot.paper).toBe(true)
    expect(body.spot.paper_cash).toBe(20000)
  })
})
