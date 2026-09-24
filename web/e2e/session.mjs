// ── §UAT-SESSION（2026-09-25）e2e 全栈共用会话 ──
// 文件职责：把"整套 e2e 到底登录了几次"收在一处，让 admin / member 各只占一条服务端会话。
//
// 为什么必须有（今天实测炸出来的，不是预防性设计）：internal/auth 每账号最多 8 条会话，
// 超出按 FIFO 淘汰最旧（auth.go 的 maxSessions / issueSession）。而浏览器用例全部依赖
// auth.setup 预先建立的那条 storageState 会话——它是整轮里**最旧**的一条。
// 各 spec 又各自逐用例 `POST /api/auth/login`（uat_full 里 adminToken() 每条用例一次、
// 四个 worker 再各缓存一份），累计登录数一越过 8，storageState 那条就被踢下线：
// 之后凡是"进页面 → 读 localStorage token → 打 admin 端点"的用例连锁红
// （401 → 前端 clearAuth 清掉 token → 读到 null → Playwright 报
// `headers[0].value: expected string, got object`），红成一片却与产品代码毫无关系。
// 今天一次全量跑 100 例红 34，其中 28 例就是这个形状。
//
// 口径：
//   1) 会话文件按账号各一份（.auth/shared/<who>.token），谁先缺谁登录，后续只读文件；
//      于是 admin / member 在全轮各占 1 条会话，距 8 条上限留有足够余量。
//   2) auth.setup 每轮开头 resetSharedStore()：上一轮遗留的 token 可能已被这一轮的登录挤掉
//      或已过期，复用它会把"栈没起来"伪装成"权限坏了"。
//   3) auth.setup 建好浏览器会话后把同一个 token 登记进共享存储（adoptSharedSession）——
//      浏览器用例与 API 用例从此共用同一条会话，不再出现"页面已登录、脚本报 401"。
//   4) 需要"用完即弃"的用例（如 D7 断言 logout 立即失效）必须走 freshToken() 自己登录，
//      绝不许借用共享会话——它吊销的就是全栈的登录态。
// English: one shared session per account for the whole Playwright run; the backend keeps at
// most 8 sessions per account (FIFO eviction), and the storageState session is always the oldest
// one, so every extra login silently evicts it and cascades into unrelated-looking failures.
import fs from 'node:fs'
import path from 'node:path'
import { expect } from '@playwright/test'

// 本文件在 web/e2e/ 下，取其上一级即 web/
const WEB_DIR = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..')
const STORE_DIR = path.join(WEB_DIR, '.auth', 'shared')
const API = process.env.E2E_API || 'http://localhost:18080'

// 两套凭据：admin=超管，member=普通用户（成员权限与 403 断言的宿主账号）。
const CREDS = {
  admin: { u: process.env.E2E_USER || 'admin', p: process.env.E2E_PASS || '' },
  member: { u: process.env.E2E_USER2 || 'tester', p: process.env.E2E_PASS2 || '' },
}

function fileFor(who) {
  return path.join(STORE_DIR, who + '.token')
}

// 删掉整份共享存储（每轮开头调用，见文件头口径 2）。
export function resetSharedStore() {
  fs.rmSync(STORE_DIR, { recursive: true, force: true })
}

function readToken(who) {
  try {
    return fs.readFileSync(fileFor(who), 'utf8').trim()
  } catch {
    return ''
  }
}

// 原子落盘（临时名 + rename）：四个 worker 可能同时补登录，半截文件会让下一个读到的 worker
// 拿到被截断的 token，表现为难以归因的 401。
function writeToken(who, token) {
  fs.mkdirSync(STORE_DIR, { recursive: true, mode: 0o700 })
  const target = fileFor(who)
  const tmp = `${target}.${process.pid}.tmp`
  fs.writeFileSync(tmp, token, { mode: 0o600 })
  fs.renameSync(tmp, target)
}

async function login(request, who) {
  const c = CREDS[who]
  const r = await request.post(API + '/api/auth/login', { data: { username: c.u, password: c.p } })
  const body = await r.json().catch(() => ({}))
  expect(body && body.token, `${c.u} 登录应拿到 token（status=${r.status()}）`).toBeTruthy()
  return body.token
}

// sharedToken：取该账号的全栈会话 token；没有就登录一条并落盘。
// 每次调用都读文件（不在进程内缓存）：多 worker 并发补登录时，后写的那份对大家都可用。
export async function sharedToken(request, who = 'admin') {
  const cached = readToken(who)
  if (cached) return cached
  const t = await login(request, who)
  writeToken(who, t) // 竞态可容忍：重复登录只是多占一条会话，彼此不吊销
  return t
}

// adoptSharedSession：auth.setup 用 UI 登录拿到浏览器会话 token 后登记为共享会话，
// 使 API 侧用例与浏览器用例复用同一条（见文件头口径 3）。
export function adoptSharedSession(who, token) {
  if (token) writeToken(who, token)
}

// freshToken：显式新建一条一次性会话（调用方用完通常立即吊销）。
// 与 sharedToken 的区别就在于"不许拿公共那条去死"。
export async function freshToken(request, who = 'admin') {
  return login(request, who)
}
