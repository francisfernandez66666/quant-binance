// ── request.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：统一请求封装 request()：超时控制、同源回退重试（仅幂等 GET）、鉴权头注入、401 处理。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { BASE, STORAGE_SERVER, LLM_PROBE_TIMEOUT, baseUrl } from './shared.js'
import { getToken, clearAuth } from './auth_store.js'

// ── 通用请求封装 ──
// ── Generic request wrapper ──

/** 请求超时时间（毫秒），防止慢接口把页面卡在空状态 */
/** Request timeout (ms) to keep slow endpoints from leaving the page stuck empty */
// 说明：超过该时长仍未得到响应，则通过 AbortController 中止请求并抛出“请求超时”
// Note: if no response within this window, the request is aborted via AbortController and "请求超时" (request timeout) is thrown
const REQUEST_TIMEOUT = 10000

/**
 * LLM 配置类接口（测试连接 / 保存 / 回滚）的请求超时。
 * Request timeout for LLM-config endpoints (probe / save / rollback), in ms.
 *
 * 为什么必须单独给这三个接口一个长超时：后端在它们里面**真的出网逐把探测**候选密钥
 * （internal/llm/probe.go：单把 DefaultProbeTimeout=45s、并发 4 路、总预算
 * ProbeMaxTotalBudget=90s）。用通用默认 10s 的后果是"后端还在探测、浏览器已经放弃"，
 * 用户看到「探测失败: 请求超时」，而后端日志里那次探测其实是成功的
 * （2026-09-20 生产实录：后端 probe=53975ms/58214ms/59421ms，前端 10s 断）。
 *
 * 取值依据：必须 ≥ 后端总预算（90s），再留出等 llmApplyMu 互斥锁 + 网络往返的余量。
 * 后端已有硬上界 ⇒ 这里**不会**在正常路径上先到点，超时只可能发生在真异常时。
 *
 * Why these three endpoints need their own longer timeout: the backend performs *real* network
 * probes inside them (45s per key, 4 concurrent, 90s total budget). With the generic 10s default
 * the browser gives up while the server is still probing — the user sees "请求超时" even though
 * the probe succeeded server-side. Must be ≥ the backend budget (90s) plus lock/round-trip slack.
 */

/** 导出供单测锁"前端超时 ≥ 后端探测预算"这条契约（改了后端预算就必须同步改这里）。 */
export const LLM_PROBE_TIMEOUT_MS = LLM_PROBE_TIMEOUT

/**
 * 统一的 HTTP 请求封装，自动附加认证头、处理 401 过期
 * Unified HTTP request wrapper that auto-attaches the auth header and handles 401 expiry
 * 原理：
 * How it works:
 *  - URL = baseUrl() + path，即用户配置的服务器地址拼接相对路径；
 *  - URL = baseUrl() + path, i.e. the user-configured server URL joined with the relative path;
 *  - 若本地存在 token，则自动附加 Authorization: Bearer <token> 请求头；
 *  - if a token exists locally, the Authorization: Bearer <token> header is attached automatically;
 *  - 使用 AbortController + setTimeout 实现超时中断，超时后抛“请求超时”；
 *  - an AbortController + setTimeout implements the timeout; "请求超时" (request timeout) is thrown on abort;
 *  - 响应为 401 时视为令牌过期，自动清除本地登录态并抛出“登录已过期”；
 *  - a 401 response means the token expired: local auth state is cleared and "登录已过期" (login expired) is thrown;
 *  - 默认以 JSON 格式发送 / 接收数据。
 *  - data is sent / received as JSON by default.
 * @param {string} path - API 路径（相对路径）
 * @param {string} path - API path (relative)
 * @param {object} [opts] - 可选参数 { method, data, headers }
 * @param {object} [opts] - optional args { method, data, headers }
 *   method:  HTTP 方法，默认 GET
 *   method:  HTTP method, GET by default
 *   data:    请求体对象，会被 JSON.stringify 序列化后作为 body
 *   data:    request body object, serialized by JSON.stringify as body
 *   headers: 附加请求头，与默认头合并（同名可覆盖默认值）
 *   headers: extra headers merged with defaults (same key overrides the default)
 * @returns {Promise<object>} 响应 JSON
 * @returns {Promise<object>} response JSON
 */
