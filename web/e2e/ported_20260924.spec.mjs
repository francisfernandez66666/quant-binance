// ── §抄母仓批（2026-09-24 可抄榜项 3/5/6/7）HTTP 契约行为锁 ──
//
// 本文件锁的是"今天搬过来的四项缺陷在 HTTP 契约上的对外承诺"，与 scripts/verify_changes.sh
// 第 67/69/70/74 段的源码级锁互为两半：静态锁防"写法回退"，本文件防"接线回退"——
// 端点仍在、鉴权档位/合并语义/口径位却悄悄变了的那类缺陷，grep 是看不见的。
//   项 6 战法参数并发保存：稀疏合并（改一个字段不许抹掉别的字段）+ 版本冲突 409 + 服务端盖戳；
//   项 3 通知测试端点：抬到管理员档（成员 403）+ 全进程 60s 频控（429 必须带 Retry-After）；
//   项 5 指标告警出口：/api/metrics/alerts 规则清单契约（含 §DEADGAUGE 三条复活规则齐备）；
//   项 7 复权口径：情绪×战法矩阵必须回显 adj_basis（口径位一旦变回可选参数=混桶假统计复活）。
// 手法沿用本仓 e2e 惯例：直打后端 API + 「先记基线、finally 还原」，不污染共享配置与后续用例。
// English: HTTP-contract locks for the four ported defects (strategy-config merge/409, notify-test
// gate+throttle, alert rules surface, adjustment-basis on the emotion matrix).
import { test, expect } from '@playwright/test'
import { sharedToken } from './session.mjs'

const ADMIN = { u: process.env.E2E_USER || 'admin', p: process.env.E2E_PASS || '' }
const MEMBER = { u: process.env.E2E_USER2 || 'tester', p: process.env.E2E_PASS2 || ADMIN.p }
const API = process.env.E2E_API || 'http://localhost:18080'

// token 走全栈共享会话（§UAT-SESSION）：admin 与成员各复用一条会话，不再"整进程缓存一份"。
// 为什么进程级缓存还不够：后端每账号只保留 8 条会话（FIFO 淘汰最旧），四个 worker 各缓存一份
// 就是 4 条，加上 uat_full 逐用例登录与 auth.setup 的浏览器会话即越限——被踢掉的正是浏览器那条。
async function tok(request, who) {
  return sharedToken(request, who === ADMIN ? 'admin' : 'member')
}

