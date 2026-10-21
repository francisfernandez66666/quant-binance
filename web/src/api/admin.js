// ── admin.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──
// 职责：用户/账号管理接口（仅 admin 可达）。
// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；
//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。

import { request } from './request.js'

// ── 用户/账号管理（仅 admin）──
// ── User / account management (admin only) ──

/** 获取全部用户列表与权限位清单 */
/** Fetch the full user list and permission-bit catalog */
// 对应 GET /api/admin/users，返回 { users: [...], perms: [...] }；users 为公开视图（不含密码/令牌）
// Maps to GET /api/admin/users; returns { users: [...], perms: [...] }; users are public views (no password/token)
export async function fetchAdminUsers() {
  return request('/api/admin/users')
}

/** 创建用户（POST /api/admin/users） */
/** Create a user (POST /api/admin/users) */
export async function createAdminUser(data) {
  return request('/api/admin/users', { method: 'POST', data })
}

/** 设置用户角色（POST /api/admin/users/{id}/role） */
/** Set a user's role (POST /api/admin/users/{id}/role) */
export async function setAdminUserRole(id, role) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/role', { method: 'POST', data: { role } })
}

/** 整体覆盖用户权限位（POST /api/admin/users/{id}/perms） */
/** Replace a user's permission bits (POST /api/admin/users/{id}/perms) */
export async function setAdminUserPerms(id, perms) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/perms', { method: 'POST', data: { perms } })
}

/** 重置用户密码（POST /api/admin/users/{id}/password） */
/** Reset a user's password (POST /api/admin/users/{id}/password) */
export async function setAdminUserPassword(id, password) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/password', { method: 'POST', data: { password } })
}

/** 启用/禁用用户（POST /api/admin/users/{id}/enabled） */
/** Enable/disable a user (POST /api/admin/users/{id}/enabled) */
export async function setAdminUserEnabled(id, enabled) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/enabled', { method: 'POST', data: { enabled } })
}

/** 设置账号有效期（POST /api/admin/users/{id}/expiry，expires_days=0 表示永久） */
/** Set an account expiry (POST /api/admin/users/{id}/expiry; expires_days=0 means permanent) */
export async function setAdminUserExpiry(id, expiresDays) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/expiry', { method: 'POST', data: { expires_days: expiresDays } })
}

/** 删除用户（DELETE /api/admin/users/{id}，管理员不可删） */
/** Delete a user (DELETE /api/admin/users/{id}; the admin account cannot be deleted) */
export async function deleteAdminUser(id) {
  return request('/api/admin/users/' + encodeURIComponent(id), { method: 'DELETE' })
}

/** 批量清理脏账号（§U-5 POST /api/admin/users/cleanup）：过期账号 + 会话全部过期/已禁用的 temp_ 临时号。
 *  dryRun=true 仅预览不落刀（返回将被清理的清单）。 */
/** Bulk-reap stale accounts (§U-5): expired + dead/disabled temp_ temps; dryRun previews. */
// 对应 POST /api/admin/users/cleanup，body { dry_run }，返回 { deleted:[{id,username,reason}], count, dry_run }
export async function cleanupAdminUsers(dryRun = false) {
  return request('/api/admin/users/cleanup', { method: 'POST', data: { dry_run: !!dryRun } })
}

/** §MT 获取租户列表（GET /api/tenants，仅平台运营者角色可访问）
 *  返回全部租户及其配额/启用状态 */
export async function fetchTenants() {
  return request('/api/tenants')
}

/** §MT 创建租户（POST /api/tenants，可选随建租户管理员）
 *  data 为 { name, ... } 及可选的管理员账号信息，返回新租户对象 */
export async function createTenant(data) {
  return request('/api/tenants', { method: 'POST', data })
}

/** §MT 更新租户（PUT /api/tenants/{id}：name/enabled/quota 局部更新）
 *  仅传需要修改的字段，未传字段保持原值 */
export async function updateTenant(id, data) {
  return request('/api/tenants/' + encodeURIComponent(id), { method: 'PUT', data })
}

// §PERM-GATE/P2-FE-DEAD 20260918：以下死 wrapper 已删除（页面零调用）——
//   fetchTenantUsage（用量已由 fetchTenants 行内返回）、moveUserTenant（迁移无 UI，待定产品决策）、
//   fetchAdmin{D1,LongShort,LLM}Config / setAdmin…（按用户配置仅 strategy 有面板）。
// 对应后端路由（server.go:512/513/517-522）保留，供脚本/后续台账页使用。

