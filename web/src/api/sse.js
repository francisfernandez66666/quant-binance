// ── sse.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：SSE 服务端推送事件订阅与自动重连。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'
import { BASE, baseUrl } from './shared.js'
import { getToken } from './auth_store.js'

// ── SSE 服务端推送事件 ──
// ── SSE server push events ──
// 原理：SSE（Server-Sent Events）是单向服务端推送协议，基于浏览器 EventSource 实现长连接。
// How it works: SSE (Server-Sent Events) is a one-way server-push protocol using the browser's EventSource for a long-lived connection.
// 与 WebSocket 不同，SSE 仅由服务端向客户端推送，连接成本低且自带重连，适合行情 / 信号类通知。
// Unlike WebSocket, SSE only pushes server-to-client with low connection cost and built-in reconnection, ideal for quotes / signals.
//  - 建立连接前先读取 token，并以查询参数 ?token=... 带给后端完成鉴权；
//  - the token is read before connecting and sent to the backend as a ?token=... query param for auth;
//  - 收到消息后按 JSON 解析，广播给所有已注册回调（sseCallbacks）；
//  - incoming messages are parsed as JSON and broadcast to all registered callbacks (sseCallbacks);
//  - 连接出错时不再主动 close：§H6 起优先让浏览器原生自动重连接管（携带 Last-Event-ID 走服务端补发），
//    仅实例被判 CLOSED 后退避手动重建。
//  - §H6: on error the browser's native reconnect takes over first (it alone carries the
//    Last-Event-ID header for server-side replay); manual backoff-rebuild only after a fatal CLOSED.

// 当前 SSE 连接对象（EventSource 实例），null 表示未连接
// The current SSE connection object (an EventSource instance); null means disconnected
let sse = null
// 已注册的 SSE 消息回调列表，新增信号等推送会依次通知所有回调
// List of registered SSE callbacks; pushes such as new signals notify each callback in turn
let sseCallbacks = []
// SSE 连续重连次数，用于退避重连与登录态失效探测（成功收到消息后重置为 0）
let sseRetry = 0
// §P1-10（2026-09-15）进行中的建链 promise（connectSSE 并发守卫用，见 connectSSE 内注释）
let sseConnecting = null
// §H6（2026-09-22 修复批）：兜底重建定时器。仅当 EventSource 被判 CLOSED、需要手动换票重建时
// 使用；同一轮断开只允许挂一个定时器（去重），退出登录时由 disconnectSSE 一并清除。
let sseReconnectTimer = null
// §M5（2026-09-22 PM 批）：客户端记录的最后一个事件序号（EventSource 的 lastEventId，
// 来自服务端 `id:` 行）。手动换票重建的新 EventSource 附加不了 Last-Event-ID 请求头
// （该头只有浏览器原生重连会自动携带），故把续读位置以 `?last_event_id=` query 带进
// 重建 URL，服务端补发环按 (账号, 序号) 查询（handlers_fix.go handleFixSSE）。
// 注意：disconnectSSE 不清此值——CLOSED 兜底重建正是「disconnectSSE → 退避 → connectSSE」
// 一条链，在这里清等于又把续读位置丢了。
// English: §M5 — last seen SSE event id, replayed on manual rebuild via the ?last_event_id=
// query (headers are reserved for the browser's native reconnect).
let sseLastEventId = ''

/**
 * 注册 SSE 消息回调，返回取消注册的函数
 * Register an SSE message callback; returns an unsubscribe function
 * @param {Function} fn - 消息处理回调，入参为解析后的消息对象
 * @param {Function} fn - the handler callback receiving the parsed message object
 * @returns {Function} unsubscribe - 调用后将该回调从列表中移除
 * @returns {Function} unsubscribe - removes this callback from the list when invoked
 */
// 注册 SSE 消息回调，返回注销函数
export function onSSE(fn) {
  sseCallbacks.push(fn)
  return () => { sseCallbacks = sseCallbacks.filter(f => f !== fn) }
}

