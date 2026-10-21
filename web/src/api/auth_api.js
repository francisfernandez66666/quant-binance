// ── auth_api.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：认证接口：登录/登出等账号动作。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'
import { baseUrl } from './shared.js'
import { storeAuth } from './auth_store.js'

// ── 认证接口 ──
// ── Authentication ──

/**
 * 登录：提交用户名密码，存储返回的 token
 * Login: submit username/password, store the returned token
 * 说明：登录接口不使用统一 request() 封装，因为登录时尚未持有 token，
 * Note: login does not use the unified request() wrapper because no token is held yet,
 *       且需要单独处理非 2xx 响应的错误信息提取（后端返回 err 字段）。
 *       and it extracts the error message from non-2xx responses separately (backend returns an err field).
 * @param {string} username - 用户名
 * @param {string} username - username
 * @param {string} password - 密码
 * @param {string} password - password
 * 对应后端 POST /api/auth/login，请求体 { username, password }
 * @returns {Promise<object>} { token, account, expires_at }
 */
// 登录：换取 token 并落盘账号/角色/权限
export async function login(username, password) {
  const url = baseUrl() + '/api/auth/login'
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) {
    // 非 2xx 响应时提取后端错误信息并抛出
    // On non-2xx responses, extract the backend error message and throw
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || '登录失败')
  }
  const data = await res.json()
  // 登录成功后持久化 token 与账号
  // Persist token and account after a successful login
  storeAuth(data.token, data.account, data.expires_at, data.role, data.perms)
  return data
}
