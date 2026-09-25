// ── §AUDITFIX925（2026-09-25 全量审计「马上能改」批）e2e 接线锁 ──
//
// 本文件锁的是今天这批修正在**全栈运行态**上的对外承诺，与 scripts/verify_changes.sh
// 第 78 段的源码级锁互为两半：静态锁防"写法回退"，本文件防"接线回退"——
// 函数还在、鉴权档位/响应形状/广播语义却悄悄变了的那类缺陷，grep 是看不见的。
//   D9  /api/health 双形态：默认体字节等价（就绪探针动不得）+ ?deep=1 与 /api/engine_health
//       同源对拍 + 未鉴权 401（§H8 教训：深探测必须带 token 判 JSON，禁裸 curl -f 把 401 当误熔）；
//   D4  币安熔断接警全链路：POST /api/binance/halt → 配置持久化 → SSE 广播 → 前端全局 Toast
//       （旧版前端对 binance_halt 事件零消费，熔断置位只有日志看得见——本条即其判据）；
//   D2  币安 fetch 层契约（cancel=POST+body / exchange_info=GET）的静态等值已由
//       web/src/__tests__/binance_contract.test.js 在 verify 78 段行为锁①覆盖，本文件不重复起 UI 用例。
// 手法沿用本仓 e2e 惯例：直打后端 API + 「先记基线、finally 还原」（服务端共享状态，
// 中途失败会毒化后续用例，同 §UAT-D8 halt 用例的 teardown 纪律）。
// English: full-stack behavior locks for the 2026-09-25 audit batch — health default body
// byte-identity + ?deep=1 same-source parity, and the binance kill-switch SSE toast chain end to end.
import { test, expect } from '@playwright/test'
import { sharedToken } from './session.mjs'

const ADMIN = { u: process.env.E2E_USER || 'admin', p: process.env.E2E_PASS || '' }
const API = process.env.E2E_API || 'http://localhost:18080'

// token 走全栈共享会话（§UAT-SESSION）：不再整进程缓存一份，避免挤爆每账号 8 条会话上限。
async function tok(request) {
  return sharedToken(request, 'admin')
}

test.describe('§AUDITFIX925-D9 /api/health 双形态（就绪默认体 + deep=1 深探针）', () => {
  // 三条用例共用鉴权档位与同源比对，串行执行（本组无写操作，仅防并发日志交错）。
  test.describe.configure({ mode: 'serial' })

  // H1 默认体字节等价：uat_bootstrap/monitor_test 把本口当就绪探针用，响应必须仍是
  // 一字不差的 {"status":"ok"}（多一个字段、少一个换行都算破坏契约——golden 级判法）。
  test('H1 默认响应字节等价 {"status":"ok"}，无 deep 字段泄漏', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request)) }
    const resp = await request.get(API + '/api/health', { headers: H })
    expect(resp.status(), '就绪探针 200').toBe(200)
    const body = await resp.text()
    expect(body, '默认体必须字节等价（writeJSON 带尾换行）').toBe('{"status":"ok"}\n')
  })

  // H2 deep=1 形状锁：status∈{ok,degraded} 且与 deep_ok 等值、subsystems 九腿齐备全布尔。
  // status 不钉死 ok——degraded 是合法运行态（CN 冻结期部分腿本就 false），
  // 但 deep_ok 必须只等于 aggregator 腿（钉别的腿＝D9 修法被改宽的信号）。
  test('H2 ?deep=1 返回 status/deep_ok/subsystems 三件套，九腿布尔齐备', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request)) }
    const resp = await request.get(API + '/api/health?deep=1', { headers: H })
    expect(resp.status(), 'deep 探测 200（不是 404/503）').toBe(200)
    const j = await resp.json()
    expect(['ok', 'degraded'], 'status 二值口径').toContain(j.status)
    expect(typeof j.deep_ok, 'deep_ok 必为布尔').toBe('boolean')
    expect(j.status === 'ok', 'status 与 deep_ok 严格等值').toBe(j.deep_ok)
    const legs = ['news_agent', 'strategy_engine', 'sector_agent', 'combat_agent', 'llm', 'ths', 'fetcher', 'aggregator', 'paper']
    for (const k of legs) {
      expect(typeof j.subsystems?.[k], `子系统腿 ${k} 必须存在且为布尔`).toBe('boolean')
    }
    expect(Object.keys(j.subsystems).length, '子系统腿恰好 9 条（多腿＝口径漂移，少腿＝装配缺失）').toBe(9)
  })

  // H3 同源对拍：deep 的 subsystems 必须与 /api/engine_health 一字不差（同一装配函数），
  // 且 deep_ok 只钉 aggregator 腿。两口径哪天分叉（有人在 deep 分支里另起炉灶），本条即红。
  test('H3 deep.subsystems 与 /api/engine_health 同源等值，deep_ok===aggregator', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request)) }
    const deep = await (await request.get(API + '/api/health?deep=1', { headers: H })).json()
    const eng = await (await request.get(API + '/api/engine_health', { headers: H })).json()
    const sortKeys = (o) => JSON.stringify(Object.fromEntries(Object.entries(o).sort((a, b) => a[0] < b[0] ? -1 : 1)))
    expect(sortKeys(deep.subsystems), '两口径必须逐键等值（同源装配）').toBe(sortKeys(eng))
    expect(deep.deep_ok, 'deep_ok 只钉 aggregator 单腿').toBe(eng.aggregator)
  })

  // H4 鉴权档位等值：deep 分支挂在同一 authMiddleware 上，无 token 必须 401——
  // 若哪天 deep 被挪到免鉴权区（泄露内部子系统态），本条判红。401 是"鉴权在、没带票"，
  // 与 §H8 血案里"把 401 当存活信号熔掉"是两回事：这里我们判的就是 JSON 语义本身。
  test('H4 未带 token 的 deep 探测回 401（鉴权档位与默认体同口）', async ({ request }) => {
    const resp = await request.get(API + '/api/health?deep=1')
    expect(resp.status(), 'deep 分支不得绕过 authMiddleware').toBe(401)
  })
})

