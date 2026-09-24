// ── §QMT-FROZEN（2026-09-24 本地接线批第 2 项）冻结链路三态的浏览器端行为锁 ──
//
// 文件职责：在真实浏览器 + 真实前端 bundle + 真实路由/轮询的形态下，锁住「A股总开关关掉之后，
//   三处实盘链路展示位（量化页链路状态卡 / 持仓页实盘头部 / 概览页系统状态行）必须说"未生效"，
//   而不是继续演"链路健康、只是今天没开市"」。
//
// 为什么组件级 vitest 不够、这一条必须存在：vitest 里 api 是整体 mock 的，它证明的是
//   「给定 state，组件渲染对」；证明不了「App.jsx 真的把 /api/status 的 cn_master 转交给了裁决模块」
//   ——那条转发腿（`setGatewayLinkCnMaster(st.cn_master)`）一旦断（改签名、漏调用、被 lint 自动修复
//   删成未使用导入），三处展示位在 vitest 里照样绿（用例自己 setCnMaster），线上却退回假活。
//   本用例只 stub 两个后端载荷，其余（打包产物、路由、fetch 层、轮询、TDesign 渲染）全是真的。
//
// 判定口径的单一来源在 web/src/gatewayLinkState.js：frozen > off > down > live > unknown。
// English: browser-level lock for the frozen-link verdict — when the server snapshot says the CN
// master switch is off, every QMT surface must render 未生效 and none of the three "looks normal"
// excuses may appear.
import { test, expect } from '@playwright/test'
import { sharedToken } from './session.mjs'

const API = process.env.E2E_API || 'http://localhost:18080'

// 冻结横幅（长/短两版）与各占位文案，与 gatewayLinkState.js / 三处展示位同源，改文案必须同步这里。
const HINT_LONG = 'A股实盘链路已冻结维护'
const HINT_SHORT = 'A股链路已冻结维护（广州执行机不再是部署目标，网关不再上报）'
const PROBE_FROZEN = '未生效（链路已冻结，不再下行探测）'
const GW_FROZEN = '未生效（广州执行机已下线）'
const PATH_FROZEN = '当前：未生效（链路冻结）'
const POS_CHIP = '未生效（链路冻结）'
// 三句"错误借口"：冻结态下它们各自暗示一件不真实的事，出现即判红。
const EXCUSE_PROBE = '休市未探测'
const EXCUSE_REPORT = '暂无回报（非交易时段属正常）'
const EXCUSE_PATH = '当前：miniQMT兼容'

// token 取自全栈共享会话（§UAT-SESSION）：本文件绝不自建登录——后端每账号 8 条会话 FIFO
// 淘汰最旧，多余登录会踢掉 storageState 那条，把浏览器用例红成一片"看起来像权限坏了"。
async function adminToken(request) {
  return sharedToken(request, 'admin')
}

// 把页面切到冻结态：取真 /api/status 载荷（保持 build_commit 等其余字段为真值，避免顺手造出
// 版本漂移横幅之类的干扰态），只把 cn_master 打成 false；同时让 /api/qmt/* 全部回 503——
// 这正是广州执行机下线后网关的真实形态（进程不在了，凡是要网关应答的端点一律无响应）。
// 为什么整族而不是只 stub state：当日委托卡读 /api/qmt/orders，UAT 栈上的假柜台还活着，
// 只 stub state 会让委托卡拿到一份真·空数组，渲染成「今日暂无实盘委托」——那是在骗用例，
// 冻结场景里根本不会有回报（实测 Z2 就是被这份"活着的 mock"喂成假红的）。
// 返回 { status, qmt }：两类路由各被命中的次数，用它反证"页面真的读了这份冻结快照"。
async function freezeLink(page, request) {
  const tok = await adminToken(request)
  const real = await (await request.get(API + '/api/status', { headers: { Authorization: 'Bearer ' + tok } })).json()
  expect(typeof real.cn_master, '/api/status 应回传 cn_master 布尔（否则本用例打的桩没有对照物）').toBe('boolean')
  const frozenBody = JSON.stringify({ ...real, cn_master: false })
  const hits = { status: 0, qmt: 0 }
  await page.route('**/api/status', (route) => {
    hits.status += 1
    route.fulfill({ status: 200, contentType: 'application/json', body: frozenBody })
  })
  await page.route('**/api/qmt/**', (route) => {
    hits.qmt += 1
    route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"qmt gateway unreachable"}' })
  })
  return hits
}

