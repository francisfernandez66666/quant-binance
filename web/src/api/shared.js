// ── shared.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：共享基础设施：存储键名、AndroidAuth 原生桥探测、服务器基址、默认超时。被所有其他模块依赖，自身不依赖任何业务模块。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

export const BASE = ''

// localStorage 存储键名
// localStorage storage keys
// 统一带 'liangzai_' 前缀，避免键名与其他应用冲突
// Namespaced with 'liangzai_' prefix to avoid collisions with other apps
// STORAGE_KEY    存储 JWT 访问令牌
// STORAGE_KEY    holds the JWT access token
// STORAGE_SERVER 存储用户配置的后端服务器地址
// STORAGE_SERVER holds the user-configured backend server URL
// STORAGE_ACCOUNT 存储当前登录账号名
// STORAGE_ACCOUNT holds the currently logged in account name
export const STORAGE_KEY = 'liangzai_token'
export const STORAGE_SERVER = 'liangzai_server_url'
export const STORAGE_ACCOUNT = 'liangzai_account'
export const STORAGE_ROLE = 'liangzai_role'
// STORAGE_ROLE 存储当前登录账号角色（admin/member），角色位图随登录响应写入
// STORAGE_ROLE holds the logged-in account role (admin/member); the role bitmap is written from the login response
export const STORAGE_PERMS = 'liangzai_perms'

// §NATIVEAUTH（2026-09-22 C批）：探测移动端原生壳注入的安全存储桥 window.AndroidAuth
// §NATIVEAUTH (2026-09-22 batch C): detect the native-shell bridge window.AndroidAuth for secure token storage
// 桥只存在于新版 APK 内嵌 WebView（getToken/setToken(str):bool/clearToken():bool 三个同步方法），
// 纯浏览器与旧 APK 均无此桥，自然回落 localStorage 路径，行为完全不变。
// The bridge only exists inside the new APK's embedded WebView (three sync methods:
// getToken/setToken(str):bool/clearToken():bool); plain browsers & old APK have none and fall back to localStorage.
// 为什么不做 origin 判定：原生注入本身即信任边界（只对 appassets 内嵌页注入），
// Why no origin check: the native injection itself is the trust boundary (injected only into the appassets page),
// token 由此落入 EncryptedSharedPreferences（AndroidKeyStore 托管主密钥），root/adb-backup 的静态读取面收小；
// the token then lives in EncryptedSharedPreferences (master key held by AndroidKeyStore), shrinking the static
// read surface via root/adb-backup;
// JS 运行时可达性与 localStorage 等价（威胁模型不劣化）。
// JS-runtime reachability stays equivalent to localStorage (no threat-model regression).
export function nativeAuthBridge() {
  return (typeof window !== 'undefined' && window.AndroidAuth &&
          typeof window.AndroidAuth.setToken === 'function') ? window.AndroidAuth : null
}

// 从 localStorage 读取服务器基础地址
// Reads the base server URL from localStorage
// 说明：所有请求均基于该地址拼接相对路径；未配置时返回空串，表示使用同源相对请求
// Note: every request joins relative paths onto this base; returns '' when unset, meaning same-origin relative requests
// 追加兜底：在移动端 WebView（https://appassets.androidplatform.net 同源内嵌）里，
// 若 localStorage 尚无 server_url（例如首次安装且 JS 初始化早于原生注入），
// 直接回退到默认服务器地址，避免请求打到 appassets 无效源而报 "Failed to fetch"。
// (Fallback for the mobile WebView: when running on the appassets origin with no stored
// server URL yet, use the default server so fetches don't hit the invalid appassets origin.)
export function baseUrl() {
  const stored = localStorage.getItem(STORAGE_SERVER)
  if (stored) return stored
  if (typeof location !== 'undefined' && location.origin &&
      location.origin.indexOf('appassets.androidplatform.net') >= 0) {
    return 'https://quant-trading.top'
  }
  return ''
}

// 读取当前 JWT 令牌：原生桥优先，无桥回落 localStorage
// Reads the JWT token: native secure-storage bridge first, falls back to localStorage when absent
// 说明：令牌由登录接口写入，后续每个请求都会携带该令牌完成鉴权
// Note: the token is written by the login endpoint; every later request carries it for authentication
// §NATIVEAUTH（2026-09-22 C批）迁移语义：新版 APK 首启且用户未重新登录时，桥内为空但
// §NATIVEAUTH (2026-09-22 batch C) migration semantics: on the new APK's first launch without a re-login,
// localStorage 里还留着旧版明文 token —— 读到即自动上桥迁移（setToken 成功才删本地明文副本），
// the bridge is empty while localStorage still holds the legacy plaintext token — migrate it up on read
// 失败则暂留 localStorage 继续返回旧值，保证升级后不掉登录态。
// (setToken success is required before removing the local plaintext copy; on failure keep and return it,
//  so an upgrade never drops the logged-in state).
// 导出供 Settings.jsx 替换对 'liangzai_token' 的直读（诊断展示当前 token）。
// Exported so Settings.jsx replaces its direct 'liangzai_token' read (diagnostic token display).
export const LLM_PROBE_TIMEOUT = 120000
