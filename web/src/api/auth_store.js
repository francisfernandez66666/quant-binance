// ── auth_store.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：登录态读写：token 原生桥优先（§NATIVEAUTH）、账号/角色/权限本地状态、登出与刷新。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { BASE, STORAGE_KEY, STORAGE_SERVER, STORAGE_ACCOUNT, STORAGE_ROLE, STORAGE_PERMS, nativeAuthBridge, baseUrl } from './shared.js'
import { request } from './request.js'

export function getToken() {
  const bridge = nativeAuthBridge()
  if (!bridge) return localStorage.getItem(STORAGE_KEY)
  try {
    const native = bridge.getToken()
    if (native) return native
  } catch (e) {
    // 桥调用异常（理论上不该发生）：回落 localStorage，鉴权不因桥故障而中断
    // Bridge call threw (shouldn't happen in theory): fall back to localStorage, auth survives bridge faults
    if (typeof console !== 'undefined') console.warn('[api] AndroidAuth.getToken 异常，回落 localStorage:', e && e.message)
    return localStorage.getItem(STORAGE_KEY)
  }
  // 桥内为空 → 迁移旧版明文 token 上桥
  // Bridge empty → migrate the legacy plaintext token up to the bridge
  const legacy = localStorage.getItem(STORAGE_KEY)
  if (!legacy) return null
  try {
    if (bridge.setToken(legacy)) {
      localStorage.removeItem(STORAGE_KEY) // 明文已入加密存储，立即清除历史副本
      return legacy
    }
  } catch (e) {
    if (typeof console !== 'undefined') console.warn('[api] AndroidAuth.setToken 迁移失败:', e && e.message)
  }
  return legacy // 上桥失败：暂留 localStorage 明文（下次读取再尝试迁移），登录态优先
}

// 将登录成功后返回的 token、账号等信息持久化：token 原生桥优先，账号/角色/权限仍留 localStorage
// Persists the token / account returned after a successful login: token goes to the native bridge first,
// account/role/perms stay in localStorage
// @param {string} token      - JWT 访问令牌
// @param {string} token      - JWT access token
// @param {string} account    - 登录账号名（可空）
// @param {string} account    - logged-in account name (may be empty)
// @param {string} expiresAt  - 令牌过期时间（预留参数，当前未使用，用于将来做本地过期校验）
// @param {string} expiresAt  - token expiry time (reserved, currently unused, for future local expiry checks)
// §NATIVEAUTH（2026-09-22 C批）：有桥时 token 只写原生加密存储、不再写 STORAGE_KEY，
// §NATIVEAUTH (2026-09-22 batch C): with the bridge, the token is written only to native secure storage
// 并顺手 removeItem(STORAGE_KEY) 清掉历史明文副本（旧版本残留）；
// and the leftover plaintext copy from older versions is removed;
// account/role/perms 为非敏感展示信息维持 localStorage（无迁移价值）。
// account/role/perms are non-sensitive display info and stay in localStorage (not worth migrating).
// 无桥（纯浏览器/旧 APK）行为完全不变。导出供 native_auth 单测直接驱动写入路径。
// Without the bridge (plain browser / old APK) behavior is unchanged. Exported for the native_auth unit tests.
export function storeAuth(token, account, expiresAt, role, perms) {
  const bridge = nativeAuthBridge()
  if (bridge) {
    let stored = false
    try {
      stored = !!bridge.setToken(token)
    } catch (e) {
      if (typeof console !== 'undefined') console.warn('[api] AndroidAuth.setToken 异常，回落 localStorage:', e && e.message)
    }
    if (stored) {
      localStorage.removeItem(STORAGE_KEY) // 清历史明文副本：token 已落 EncryptedSharedPreferences
    } else {
      localStorage.setItem(STORAGE_KEY, token) // 上桥失败兜底：宁可留明文也不丢登录态
    }
  } else {
    localStorage.setItem(STORAGE_KEY, token)
  }
  localStorage.setItem(STORAGE_ACCOUNT, account || '')
  localStorage.setItem(STORAGE_ROLE, role || 'user')
  localStorage.setItem(STORAGE_PERMS, JSON.stringify(perms || []))
  // §M-11（2026-09-22 修复批）登录账号上桥：移动端原生壳据此派生并即时重设极光别名
  // （quant_<uid>），换账号不再停留在默认别名 quant_owner（定向推送变广播的根因）。
  // 旧版 APK 无 setAccount 方法——typeof 守卫后静默跳过，行为与迁移前一致。
  // English: §M-11 — report the logged-in account to the native shell so it can re-derive and
  // re-register the per-user JPush alias; old APKs without the method are skipped silently.
  if (bridge && typeof bridge.setAccount === 'function') {
    try { bridge.setAccount(account || '') } catch (e) {
      if (typeof console !== 'undefined') console.warn('[api] AndroidAuth.setAccount 异常（不影响登录态）:', e && e.message)
    }
  }
}