/** 读取指定账号战法参数（GET /api/admin/users/{id}/config/strategy） */
export async function fetchAdminStrategyConfig(id) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/config/strategy')
}

/** 保存指定账号战法参数（POST /api/admin/users/{id}/config/strategy） */
export async function setAdminStrategyConfig(id, cfg) {
  return request('/api/admin/users/' + encodeURIComponent(id) + '/config/strategy', { method: 'POST', data: cfg })
}

/** §P2-f 参数优化：入队全库扫参任务（objective: profitFactor|winRate|avgWin；盘后窗口执行）。
 *  params 可选 {objective, start, end, top_n}；同 ref 幂等（已有排队/运行中返回现任务）。
 *  对应后端 POST /api/backtest/optimize。 */
export async function enqueueOptimize(params) {
  return request('/api/backtest/optimize', { method: 'POST', data: params || {} })
}

/** §DAILY_OPSLOG 每日系统运行日志（管理员）：列出已有日志的日期与大小（倒序）。
 *  对应后端 GET /api/opslog/dates。 */
export async function fetchOpslogDates() {
  return request('/api/opslog/dates')
}

/** §DAILY_OPSLOG 每日系统运行日志（管理员）：读取某日内容（date=YYYYMMDD，缺省今天；
 *  tail 可选，服务端取最后 N 行，默认 2000）。对应后端 GET /api/opslog。 */
export async function fetchOpslog(date, tail) {
  const q = []
  if (date) q.push('date=' + encodeURIComponent(date))
  if (tail) q.push('tail=' + encodeURIComponent(tail))
  return request('/api/opslog' + (q.length ? '?' + q.join('&') : ''))
}

/** §P2-f 查询寻优结果列表（按任务倒序分组，含每行排名/参数/指标/审批状态）
 *  对应后端 GET /api/research/optimizations */
export async function fetchOptimizations() {
  return request('/api/research/optimizations')
}

/** §D1 各战法寻优参数池：列出全部自定义四维步进配置（未配置战法走引擎默认池）
 *  对应后端 GET /api/research/sweep-pools */
export async function fetchSweepPools() {
  return request('/api/research/sweep-pools')
}

/** §D1 保存单战法四维步进搜索空间（服务端校验组合数护栏，超 10 万拒绝）
 *  对应后端 PUT /api/research/sweep-pools，body 为单战法配置对象 */
export async function saveSweepPool(cfg) {
  return request('/api/research/sweep-pools', { method: 'PUT', data: cfg })
}

/** §P2-f 审批一条寻优排名：规则级参数覆盖写 applied_*.json + 热重载实盘生效
 *  对应后端 POST /api/research/optimizations/{id}/approve
 *  §回测自动增强 D：可选 params（Pareto 推荐解四维参数）覆盖冠军行参数 */
export async function approveOptimization(id, params) {
  const data = params ? { params } : undefined
  return request('/api/research/optimizations/' + id + '/approve', { method: 'POST', data })
}

/** §回测自动增强 D 读取回测增强配置（GET /api/research/backtest-config）
 *  返回 { config: BacktestConfig, enabled: bool } */
export async function fetchBacktestConfig() {
  return request('/api/research/backtest-config')
}

/** §回测自动增强 D 保存回测增强配置（PUT /api/research/backtest-config，服务端校验越界 400）
 *  body 为完整 BacktestConfig 对象 */
export async function saveBacktestConfig(cfg) {
  return request('/api/research/backtest-config', { method: 'PUT', data: cfg })
}

/** §P2-f 淘汰一条寻优排名
 *  对应后端 POST /api/research/optimizations/{id}/reject */
export async function rejectOptimization(id) {
  return request('/api/research/optimizations/' + id + '/reject', { method: 'POST' })
}

/** §v2 清盘：支持显式指定重置后初始资金（reset_to）与持仓上限
 *  对应后端 POST /api/paper/reset（body 原样透传后端，与 resetPaper() 共用同一路由） */
export async function paperResetV2(body) {
  return request('/api/paper/reset', { method: 'POST', data: body })
}
