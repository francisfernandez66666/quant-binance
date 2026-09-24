// ── §F6 Playwright 登录态预置（setup project）──
// 全栈仅登录一次，把 localStorage(liangzai_token 等) 存为 storageState 供各用例复用，
// 规避后端匿名端点 IP 频控（login 5/分钟）——每个用例各自登录会在 6 连跑时触发 429 假失败。
// §UAT-SESSION（2026-09-25）：这里登录出的 token 同时登记为 admin 的"全栈共享会话"，
// 并且每次跑先清空共享存储——后端每账号只留 8 条会话（FIFO 淘汰最旧），而本会话是整轮里
// 最旧的一条；各 spec 自行逐用例登录会把它挤下线，表现为一大片与产品无关的权限红。
// English: log in ONCE, persist localStorage as storageState AND register it as the run-wide
// shared admin session; the backend keeps only 8 sessions per account (FIFO), so any extra
// per-test login silently evicts this one and cascades into unrelated failures.
import { test, expect } from '@playwright/test'
import { resetSharedStore, adoptSharedSession } from './session.mjs'

const USER = process.env.E2E_USER || ''
const PASS = process.env.E2E_PASS || ''

test('预置登录态 storageState', async ({ page }) => {
  resetSharedStore() // 上一轮的 token 可能已被淘汰/过期，复用会把故障伪装成权限缺陷
  await page.goto('/#/')
  // 已是登录态（复用旧 state）则先登出，确保本次用真实凭据建立干净的会话
  const acct = page.getByPlaceholder('输入账号')
  await acct.waitFor({ timeout: 15000 })
  await acct.fill(USER)
  await page.getByPlaceholder('输入密码').fill(PASS)
  await page.getByPlaceholder('输入密码').press('Enter')
  await expect(page.locator('.app-shell')).toBeVisible({ timeout: 15000 })
  // 把浏览器这条会话登记为共享会话：API 用例不再另登录
  const tok = await page.evaluate(() => localStorage.getItem('liangzai_token'))
  expect(tok, '登录后 localStorage 应有 liangzai_token（否则共享会话无从登记）').toBeTruthy()
  adoptSharedSession('admin', tok)
  await page.context().storageState({ path: '.auth/state.json' })
})