/**
 * §M-11（2026-09-22 修复批）把当前账号重新上报原生壳（登录态恢复路径用）。
 * 场景：App 冷启动、token 已持久化（storeAuth 在上一进程已跑过，但旧版 APK 升级 / 首装
 * 迁移场景壳侧可能从未收到过账号）——恢复登录态时补报一次，壳侧按期望别名幂等对账，
 * 无变化不会重复触发 setAlias。无桥（纯浏览器/旧 APK）为空操作。
 * English: re-report the current account to the native shell on session restore; the shell
 * dedupes against its desired-alias record, and is a no-op without the bridge.
 */
export function syncPushAccount() {
  const bridge = nativeAuthBridge()
  if (!bridge || typeof bridge.setAccount !== 'function') return
  try { bridge.setAccount(getAccount() || '') } catch (_) { /* 推送别名失败不影响会话 */ }
}

/**
 * 清除本地存储的认证信息（退出登录时调用）
 * Clears locally stored auth info (called on logout)
 * 同时移除 token 与账号，使 isLoggedIn() 立即失效
 * Removes both token and account so isLoggedIn() immediately becomes false
 */
// 清空本地登录态（token/账号/角色/权限）
// §NATIVEAUTH（2026-09-22 C批）：有桥时同步 clearToken()——localStorage 与原生加密存储双清，
// §NATIVEAUTH (2026-09-22 batch C): with the bridge also call clearToken() — clearing both
// 防残票（只清一边会留下可用令牌成"残票"，退出后仍可被请求携带续用）。
// localStorage and native secure storage, so no residual token survives logout.
export function clearAuth() {
  const bridge = nativeAuthBridge()
  if (bridge) {
    try {
      bridge.clearToken()
    } catch (e) {
      if (typeof console !== 'undefined') console.warn('[api] AndroidAuth.clearToken 异常:', e && e.message)
    }
    // §M-11（2026-09-22 修复批）登出同步下桥：账号别名退回壳侧默认值（quant_owner），
    // 登出后设备不再挂在前一账号的定向推送别名上。
    if (typeof bridge.setAccount === 'function') {
      try { bridge.setAccount('') } catch (e) {
        if (typeof console !== 'undefined') console.warn('[api] AndroidAuth.setAccount(登出) 异常:', e && e.message)
      }
    }
  }
  localStorage.removeItem(STORAGE_KEY)
  localStorage.removeItem(STORAGE_ACCOUNT)
  localStorage.removeItem(STORAGE_ROLE)
  localStorage.removeItem(STORAGE_PERMS)
}

/** §D7 自助退出：先向服务端吊销当前 Bearer 对应的会话（POST /api/auth/logout）。
 *  本地 clearAuth 立即执行（不 await 网络），避免用户快速重登时旧请求把新令牌误清；
 *  网络请求带 captured token 走 fetch 直连，不走 request() 的 getToken() 路径。
 *  English: clears local state synchronously then revokes server-side using the captured token. */
