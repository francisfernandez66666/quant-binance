// ── §UATFIX929（2026-09-29 全量审计施工批）e2e 接线锁 ──
//
// 文件职责：把今天这批修正在**全栈运行态**上的对外承诺钉成端到端用例——与
// scripts/verify_changes.sh 第 81 段的源码级/行为级锁互为两半：段 81 防"写法回退"，
// 本文件防"接线回退"（键还在、装配函数换页后却不发值这类缺陷，单测面看不到）。
//   U929-1（M3/审计 P2-C）/api/binance/state 顶层必发 disclaimer_signed_at（空串也发=键在），
//         且与 config 面同名键严格等值（同源 cfg 快照的两面承诺）；
//   U929-2（M3 前端腿）Quant 页币安状态卡「披露签署」行按 state 真值渲染——
//         state 空串显「未签署」、非空显签署时刻（旧缺陷正是 state 正常却恒显未签署，
//         因为卡片读的是 state 面上根本不存在的键；后端补键后本条即回归判据）；
//   U929-3（M4-lite ③④）模拟柜台保真：/state 顶层带 broker_mode 且与 /health 同源等值
//         （真网关 _do_state 亦发该键，Go GatewayState.BrokerMode 本就解析）；
//   U929-4（M4-lite ⑤）模拟柜台 /quotes 顶层带 feed_age_sec=0（真网关恒发，纯观察字段）。
// 手法沿用本仓 e2e 惯例：admin 共享会话（§UAT-SESSION）+ 直打 API；mock 直连地址/口令
// 一律走环境变量（§UAT-PORTS 铁律：硬编 18789 会打到别人那套栈的 mock 上假绿）。
// English: full-stack wiring locks for the 2026-09-29 audit batch — state-side disclaimer
// key parity with config, status-card rendering, and mock gateway broker_mode/feed_age_sec
// fidelity keys dialed against the live bootstrap stack.
import { test, expect } from '@playwright/test'
import { sharedToken } from './session.mjs'

const API = process.env.E2E_API || 'http://localhost:18080'
// §UAT-PORTS 同口径：mock 地址走环境变量；显式传了 E2E_MOCK_URL 即"跑在自举栈上"，
// mock 连不上必须判红而不是 skip（skip 会把"栈没起来"洗成绿）。
const MOCK_URL = process.env.E2E_MOCK_URL || 'http://127.0.0.1:18789'
const MOCK_TOKEN = process.env.QMT_TOKEN || 'uat-secret'
const MOCK_MANDATORY = !!process.env.E2E_MOCK_URL

// mock 不可达的两种口径（与 uat_full.spec.mjs 同款收在一处）：外部部署 skip、自举栈判红。
function mockUnavailableOrFail(lastErr) {
  const why = lastErr ? ': ' + lastErr : ''
  if (MOCK_MANDATORY) {
    throw new Error(`自举栈的 qmt-mock 不可达（E2E_MOCK_URL=${MOCK_URL}）${why}——此场景必须判红`)
  }
  test.skip(true, 'qmt-mock 未就绪（独立部署场景跳过，不算失败）' + why)
}

// admin 共享会话 token（§UAT-SESSION：全轮每账号只占一条会话，防 8 条上限 FIFO 踢人）
async function tok(request) {
  return sharedToken(request, 'admin')
}

