// ── §抄母仓-8（2026-09-24 可抄榜项 8）概览页「实盘链路」指示行为锁 ──
//
// 缺陷回音壁：Dashboard.jsx 的实盘状态轮询回调把 setQmtState 误写成 setQMTState（未声明符号）
// → ReferenceError 被同函数的空 catch 吞掉 → qmtState 恒 null → qmtLine 恒 '' →
// 「实盘链路」行**永不渲染**，且 15s 轮询每 tick 静默抛一次。
// 为什么这一条必须存在：类型检查不覆盖 .jsx、e2e 的整页巡检又只看"有没有未捕获 JS 异常"
// （ReferenceError 落在 catch 里，永远不上报）⇒ 这类拼写型缺陷在整个测试栈里零信号。
// lint 门（no-undef）防再犯，本用例防静默回退——两者配对，缺一即只剩一半保障。
// English: behavior lock for the §LINTGATE batch — with a healthy QMT status payload the overview
// page must actually render the「实盘链路」indicator.
import { test, expect } from '@playwright/test'

const ADMIN = { u: process.env.E2E_USER || 'admin', p: process.env.E2E_PASS || '' }
const API = process.env.E2E_API || 'http://localhost:18080'

// token 整进程缓存一次：/api/auth/login 计入 §T-3 租户频控，全套 spec 并发时逐用例登录会挤进 429。
let cachedTok = ''
async function adminToken(request) {
  if (cachedTok) return cachedTok
  const r = await request.post(API + '/api/auth/login', { data: { username: ADMIN.u, password: ADMIN.p } })
  const body = await r.json()
  expect(body.token, `admin 登录应拿到 token（status=${r.status()}）`).toBeTruthy()
  cachedTok = body.token
  return cachedTok
}

test.describe('§LINTGATE 概览页实盘链路渲染', () => {
  // L1：QMT 状态启用 + 探测成功 → 概览页必须出现「实盘链路：」摘要行。
  // UAT 栈在 bootstrap/nightly 里已装配 QMT（enabled:true），故本条断言的是真链路：
  // 后端 /api/qmt/state → 前端 fetchQMTState → qmtLine → 渲染，任何一环改名回归都会红。
  test('L1 概览页渲染「实盘链路」摘要行（未定义符号形态下整行静默消失）', async ({ request, page }) => {
    const tok = await adminToken(request)
    const st = await (await request.get(API + '/api/qmt/state', { headers: { Authorization: 'Bearer ' + tok } })).json()
    test.skip(!st.enabled, 'QMT 未启用（本栈未装配网关配置）——无链路可展示，跳过而非判红')
    await page.goto('/#/dashboard')
    const line = page.locator('text=实盘链路').first()
    await expect(line, '概览页应渲染「实盘链路」摘要（旧 ReferenceError 形态下此处永不出现）')
      .toBeVisible({ timeout: 15000 })
    // 摘要必须是"符号 + 模式 + 状态"三段拼接，而不是常量占位（占位会在链路真坏时仍显示绿）
    await expect(line, '实盘链路摘要应含状态符号与模式字样').toContainText(/[●○].*(自动|手动)/)
  })

  // L2：整页零未捕获异常（拼写型炸弹若不落 catch 会在此现形；配合 lint 门双向覆盖）。
  test('L2 概览页轮询 15s 无 PAGEERROR/401（静默抛错族的可观测面）', async ({ page }) => {
    const errs = []
    page.on('pageerror', (e) => errs.push('PAGEERROR: ' + String(e).slice(0, 160)))
    page.on('response', (r) => {
      if (r.url().includes('/api/') && r.status() === 401) errs.push('401: ' + r.url().slice(-40))
    })
    await page.goto('/#/dashboard')
    await page.waitForTimeout(3000) // 等首帧轮询落定
    expect(errs, '概览页无未捕获异常/401：' + errs.join('|')).toHaveLength(0)
  })
})