// 登出：通知后端作废会话并清本地凭据
export async function logout() {
  const base = baseUrl()
  // §NATIVEAUTH（2026-09-22 C批）：改走 getToken() 而非直读 localStorage——token 在原生路径下
  // §NATIVEAUTH (2026-09-22 batch C): read via getToken() instead of raw localStorage — under the native
  // bridge the token lives in secure storage, a direct localStorage read would miss it and the server-side
  // session would never be revoked（拿不到票 → 服务端会话不吊销）。
  // 必须在 clearAuth() 前捕获（快照 token 供吊销请求使用）。
  // Must capture before clearAuth() (snapshot token carried by the revoke request).
  const token = getToken()
  clearAuth() // 立即清，网络请求带快照 token 继续跑
  if (!token) return
  try {
    await fetch(base + '/api/auth/logout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    })
  } catch (e) {
    if (typeof console !== 'undefined') console.warn('[api] 服务端 logout 网络失败:', e && e.message)
  }
}

/** §Dashboard 情绪面板 A：最近 N 交易日市场情绪/风险档快照（默认 30）。
 *  数据源 market_risk_daily，引擎每交易日落一行；前端消费点：情绪色带 + 涨停/连板指标。
 *  English: sentiment history for the Dashboard sentiment card (defaults to last 30 sessions). */
export function isLoggedIn() {
  return !!getToken()
}

/**
 * 获取当前登录账号名
 * Returns the current logged-in account name
 * @returns {string} 账号名，未登录时返回空字符串
 * @returns {string} account name, empty string when not logged in
 */
// 本地缓存的登录账号名
export function getAccount() {
  return localStorage.getItem(STORAGE_ACCOUNT) || ''
}

/**
 * 获取当前登录用户角色（admin / user）
 * Returns the current logged-in user's role (admin / user)
 */
// 本地缓存的角色（缺省 user）
export function getRole() {
  return localStorage.getItem(STORAGE_ROLE) || 'user'
}

/**
 * 获取当前登录用户的权限位列表
 * Returns the current logged-in user's permission bits
 * @returns {string[]} 权限位数组
 */
// 本地缓存的细粒度权限列表
export function getPerms() {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_PERMS) || '[]')
  } catch (_) {
    return []
  }
}

/**
 * 当前用户是否为管理员
 * Whether the current user is an admin
 */
// 是否管理员角色
export function isAdmin() {
  return getRole() === 'admin'
}

/**
 * 当前用户是否拥有指定权限位（管理员隐式全部）
 * Whether the current user holds a permission bit (admin implies all)
 * @param {string} perm - 权限位名
 */
// 权限判定：管理员恒真，其余查权限表
export function hasPerm(perm) {
  if (isAdmin()) return true
  return getPerms().indexOf(perm) >= 0
}

/**
 * 拉取并缓存当前登录用户信息（角色/权限位）
 * Fetches and caches the current user's profile (role / permission bits)
 * 对应后端 GET /api/auth/me；成功后把 role/perms 写回 localStorage，
 * 供 isAdmin()/hasPerm() 在换账号后读到最新权限
 * @returns {Promise<object>} { id, username, role, perms, enabled }
 */
// 刷新当前用户角色/权限缓存
export async function refreshMe() {
  const me = await request('/api/auth/me')
  localStorage.setItem(STORAGE_ROLE, me.role || 'user')
  localStorage.setItem(STORAGE_PERMS, JSON.stringify(me.perms || []))
  return me
}

/**
 * §A5（20260918 审计批）错误是否为后端 403（无权限）。
 * 依赖 request() 在非 2xx 抛错上挂载的 status；网络层错误无 status 一律 false。
 * English: §A5 — true only for backend 403 responses surfaced by request().
 * @param {Error|any} e - request() 抛出的错误对象
 */
export function isForbidden(e) {
  return !!(e && e.status === 403)
}

/**
 * 获取持久化的服务器地址
 * Returns the persisted server URL
 * @returns {string} 服务器地址，未配置时返回空字符串
 * @returns {string} server URL, empty string when not configured
 */
// 读取本地保存的后端服务器地址
export function getStoredServer() {
  return localStorage.getItem(STORAGE_SERVER) || ''
}

/**
 * 持久化保存服务器地址
 * Persists the server URL
 * 说明：写入 localStorage，供 baseUrl() 在拼接所有请求时读取
 * Note: written to localStorage; read by baseUrl() when building every request
 * @param {string} url - 服务器地址
 * @param {string} url - server URL
 */
// 保存后端服务器地址到本地
export function setStoredServer(url) {
  localStorage.setItem(STORAGE_SERVER, url)
}