test.describe('§UATFIX929 施工批接线锁（state 披露键 + mock 保真观察键）', () => {
  test.describe.configure({ mode: 'serial' }) // 共用会话与页面，串行防日志交错

  // U929-1 后端两面等值：state 顶层 disclaimer_signed_at 必发（字符串，空串=未签署也要有键），
  // 且与 GET /api/config/binance 同名键严格等值——两面读的是同一 cfg 快照，任何一侧改装配
  // 而另一侧漏改（正是本批修掉前的形态），本条即红。
  test('U929-1 /api/binance/state 顶层必发 disclaimer_signed_at 且与 config 面等值', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request)) }
    const stResp = await request.get(API + '/api/binance/state', { headers: H })
    expect(stResp.status(), 'admin 读 binance state 应 200').toBe(200)
    const st = await stResp.json()
    expect(typeof st.disclaimer_signed_at, 'state 顶层必须有 disclaimer_signed_at 字符串键（空串=未签署也须在场）').toBe('string')
    const cfg = await (await request.get(API + '/api/config/binance', { headers: H })).json()
    expect(typeof cfg.disclaimer_signed_at, 'config 面同键须在场（历史承诺）').toBe('string')
    expect(st.disclaimer_signed_at, 'state 与 config 两面披露签署值必须逐字节一致（同源 cfg）').toBe(cfg.disclaimer_signed_at)
  })

  // U929-2 前端渲染腿：Quant 页币安状态卡「披露签署」行按 state 真值显示。
  // 判据取运行态实值而非写死文案：UAT 栈出厂未签署（空串）→ 应显「未签署」；
  // 若将来栈内已签署，行文本应含 state 返回的时刻串。两种形态都覆盖＝不是"恰好绿"。
  test('U929-2 Quant 状态卡「披露签署」行渲染与 state 真值一致', async ({ page, request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request)) }
    const st = await (await request.get(API + '/api/binance/state', { headers: H })).json()
    const expected = st.disclaimer_signed_at ? st.disclaimer_signed_at : '未签署'
    await page.goto('/#/quant')
    const row = page.locator('text=披露签署').first()
    await expect(row, '币安状态卡应含「披露签署」行').toBeVisible({ timeout: 15000 })
    // 值渲染判据只看「state 真值对应的文案在同屏出现」——行容器 DOM 结构由组件自持，
    // 精确的读源优先级（state 优先、非回落 config）已由 vitest uatfix929_card_disclaimer 锁死，
    // 本条防的是整行消失/文案错位这类运行态接线回退。
    await expect(page.locator(`text=${expected}`).first(), `页面应渲染披露签署值「${expected}」`).toBeVisible({ timeout: 10000 })
    await page.screenshot({ path: 'test-results/uat-pixels/uatfix929-card-disclaimer.png', fullPage: false })
  })

  // U929-3 mock 保真 broker_mode：/state 顶层带该键且与 /health 同源等值（activeBroker 一个真值、
  // 两个观察面，切换 broker 后两面仍须一致——本用例验等值，不做 /admin/broker 写操作防污染其它用例）。
  test('U929-3 mock /state 带 broker_mode 且与 /health 同源', async ({ page }) => {
    let resp = null
    let lastErr = ''
    for (let i = 0; i < 3 && !resp; i++) { // mock 刚重启/瞬时繁忙重试三轮（L1-2 同款手法）
      await page.waitForTimeout(500)
      resp = await page.request.get(`${MOCK_URL}/state`, {
        headers: { Authorization: `Bearer ${MOCK_TOKEN}` },
      }).catch((e) => { lastErr = String(e && e.message || e); return null })
    }
    if (!resp) mockUnavailableOrFail(lastErr)
    expect(resp.status(), 'mock /state 带 token 应 200').toBe(200)
    const body = await resp.json()
    expect(typeof body.broker_mode, '§UATFIX929-E④：/state 顶层必须回显 broker_mode（对齐真网关 _do_state）').toBe('string')
    expect(body.broker_mode.length, 'broker_mode 非空串（空=activeBroker 装配漏了默认 xt）').toBeGreaterThan(0)
    const h = await (await page.request.get(`${MOCK_URL}/health`, {
      headers: { Authorization: `Bearer ${MOCK_TOKEN}` },
    })).json()
    expect(h.broker_mode, 'broker_mode 两面同源等值（一个 activeBroker 两个观察面）').toBe(body.broker_mode)
  })

  // U929-4 mock 保真 feed_age_sec：/quotes 顶层恒发 0（mock tick 即时合成、无真实 feed 通道；
  // 真网关恒发该键——缺键即保真差复活，Go 侧虽不解析该观察字段，UAT/排障面吃它就撞 404 式空值）。
  test('U929-4 mock /quotes 顶层带 feed_age_sec=0', async ({ page }) => {
    const resp = await page.request.get(`${MOCK_URL}/quotes?codes=600000.SH`, {
      headers: { Authorization: `Bearer ${MOCK_TOKEN}` },
    }).catch((e) => { return null })
    if (!resp) mockUnavailableOrFail('')
    expect(resp.status(), 'mock /quotes 带 codes 应 200').toBe(200)
    const body = await resp.json()
    expect(body, '§UATFIX929-E⑤：/quotes 顶层必须含 feed_age_sec 键').toHaveProperty('feed_age_sec')
    expect(body.feed_age_sec, 'mock tick 即时合成，feed_age_sec 恒 0（零秒前刚更新）').toBe(0)
  })
})