/** 建立与后端的 SSE 长连接，接收实时推送 */
/** Establish the SSE long connection to the backend to receive realtime pushes */
// 说明：
// Notes:
//  - 已有连接或未登录时直接返回（幂等操作，避免重复建连）；
//  - returns immediately if already connected or not logged in (idempotent, avoids duplicate connections);
//  - §WS-F C4a：先 POST /api/events/ticket 取 60s 短时效票据，再以
//    connection URL = baseUrl() + '/api/events?ticket=' + encodeURIComponent(ticket) 建链；
//    §M5 起票据 TTL 内可复用——浏览器原生重连只会原样重发同一 URL，旧「消费即废」语义
//    让每次重连必然 401，唯一带 Last-Event-ID 头的通道被掐死。
//  - §WS-F C4a: mint a 60s ticket at POST /api/events/ticket, then connect with
//    baseUrl() + '/api/events?ticket='. Since §M5 the ticket stays valid (read-only, account-bound)
//    for the rest of its TTL so the browser's native reconnect — which replays the exact same URL —
//    succeeds instead of dying on a guaranteed 401.
//  - 收到消息时解析 JSON 并依次调用 sseCallbacks 中的回调；同时记录 lastEventId（§M5 重建续传用）；
//  - received messages are parsed as JSON and dispatched to each callback in sseCallbacks;
//  - onerror 触发时：§H6 起不再手动 close+新建——非 CLOSED 态交给浏览器原生自动重连
//    （原生重连会自动携带 Last-Event-ID 头，服务端据此补发断流期间错过的事件）；
//    仅当 EventSource 被彻底关闭（CLOSED，如票据 60s TTL 已耗尽后重连 401）才手动换票重建，
//    并以 ?last_event_id= query 携带续读位置（§M5），补发环不再对重建路径不可达。
//  - English: §H6 — on error the client now lets the browser's native auto-reconnect run untouched
//    (native reconnect is the only path that carries the Last-Event-ID header the server's replay
//    ring reads); a manual close-and-recreate happens only after the EventSource fatally CLOSED.
export async function connectSSE() {
  if (sse) return
  // §P1-10（2026-09-15）：建链并发守卫。connectSSE 在取票据（await POST /api/events/ticket）
  // 期间 sse 仍为 null，Dashboard/LLMDebug 等页面 effect 或 onerror 重连若在此窗口并发调用，
  // 会各自通过 null 检查开出第二条 EventSource——后端将看到同一 token 的重复连接，事件被双份投递。
  // 现以模块级 connecting promise 兜住：窗口内的并发调用直接复用同一次建链结果。
  // English: race guard. The ticket fetch awaits while `sse` is still null, so concurrent
  // connectSSE() calls (two page effects, or an onerror reconnect) would each pass the null
  // check and open duplicate EventSources. A module-level connecting promise dedupes them.
  if (sseConnecting) return sseConnecting
  const run = async () => {
    const token = getToken()
    // 未登录时不建立连接
    // Do not connect when not logged in
    if (!token) return
  // 每次（重）建链都新签一张票：旧票即便 TTL 内可复用也可能已过期，换新的最稳
  // Mint a fresh ticket per (re)connect; the previous one may have expired
    let ticket = ''
    try {
      const resp = await request('/api/events/ticket', { method: 'POST' })
      ticket = resp && resp.ticket ? resp.ticket : ''
    } catch (e) {
      // §A6（20260918 审计批）：取票失败不再整体静默——旧行为下 SSE 链路断裂但页面
      // 毫无痕迹，只能靠 60s 轮询兜底续命；留 warn 便于控制台直确认"实时通道没通"。
      // English: §A6 — log ticket-mint failure instead of silently losing the realtime channel.
      if (typeof console !== 'undefined') console.warn('[api] SSE 票据签发失败，实时推送暂不可用:', (e && e.message) || e)
    }
    if (!ticket) {
      // 票据签发失败（如 token 失效）：走一次探测，交给上层统一回到登录态
      // Ticket mint failed (e.g. dead token): probe once and let the app return to login
      request('/api/status').catch(() => {})
      return
    }
    // 说明：浏览器 EventSource 会自动携带 Last-Event-ID 头（来自服务端 `id:` 行），
    //       服务端据此实现断线续传（见 handleFixSSE 读取 Last-Event-ID）。
    // Note: the browser EventSource automatically sends the Last-Event-ID header
    //       (from the server's `id:` line), enabling reconnect resume server-side.
    // §H6 保住的是「原生重连带请求头」这条通道；§M5 补上另一条：手动换票重建附加不了
    // 请求头，改以 `?last_event_id=` query 携带同一续读位置，服务端两种载体共用补发环。
    let url = baseUrl() + '/api/events?ticket=' + encodeURIComponent(ticket)
    if (sseLastEventId) url += '&last_event_id=' + encodeURIComponent(sseLastEventId)
    sse = new EventSource(url)
    // §H6：实例身份守卫——旧实例（已被替换/关闭）迟到的回调不应再操作当前连接状态
    const es = sse
    sse.onmessage = (e) => {
      // 成功收到一条消息即重置重连计数（连接已恢复）
      sseRetry = 0
      // §M5：记录续读位置（服务端 `id:` 行），CLOSED 重建时以 query 带回
      if (e.lastEventId) sseLastEventId = e.lastEventId
      try {
        const msg = JSON.parse(e.data)
        sseCallbacks.forEach(fn => fn(msg))
      } catch (_) {}
    }
    sse.onerror = () => {
      // §H6（2026-09-22 修复批）冷启动丢补发：旧实现在此先 disconnectSSE() 手动 close、
      // 再 setTimeout 新建 EventSource——恰好打断了浏览器原生自动重连通道。Last-Event-ID
      // 请求头只有原生重连才会自动携带（服务端补发环 sse.go SubscribeFor(lastID) 只认这个），
      // close+new 后服务端永远收不到该头，断流期间错过的事件无法恢复。
      // 现策略：只要实例还没被判 CLOSED（CONNECTING 表示浏览器正按其 `retry:` 提示自行
      // 退避重连），绝不触碰连接、也不手动重建，把补发机会完整留给原生通道；期间仅按
      // 计数做登录态探测（token 失效时 request() 广播 auth:expired → App 层登出并
      // disconnectSSE，从而终止无限重连，§P1-9 语义不变）。
      // English: §H6 — never close-and-resurrect while the browser's native reconnect (the only
      // carrier of the Last-Event-ID header) is still alive; manual rebuild is a fatal-CLOSED fallback.
      if (sse !== es) return
      if (sse.readyState !== 2 /* EventSource.CLOSED */) {
        sseRetry++
        // 连续多次原生重连仍失败时，探测一次登录态：若 token 已失效（401），
        // request() 内部会 clearAuth 并广播 auth:expired，App 层据此回到登录页，
        // 从而终止对过期 token 的无限重连。
        if (sseRetry === 5) {
          request('/api/status').catch(() => {})
        }
        return
      }
      // 走到这里说明 EventSource 已被浏览器彻底放弃（CLOSED，典型：断流超过票据 60s TTL 后
      // 重连收到 401）——原生通道已不可用，只能手动换票重建兜底。§M5：重建 URL 带
      // ?last_event_id=（见 connectSSE 建链处），补发环对这条路径同样可达，断流期间
      // 错过的事件不再永久丢失。
      // English: §M5 — the fatal-CLOSED manual rebuild carries the resume position as a query, so the
      // server's replay ring is reachable on this path too (previously events missed during the
      // outage were silently dropped, the "known degradation" the old comment conceded).
      disconnectSSE()
      sseRetry++
      // 退避延迟：3s 起，指数增长并封顶 30s，避免网络异常时无限快速重连风暴
      // Backoff delay: starts at 3s, grows exponentially and caps at 30s to avoid a reconnect storm
      const delay = Math.min(3000 * Math.pow(1.6, sseRetry - 1), 30000)
      // §H6：去重守卫——同一次断开的连环 error 只挂一个重建定时器
      if (sseReconnectTimer) return
      sseReconnectTimer = setTimeout(() => {
        sseReconnectTimer = null
        connectSSE()
      }, delay)
    }
  }
  // §P1-10 记录进行中的建链 promise，finally 里清除——成功/失败都不留悬挂守卫
  sseConnecting = run()
  try {
    await sseConnecting
  } finally {
    sseConnecting = null
  }
}

/** 断开 SSE 长连接 */
/** Disconnect the SSE long connection */
// 说明：关闭连接并将 sse 置空，使下次 connectSSE() 可以重新建立连接
// Note: closes the connection and nulls `sse` so the next connectSSE() can reconnect
export function disconnectSSE() {
  if (sse) { sse.close(); sse = null }
  // §H6：主动断开（登出/卸载）时同步清掉待触发的兜底重建定时器，
  // 避免旧定时器在退出登录后把连接复活（登录页挂死连接）。
  // English: §H6 — an explicit disconnect also cancels a pending backoff rebuild so a
  // stale timer cannot resurrect the stream after logout.
  if (sseReconnectTimer) { clearTimeout(sseReconnectTimer); sseReconnectTimer = null }
}