test.describe('§抄母仓-6 战法参数并发保存（稀疏合并 + 409）', () => {
  // 三条用例共用同一份全局战法配置，必须串行（并发下彼此的基线/还原会互踩）。
  test.describe.configure({ mode: 'serial' })
  // 还原载荷必须把版本位清空（updated_at:''）：服务端 MergeStrategyConfig 会 delete 掉 body 里的
  // updated_at 不参与 merge，但它同时是**乐观锁比对输入**——照抄基线原样回传会因自己的上一次写入
  // 已推进版本而吃 409，还原静默失败、把改动留给后续用例与共享配置（本文件三处 finally 同此口径）。
  const restore = (base) => ({ ...base, updated_at: '' })

  // M1 稀疏合并：只提交 dragon.f1_seal_weight 一个嵌套字段，回读时
  // ① 该字段生效；② dragon 的兄弟字段（f2/f3/pullback_max_pct）原值不动；③ momentum 整块不动。
  // 旧实现把请求体整体反序列化成 typed struct 再整档覆盖 ⇒ ②③ 会被抹成零值，本条即其判据。
  test('M1 只改一个嵌套字段，兄弟字段与其他战法块不得被抹掉', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, ADMIN)) }
    const base = await (await request.get(API + '/api/config/strategy', { headers: H })).json()
    expect(base.dragon, '基线应含 dragon 块').toBeTruthy()
    try {
      const patched = { dragon: { f1_seal_weight: Number(base.dragon.f1_seal_weight) + 0.01 } }
      const save = await request.post(API + '/api/config/strategy', { headers: H, data: patched })
      expect(save.status(), '稀疏 patch 保存应 200').toBe(200)
      const back = await (await request.get(API + '/api/config/strategy', { headers: H })).json()
      expect(back.dragon.f1_seal_weight, '被改字段应生效').not.toBe(base.dragon.f1_seal_weight)
      for (const sib of ['f2_resonance_weight', 'f3_premium_weight', 'pullback_max_pct']) {
        expect(back.dragon[sib], `未提及的兄弟字段 ${sib} 必须原值保留`).toBe(base.dragon[sib])
      }
      expect(JSON.stringify(back.momentum), '未提及的 momentum 整块必须原值保留')
        .toBe(JSON.stringify(base.momentum))
    } finally {
      await request.post(API + '/api/config/strategy', { headers: H, data: restore(base) })
    }
  })

  // M2 版本冲突：带陈旧 updated_at 的写入必须 409 并回显当前版本（后写不许静默覆盖前写）。
  test('M2 基线版本过期 → 409 + 当前版本回显，参数不落地', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, ADMIN)) }
    const base = await (await request.get(API + '/api/config/strategy', { headers: H })).json()
    const stale = '2000-01-01T00:00:00Z' // 必然早于任何真实版本戳的哨兵基线
    try {
      const hit = await request.post(API + '/api/config/strategy', {
        headers: H,
        data: { updated_at: stale, momentum: { signal_threshold: 1.2345 } },
      })
      expect(hit.status(), '版本不一致应 409 而不是静默覆盖').toBe(409)
      const j = await hit.json()
      expect(j.current_updated_at, '409 响应必须回显当前版本供前端重载').toBeTruthy()
      const back = await (await request.get(API + '/api/config/strategy', { headers: H })).json()
      expect(back.momentum.signal_threshold, '被拒的写入不得留下任何痕迹')
        .toBe(base.momentum.signal_threshold)
    } finally {
      await request.post(API + '/api/config/strategy', { headers: H, data: restore(base) })
    }
  })

  // M3 版本戳服务端所有且随写前进：合法写入后，拿"写入前的版本"再写一次必须 409——
  // 只有服务端在盖戳、且戳会前进，这条才成立（客户端可自填/戳恒不变的实现都会在这里红）。
  test('M3 版本戳由服务端盖且随每次写入前进，旧版本随即失效', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, ADMIN)) }
    const get = async () => (await request.get(API + '/api/config/strategy', { headers: H })).json()
    let base = await get()
    let v0 = base.updated_at || ''
    if (!v0) {
      // 全新配置尚无版本戳（"首个写请求跳过比对并补盖"是既有语义）：先空写一次让服务端补盖，
      // 否则本用例的第二次写入会因 baseVersion 空串而再次跳过比对，把 409 断言跑成恒绿。
      await request.post(API + '/api/config/strategy', { headers: H, data: restore(base) })
      base = await get()
      v0 = base.updated_at || ''
    }
    expect(v0, '预热后应已存在服务端版本戳').toBeTruthy()
    try {
      const save = await request.post(API + '/api/config/strategy', {
        headers: H,
        data: { updated_at: v0, dragon: { f3_premium_weight: Number(base.dragon.f3_premium_weight) + 0.02 } },
      })
      expect(save.status(), '带当前版本的合法保存应 200').toBe(200)
      const v1 = (await save.json()).updated_at
      expect(v1, '响应必须回盖新戳').toBeTruthy()
      expect(Date.parse(v1) > 0, '服务端戳必须可解析为时间').toBe(true)
      expect(v1, '版本戳须随写入前进').not.toBe(v0)
      // 用刚失效的旧基线再写一次：必须 409（证明比对读的就是服务端那份新戳）
      const again = await request.post(API + '/api/config/strategy', {
        headers: H,
        data: { updated_at: v0, dragon: { f3_premium_weight: Number(base.dragon.f3_premium_weight) + 0.04 } },
      })
      expect(again.status(), '旧基线在他人写入后必须失效').toBe(409)
      expect((await again.json()).current_updated_at, '409 回显的当前版本即服务端新戳').toBe(v1)
    } finally {
      await request.post(API + '/api/config/strategy', { headers: H, data: restore(base) })
    }
  })
})