// §P2-15（2026-09-15）：导出 request 供单测直接驱动四个错误分支（401 清态/超时/同源回退/非 JSON 错误体）
export async function request(path, opts = {}) {
  // 拼接完整请求地址：服务器基础地址 + 相对路径
  // Build the full request URL: server base URL + relative path
  const base = baseUrl()
  // 设置请求超时（默认 REQUEST_TIMEOUT，可通过 opts.timeout 覆盖，如 LLM 咨询需要更长等待），超时后中止请求
  // Set a request timeout (default REQUEST_TIMEOUT, overridable via opts.timeout, e.g. LLM consult needs to wait longer); abort on timeout
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), opts.timeout || REQUEST_TIMEOUT)

  // 单次请求执行：base 为基础地址（可为自定义服务器或同源空串）
  // Single attempt: base is the server base (custom server URL or same-origin empty string)
  const doFetch = async (tryBase) => {
    const url = tryBase + path
    // 合并请求头：默认 JSON 头在前，opts.headers 同名可覆盖默认值
    const headers = { 'Content-Type': 'application/json', ...opts.headers }
    // 鉴权：本地持有 token 则自动附加 Authorization: Bearer 头
    const token = getToken()
    if (token) headers['Authorization'] = 'Bearer ' + token
    return fetch(url, {
      method: opts.method || 'GET',
      headers,
      body: opts.data ? JSON.stringify(opts.data) : undefined,
      signal: ctrl.signal,
    })
  }

  let res
  try {
    res = await doFetch(base)
  } catch (e) {
    // 网络层失败（DNS 解析失败 / 连接被拒 / 跨域 / 超时）：若配置了自定义服务器且同源可用，
    // 自动回退到「当前页面同源」再试一次——修复「设置了失效的自定义服务器地址后，
    // 登录态靠缓存令牌保留、但所有页面都拿不到后端数据」的系统性断联问题。
    // English: on a network-level failure (DNS/connection/CORS/timeout), if a custom server was
    // configured but same-origin is reachable, retry against same-origin once. This heals the
    // "stale custom server URL keeps you logged in (cached token) yet every page is empty" case.
    if (e && e.name === 'AbortError') {
      clearTimeout(timer)
      throw new Error('请求超时')
    }
    // 非超时类网络错误：若配置了自定义服务器，则回退当前页面同源地址重试一次。
    // §FIX-9i(2026-09-19)：回退重发仅限幂等的 GET——此前不分方法，POST /api/consult
    // 这类"计费 + 写历史"的请求在自定义地址网络失败后会被原样重发一次，等于用户
    // 点一次发送花两份钱、落两条历史。非幂等方法（POST/PUT/PATCH 等）直接抛原始错误。
    // English: same-origin fallback retry is now restricted to idempotent GET; billable
    // POSTs (e.g. consult) must never be auto-resent.
    if (base !== '' && (opts.method || 'GET') === 'GET') {
      try {
        if (typeof console !== 'undefined') {
          console.warn('[api] 自定义服务器地址不可达，回退同源请求:', base + path)
        }
        res = await doFetch('')
      } catch (e2) {
        clearTimeout(timer)
        throw e2 || e
      }
      // 未配置自定义服务器（base 为空同源请求）或不可重发方法：无回退余地，直接抛出原始网络错误
    } else {
      clearTimeout(timer)
      throw e
    }
  }
  clearTimeout(timer)

  // 401 表示令牌过期，清除本地登录态
  // 401 means the token expired; clear local auth state
  if (res.status === 401) {
    clearAuth()
    // 广播"登录过期"事件，让 App 层监听后退出登录回到登录页（配合 SSE 无限重连的兜底）
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new Event('auth:expired'))
    }
    const expired = new Error('登录已过期')
    expired.status = 401 // §A5：过期同样携带状态码，便于调用方分支
    throw expired
  }
  // 非 2xx：尝试解析后端错误信息，解析失败则给出明确状态码提示，
  // Non-2xx: try to parse the backend error message; fall back to a clear status-code note if parsing fails,
  // 避免下游 res.json() 对 HTML 等非 JSON 响应抛出的隐晦异常。
  // avoiding cryptic errors from downstream res.json() on non-JSON responses such as HTML.
  if (!res.ok) {
    let msg = '请求失败 ' + res.status
    let code = ''
    try {
      const e = await res.json()
      if (e && e.error) msg = e.error
      // §FIX-9d(2026-09-19)：后端错误体新增机读 code（如 llm_not_configured/consult_budget_exceeded），
      // 随错误对象透传——调用方从此不再拿 message 做"配置"等关键字猜测。
      if (e && e.code) code = e.code
    } catch (_) {}
    // §A5（20260918 审计批）：错误对象挂载 HTTP 状态码——此前调用方只能拿 message 做关键字
    // 匹配（如 e.message.indexOf('无权限')），无法机读判定 403；现页面/守卫可用 isForbidden()
    // 把"数据首拉即 403"统一重路由到 /403。
    // English: §A5 — non-2xx errors now carry res.status so callers can test 403 programmatically.
    const err = new Error(msg)
    err.status = res.status
    err.code = code
    throw err
  }
  // 成功响应：解析 JSON 返回给调用方（后端接口约定均为 JSON 体）
  return res.json()
}
