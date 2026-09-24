// ── §QMT-FROZEN 链路三态判定单测 gateway_link_state.test.js ──
// 文件职责：锁 gatewayLinkState.js 的裁决语义——frozen（A股链路冻结，广州机不再是部署目标）
// ≠ off（链路在但没启用）≠ down（启用了却失联），以及"没取到 cn_master 不判冻结"的兜底。
// 为什么必须有：三态文案在 Quant/Positions/Dashboard 三处分流展示，判错一态界面就把
// "不会再有网关"说成"等开盘就好"或"网络抖了一下"，运维会去排查一条不存在的链路。
// English: unit locks for the three-state gateway verdict introduced by the 2026-09-24
// Guangzhou-freeze ruling; each state must map to a distinct, honest UI surface.
import { describe, it, expect, afterEach } from 'vitest'
import {
  gatewayVerdict, setCnMaster,
  LINK_FROZEN, LINK_OFF, LINK_DOWN, LINK_LIVE, LINK_UNKNOWN,
  FROZEN_HINT, FROZEN_HINT_SHORT, NOT_IN_EFFECT,
} from '../gatewayLinkState'

const HEALTHY = {
  enabled: true, mode: 'auto', tripped: false,
  last_probe_at: '2026-09-25 10:00:00', last_probe_ok: true,
}

describe('gatewayVerdict 三态裁决', () => {
  afterEach(() => setCnMaster(null)) // 模块级缓存：跨用例必须复位，避免冻结态泄漏

  it('cn_master=false → 恒判 frozen，哪怕配置里 enabled 仍是广州时代残留的 true', () => {
    setCnMaster(false)
    expect(gatewayVerdict({ ...HEALTHY, gateway_url: 'http://gw-guangzhou.invalid:8789' })).toBe(LINK_FROZEN)
    // 冻结时 state 从未拉到（/api/qmt/state 503）也必须出冻结态，不能回落成"未知/加载中"
    expect(gatewayVerdict(null)).toBe(LINK_FROZEN)
  })

  it('cn_master=true + enabled=false → off（链路未启用，不是冻结）', () => {
    setCnMaster(true)
    expect(gatewayVerdict({ enabled: false })).toBe(LINK_OFF)
  })

  it('cn_master=true + enabled=true + 探测确证失败 → down（启用了但失联）', () => {
    setCnMaster(true)
    expect(gatewayVerdict({ ...HEALTHY, last_probe_ok: false })).toBe(LINK_DOWN)
  })

  it('enabled=true 但从未探测（休市）不算 down——沿用改动前 probeNever 口径防每天误报', () => {
    setCnMaster(true)
    expect(gatewayVerdict({ enabled: true, last_probe_at: '0001-01-01 00:00:00' })).toBe(LINK_LIVE)
  })

  it('cn_master 未知（null：旧服务端缺字段/首轮未到达）→ 不判 frozen，保持改动前口径', () => {
    setCnMaster(undefined) // 非显式布尔一律归 null
    expect(gatewayVerdict(HEALTHY)).toBe(LINK_LIVE)
    expect(gatewayVerdict(null)).toBe(LINK_UNKNOWN)
  })

  it('冻结文案三处展示位同源：横幅含"冻结维护/广州/不再上报"，短文案与未生效占位齐备', () => {
    expect(FROZEN_HINT).toMatch(/冻结维护/)
    expect(FROZEN_HINT).toMatch(/广州执行机不再是部署目标/)
    expect(FROZEN_HINT).toMatch(/不再上报/)
    expect(FROZEN_HINT_SHORT).toMatch(/冻结维护/)
    expect(NOT_IN_EFFECT).toBe('未生效')
  })
})