test.describe('§AUDITFIX925-D4 币安熔断接警全链路（halt API→SSE→前端 Toast）', () => {
  // 共享服务端状态（per-user halted），两条用例严格串行，防彼此的基线互踩。
  test.describe.configure({ mode: 'serial' })

  // H5 HTTP 契约往返：置位 200 {ok,halted,cancelled}→config 持久化回读→非法 body 400→
  // finally 复位基线。cancelled 只断"必为数字"——UAT 栈币安控制器未装配时恒 0 是合法值。
  test('H5 POST /api/binance/halt 置位→持久化回读→400 闸口→finally 还原', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request)) }
    const base = (await (await request.get(API + '/api/config/binance', { headers: H })).json()).halted === true
    try {
      const on = await request.post(API + '/api/binance/halt', { headers: H, data: { halted: true } })
      expect(on.status(), 'kill-switch 置位 200').toBe(200)
      const j = await on.json()
      expect(j.halted, '响应回显 halted=true').toBe(true)
      expect(typeof j.cancelled, 'cancelled 必为数字（撤销笔数如实报）').toBe('number')
      const back = await (await request.get(API + '/api/config/binance', { headers: H })).json()
      expect(back.halted, 'halted=true 已持久化（跨重启保留）').toBe(true)
      const bad = await request.post(API + '/api/binance/halt', { headers: H, data: {} })
      expect(bad.status(), '缺 halted 字段的请求必须 400（不许静默当 false）').toBe(400)
    } finally {
      await request.post(API + '/api/binance/halt', { headers: H, data: { halted: base } })
      await expect(async () => {
        const cfg = await (await request.get(API + '/api/config/binance', { headers: H })).json()
        expect(cfg.halted, 'finally 兜底后回到基线').toBe(base)
      }).toPass({ timeout: 8000 })
    }
  })

  // H6 UI 接警（本批核心缺口的端到端判据）：浏览器登录后 App 全局挂着 SSE；
  // 置位/解除各触发一次广播，前端必须弹出对应 Toast（旧实现零消费，只有后端日志看得见）。
  // Toast 文案锁 sseOpsAlert 映射的 body 关键片段，不锁整句时间戳（时分秒随运行时刻漂移）。
  test('H6 置位/解除经 SSE 变全局 Toast（熔断红警 + 恢复绿提示）', async ({ page }) => {
    await page.goto('/#/dashboard')
    const hdr = { Authorization: await page.evaluate(() => localStorage.getItem('liangzai_token')) }
    // 时序坑（首跑实锤）：page.reload()/goto 只保证 DOM 就绪，页面内 SSE 还要再走
    // "签票→EventSource 建链"几百毫秒；这期间后端广播无人接收，且首连不带 last_event_id
    // 补发环——事件错过就是永久错过。故不能"发一次再等 15s"，改成 toPass 重试相：
    // 每轮 置位→短等 Toast，链路一旦建立必有一发落住（置位幂等、cancelled 恒 0，重发无害）。
    await page.request.post('/api/binance/halt', { headers: hdr, data: { halted: false } })
    try {
      await expect(async () => {
        await page.request.post('/api/binance/halt', { headers: hdr, data: { halted: true } })
        await expect(
          page.locator('.t-message', { hasText: '美股/加密货币实盘已紧急停止' }).first(),
          '置位广播必须落全局 Toast（接警腿在位）'
        ).toBeVisible({ timeout: 2500 })
      }).toPass({ timeout: 30000 })
      // 解除相同步纪律（此时链路已建立，单发即可，但保留 toPass 防 Toast 存活期竞态）。
      await expect(async () => {
        await page.request.post('/api/binance/halt', { headers: hdr, data: { halted: false } })
        await expect(
          page.locator('.t-message', { hasText: '币安熔断已解除' }).first(),
          '解除广播同样要落 Toast（恢复态不许静默）'
        ).toBeVisible({ timeout: 2500 })
      }).toPass({ timeout: 20000 })
    } finally {
      await page.request.post('/api/binance/halt', { headers: hdr, data: { halted: false } })
    }
  })
})
