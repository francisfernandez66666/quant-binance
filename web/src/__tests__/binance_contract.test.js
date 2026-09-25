// ── 币安 fetch 层契约用例 binance_contract.test.js ──
// 文件职责：§AUDITFIX925-D2（2026-09-25 审计批）——把 web/src/api/binance.js 的
// 方法+路径+载荷钉成「等于后端实注册契约」的等值断言（证据行号见各用例注释），
// 防止"注释里写 /cancel/{id}、函数照抄、后端其实是 POST+body"这类地雷再次复活。
// 后端契约单一事实源：internal/server/server.go 路由注册 + internal/server/binance_api.go 处理器。
// 断言口径：等值（method/path/body 三者全等），不用"包含"式弱断言——弱断言放得过拼错的路径。
// English: byte-level contract locks for the Binance fetch layer (method+path+body equality).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { cancelBinanceOrder, fetchBinanceExchangeInfo, fetchBinanceOrders, binanceHalt } from '../api/binance.js'

describe('api/binance 契约锁（§AUDITFIX925-D2）', () => {
  let calls = []
  beforeEach(() => {
    calls = []
    // 全局 fetch 桩：记录 url/options，返回统一空 JSON，不真发请求
    global.fetch = vi.fn(async (url, opts) => {
      calls.push({ url: String(url), opts })
      return { status: 200, ok: true, json: async () => ({}), text: async () => '{}' }
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // 后端实况：POST /api/binance/cancel，body {"market":"US|CRYPTO","order_id":"..."}
  //（server.go:699 注册；binance_api.go:244-249 解析，order_id 走 body 非路径）。
  it('cancelBinanceOrder = POST /api/binance/cancel + body{market,order_id}（无路径参数）', async () => {
    await cancelBinanceOrder('CRYPTO', '123456')
    expect(calls).toHaveLength(1)
    const { url, opts } = calls[0]
    expect(url).toMatch(/\/api\/binance\/cancel$/) // 收紧到行尾：任何 /cancel/{id} 路径拼接形态都判红
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ market: 'CRYPTO', order_id: '123456' })
  })

  // 后端实况：GET /api/binance/exchange_info?symbol=…（server.go:698；
  // binance_api.go:311-314 缺 symbol 直接 400——所以 symbol 是必传参数而非可选）。
  it('fetchBinanceExchangeInfo = GET + symbol 查询参数（旧 POST 触发刷新语义判红）', async () => {
    await fetchBinanceExchangeInfo('BTCUSDT')
    const { url, opts } = calls[0]
    expect(url).toMatch(/\/api\/binance\/exchange_info\?symbol=BTCUSDT$/)
    // 缺省方法即 GET（request() 内 opts.method || 'GET'）；显式 undefined/POST 都判红
    expect(opts.method === undefined || opts.method === 'GET').toBe(true)
    expect(opts.body).toBeUndefined() // GET 不带体：防"POST 刷新"语义复活
  })

  // 后端实况：GET /api/binance/orders?market=…（server.go:700；响应是数组，CN 行不回流）。
  it('fetchBinanceOrders = GET，market 过滤走查询串、ALL 不带参', async () => {
    await fetchBinanceOrders('US')
    expect(calls[0].url).toMatch(/\/api\/binance\/orders\?market=US$/)
    await fetchBinanceOrders('ALL')
    expect(calls[1].url).toMatch(/\/api\/binance\/orders$/)
  })

  // 后端实况：POST /api/binance/halt body {"halted":bool}（server.go:701；
  // binance_api.go:270-274 halted 为 null 直接 400——必须显式传布尔）。
  it('binanceHalt = POST + body{halted:严格布尔}', async () => {
    await binanceHalt(true)
    const { url, opts } = calls[0]
    expect(url).toMatch(/\/api\/binance\/halt$/)
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ halted: true })
  })
})
