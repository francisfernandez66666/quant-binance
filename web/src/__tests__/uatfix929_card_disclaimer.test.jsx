// ── §UATFIX929-C 状态卡披露签署行行为锁 ──
// 文件职责：钉死「披露签署」行的读源契约——state 载荷顶层 disclaimer_signed_at 有值即渲染
// 时间戳、空串/缺键即渲染「未签署」；且 state 成功时绝不回落 config（缺陷 C 的旧形态是
// state 从不发该键 → state 越正常越恒显「未签署」，只有 state 请求失败回落 config 摘要时
// 才显示正确——「坏得越对」）。后端同源补发由 Go 锁（uatfix929_state_disclaimer_test.go）
// 与本文件共同构成双腿：契约面（后端必发键）+ 消费面（前端按键如实渲染）。
// English: locks the status card's disclaimer row against the §UATFIX929-C regression class
// (state payload carries the key; card renders truthfully from it; no silent "未签署" default).
import React from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import * as bapi from '../api/binance.js'
import BinanceStatusCard from '../components/BinanceStatusCard.jsx'

vi.mock('../api/binance.js', () => ({
  fetchBinanceState: vi.fn(),
  fetchBinanceConfig: vi.fn(),
  binanceHalt: vi.fn(),
}))

beforeEach(() => { vi.clearAllMocks() })

// baseState 与后端实发骨架同形（controllers/reporters/feeds），两例只差 disclaimer_signed_at。
const baseState = (signed) => ({
  enabled: true, mode: 'live', testnet: false, halted: false,
  disclaimer_signed_at: signed,
  controllers: { US: { executor: true, snapshot: {} }, CRYPTO: { executor: true, snapshot: {} } },
  reporters: {}, feeds: [],
})

describe('BinanceStatusCard 披露签署行（§UATFIX929-C）', () => {
  it('state 发时间戳：行显示该时间戳，绝不回落「未签署」', async () => {
    bapi.fetchBinanceState.mockResolvedValue(baseState('2026-09-20T12:00:00Z'))
    render(<BinanceStatusCard />)
    await waitFor(() => expect(screen.getByText(/披露签署/)).toBeTruthy())
    const row = screen.getByText(/披露签署/).parentElement
    expect(row.textContent).toContain('2026-09-20T12:00:00Z')
    expect(row.textContent).not.toContain('未签署')
    // state 成功路径不得消费 config 面（回落只属于 404/网络失败分支）
    expect(bapi.fetchBinanceConfig).not.toHaveBeenCalled()
  })

  it('state 发空串（未签署态）：行如实显示「未签署」', async () => {
    bapi.fetchBinanceState.mockResolvedValue(baseState(''))
    render(<BinanceStatusCard />)
    await waitFor(() => expect(screen.getByText(/披露签署/)).toBeTruthy())
    const row = screen.getByText(/披露签署/).parentElement
    expect(row.textContent).toContain('未签署')
  })
})