test.describe('§QMT-FROZEN 冻结链路三态展示（浏览器端）', () => {
  test('Z1 量化页链路状态卡：出冻结横幅、各项显示未生效、三句错误借口绝迹', async ({ page, request }) => {
    const hits = await freezeLink(page, request)
    await page.goto('/#/quant')
    await expect(page.locator(`text=${HINT_LONG}`).first(), '量化页必须出现冻结横幅（先说清"下面全是未生效"）')
      .toBeVisible({ timeout: 15000 })
    // 桩必须真被读到：没命中一次就说明转发腿或路由前缀变了，此时下面的绿全是假绿。
    expect(hits.status, '页面确实读过被打成 cn_master=false 的 /api/status').toBeGreaterThan(0)
    await expect(page.locator(`text=${PROBE_FROZEN}`).first(), '下行探测必须说"链路已冻结，不再下行探测"').toBeVisible()
    await expect(page.locator(`text=${GW_FROZEN}`).first(), '网关地址行必须显示"未生效（广州执行机已下线）"').toBeVisible()
    await expect(page.locator(`text=${PATH_FROZEN}`).first(), '执行路径必须显示"当前：未生效（链路冻结）"').toBeVisible()
    // 三句借口：各自暗示"等开盘就好/网络正常只是没回报/当前通道是 miniQMT"，冻结态下都不成立。
    await expect(page.locator(`text=${EXCUSE_PROBE}`), `冻结态不得再出现「${EXCUSE_PROBE}」`).toHaveCount(0)
    await expect(page.locator(`text=${EXCUSE_REPORT}`), `冻结态不得再出现「${EXCUSE_REPORT}」`).toHaveCount(0)
    await expect(page.locator(`text=${EXCUSE_PATH}`), `冻结态不得再出现「${EXCUSE_PATH}」`).toHaveCount(0)
  })

  test('Z2 量化页当日委托卡：显示"链路已冻结、网关不再上报委托"，不弹重试横幅也不空转', async ({ page, request }) => {
    await freezeLink(page, request)
    await page.goto('/#/quant')
    await expect(page.locator(`text=${HINT_LONG}`).first()).toBeVisible({ timeout: 15000 })
    await expect(page.locator('text=无数据（A股链路已冻结').first(),
      '当日委托卡必须把"取不到"归因到链路冻结（旧形态把它说成重试可解的网络问题）').toBeVisible()
  })

  test('Z3 持仓页实盘头部：一排"已启用/正常/网关 URL"必须换成未生效口径', async ({ page, request }) => {
    const hits = await freezeLink(page, request)
    await page.goto('/#/positions')
    // 实盘头部在「实盘持仓」这一页签里（TDesign Tabs 惰性挂载：不点开就没有这段 DOM）。
    // 旧写法直接 goto 后断言，测到的其实是默认页签——冻结横幅永远找不到。
    await page.locator('.t-tabs__nav-item', { hasText: '实盘持仓' }).first().click()
    await expect(page.locator(`text=${HINT_LONG}`).first(), '持仓页必须出现冻结横幅').toBeVisible({ timeout: 15000 })
    expect(hits.status, '持仓页确实读过冻结快照').toBeGreaterThan(0)
    await expect(page.locator(`text=${POS_CHIP}`).first(), '实盘头部必须挂"未生效（链路冻结）"标记').toBeVisible()
    // 负锁：旧形态把配置残影里的广州网关 URL 当实时健康展示在头部（`网关 http://…`），冻结态绝迹。
    await expect(page.locator('text=/网关 http/'), '冻结态持仓页不得再打印网关 URL（那是勾人去排查已下线机器）').toHaveCount(0)
  })

  test('Z4 概览页系统状态行：链路 503 时该行仍须渲染并写短横幅（不得整行静默消失）', async ({ page, request }) => {
    const hits = await freezeLink(page, request)
    await page.goto('/#/dashboard')
    await expect(page.locator('text=实盘链路').first(),
      '概览页「实盘链路」行必须渲染（§LINTGATE 事故形态就是一 503 整行静默消失）').toBeVisible({ timeout: 15000 })
    expect(hits.status, '概览页确实读过冻结快照').toBeGreaterThan(0)
    await expect(page.locator(`text=${HINT_SHORT}`).first(), '该行必须直接给出冻结短说明').toBeVisible()
  })

  test('Z5 冻结态下执行通道切换按钮保留但禁用，点下去既不弹确认也不发后端请求', async ({ page, request }) => {
    await freezeLink(page, request)
    const posts = []
    page.on('request', (r) => {
      if (r.method() === 'POST' && /\/api\/(qmt|config)\/.*(broker|switch)/.test(r.url())) posts.push(r.url())
    })
    await page.goto('/#/quant')
    const btn = page.locator('.t-button', { hasText: '切到 QMT桥' }).first()
    await expect(btn, '切换按钮必须保留（实现而非删除）').toBeVisible({ timeout: 15000 })
    // TDesign 的禁用按钮渲染成 <div class="t-button t-is-disabled">，所以按 role=button 查会零命中——
    // 只能按类名判禁用（这个 DOM 事实在 vitest 用例里也踩过一次）。
    await expect(btn, '冻结态下切换按钮必须禁用').toHaveClass(/t-is-disabled/)
    await expect(btn, '禁用原因必须写在 tooltip 里').toHaveAttribute('title', new RegExp(HINT_SHORT.slice(0, 12)))
    await btn.click({ force: true })
    await page.waitForTimeout(600)
    await expect(page.locator('text=切换执行通道'), '不得弹出切换确认框').toHaveCount(0)
    expect(posts, '不得向切换端点发请求：' + posts.join('|')).toHaveLength(0)
  })
})