test.describe('§抄母仓-3 通知测试端点收权与频控', () => {
  // G1 抬档：该端点会真发外部推送，普通成员不得触发（403）。
  test('G1 成员 POST /api/notify-test → 403', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, MEMBER)) }
    const r = await request.post(API + '/api/notify-test', { headers: H, data: {} })
    expect(r.status(), '通知测试端点必须管理员档').toBe(403)
  })

  // G2 频控：全进程 60s 最小间隔；第二次必须 429 且带 Retry-After（429 无语义=调用方盲重试）。
  // 副作用说明：本条会消耗当次运行的 60s 通知测试窗口——全栈 e2e 中仅此一处触达该端点。
  test('G2 管理员连发 → 429 + Retry-After', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, ADMIN)) }
    const first = await request.post(API + '/api/notify-test', { headers: H, data: {} })
    expect([200, 429], '首发的语义：通道已配则实发 200，已被更早的请求占窗则 429')
      .toContain(first.status())
    const second = await request.post(API + '/api/notify-test', { headers: H, data: {} })
    expect(second.status(), '60s 窗口内连发必须被频控').toBe(429)
    const ra = second.headers()['retry-after']
    expect(ra, '429 必须回 Retry-After（否则调用方只能盲重试打爆日志）').toBeTruthy()
    expect(Number(ra), 'Retry-After 应为秒数且落在窗口内').toBeGreaterThan(0)
    expect(Number(ra), 'Retry-After 不应超过 60s 频控窗口').toBeLessThanOrEqual(60)
  })
})

test.describe('§抄母仓-5/7 告警出口与复权口径契约', () => {
  // A1 告警规则清单契约：/api/metrics/alerts 每条规则必须有 Name/Metric/Level，
  // 且 §DEADGAUGE 复活的三条（order_fail_rate/settlement_diff/llm_cooldown）必须在列——
  // 它们曾因"只有规则没有数据源"永不触发，规则从清单里掉出去是同一类事故的另一半。
  test('A1 /api/metrics/alerts 规则齐备且形状正确', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, ADMIN)) }
    const r = await request.get(API + '/api/metrics/alerts', { headers: H })
    expect(r.status(), 'admin 读告警规则应 200').toBe(200)
    const rules = (await r.json()).rules
    expect(Array.isArray(rules), 'rules 应为数组').toBe(true)
    expect(rules.length, '规则条数不应为 0（清单空=告警出口整体失联）').toBeGreaterThanOrEqual(8)
    for (const one of rules) {
      for (const k of ['name', 'metric', 'level']) {
        expect(typeof one[k], `规则 ${JSON.stringify(one)} 缺字段 ${k}`).toBe('string')
        expect(one[k], `规则字段 ${k} 不得为空串`).not.toBe('')
      }
    }
    const names = rules.map((x) => x.name)
    for (const revived of ['order_fail_rate', 'settlement_diff', 'llm_cooldown']) {
      expect(names, `复活规则 ${revived} 从清单里消失`).toContain(revived)
    }
  })

  // A2 鉴权档位：告警规则含内部阈值与指标名，成员档不得读（403）。
  test('A2 成员 GET /api/metrics/alerts → 403', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, MEMBER)) }
    expect((await request.get(API + '/api/metrics/alerts', { headers: H })).status()).toBe(403)
  })

  // E1 §ADJ-BASIS：情绪×战法矩阵必须回显自己算在哪个复权口径上。
  // 空串=改前旧证据行的哨兵值，永远不许作为当前口径对外发布（一旦回显空串，说明读侧又开始
  // 复用未标口径的旧行，或调用方把口径位当可选参数省略了）。
  test('E1 情绪×战法矩阵回显 adj_basis 且等于当前复权口径', async ({ request }) => {
    const H = { Authorization: 'Bearer ' + (await tok(request, ADMIN)) }
    const r = await request.get(API + '/api/research/emotion-strategy-matrix', { headers: H })
    test.skip(r.status() === 404, '本栈未提供矩阵端点（无研究库）——跳过而非判红')
    expect(r.status(), '矩阵端点应 200').toBe(200)
    const body = await r.json()
    expect(body.adj_basis, 'adj_basis 必须回显（缺字段=口径位又变成可选）').toBeTruthy()
    expect(body.adj_basis, '复权口径位漂移：请同步 verify 第 73/74 段与 research.AdjBaselineVersion')
      .toBe('hfq-forward-fill-1')
  })
})
