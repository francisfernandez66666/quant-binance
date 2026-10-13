// ── §H7（2026-09-22 修复批）auth:expired 闭包死亡 · App 壳集成锚 ──
// §FIX1012-M5：断言目标（ref 镜像 + 转发壳）已下沉 useAuthExpired hook，hook 级用例在
// use_auth_expired.test.jsx（<2s，§H6/§H7 门禁段 targeted 过滤跑它）。本文件只保留
// **1 条 App 壳集成用例**作端到端锚：证明 App.jsx 真的把 hook 接上了（删掉接线时
// hook 单测仍绿、功能已死——这类回归只有集成锚能抓到）。
// 注意：本用例 import 整个 App + tdesign 链，高负载下导入 >60s（2026-10-12 实测单文件
// 226s），故 timeout 放到 120s 且**只进全量 npm test，不进门禁 targeted 过滤**。
// English: §H7 App-shell integration anchor — proves App.jsx actually wires useAuthExpired.
// Heavy (imports the whole App + tdesign chain); runs in the full suite only, not in the
// targeted §H6/§H7 gate filter. Hook-level assertions live in use_auth_expired.test.jsx.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import React from 'react'
import { render, screen, waitFor, act, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// 每个用例独立控制 isLoggedIn 的返回值（vi.doMock 工厂在动态 import 时才求值，闭包安全）
let loggedFlag = true

// §H7 用例共用 API mock 基座：默认全接口成功；loggedFlag 控制登录态。
function apiMockBase() {
  return {
    isLoggedIn: () => loggedFlag,
    getAccount: () => 'alice',
    getStoredServer: () => '',
    setStoredServer: () => {},
    hasPerm: () => false,
    isAdmin: () => false,
    refreshMe: vi.fn(async () => ({ role: 'user', perms: [] })),
    login: vi.fn(async () => ({})),
    logout: vi.fn(),
    // §M-11（2026-09-22 修复批）App.checkAuth 恢复登录态后会补报推送账号（api.syncPushAccount），
    // mock 基座必须覆盖该导出，否则属性访问即抛「No export defined on mock」。
    syncPushAccount: vi.fn(),
    fetchStatus: vi.fn(async () => ({ signal_count: 0, in_trade_time: false, active: false })),
    fetchAlerts: vi.fn(async () => []),
    fetchShortStatus: vi.fn(async () => ({ short_enabled: false })),
    fetchPaperState: vi.fn(async () => ({ enabled: false })),
    toggleShort: vi.fn(async () => ({ short_enabled: false })),
    connectSSE: vi.fn(async () => {}),
    disconnectSSE: vi.fn(),
    onSSE: vi.fn(() => () => {}),
  }
}

async function mountApp() {
  vi.resetModules()
  const api = apiMockBase()
  vi.doMock('../api/index.js', () => api)
  // 首屏页 Dashboard 与全部业务接口解耦掉：本用例只锁 App 壳层的 expired 行为
  vi.doMock('../pages/Dashboard.jsx', () => ({
    default: () => <div data-testid="dashboard-stub">仪表盘</div>,
  }))
  const { default: App } = await import('../App.jsx')
  const utils = render(
    <MemoryRouter initialEntries={['/dashboard']}>
      <App />
    </MemoryRouter>
  )
  return { api, utils }
}

describe('App 壳 - auth:expired 接线集成锚（§H7 / §FIX1012-M5，慢用例只进全量）', () => {
  beforeEach(() => {
    localStorage.clear()
    loggedFlag = true
  })
  // §H7 收尾核验加固：用例超时会跳过默认清理，残留首挂载体让下一条用例的 getByText
  // 命中「multiple elements」级联红。显式 cleanup 斩断。
  afterEach(() => { cleanup() })

  it('loggedIn false→true 后收到 expired 事件：App 接线生效，执行登出并回到登录页', { timeout: 120000 }, async () => {
    const { api } = await mountApp()
    // checkAuth 异步恢复登录态 → 主布局渲染（顶栏「退出」按钮出现）
    await waitFor(() => expect(screen.getByText('退出')).toBeInTheDocument())
    expect(api.connectSSE).toHaveBeenCalled() // 登录态链路已拉起

    // 模拟 request() 401 后广播的全局过期事件
    await act(async () => { window.dispatchEvent(new Event('auth:expired')) })

    // §H7 断言：事件必须真正走到登出（旧实现首帧 loggedIn=false 早退，永远不会调 logout）
    expect(api.logout).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(screen.getByText('量化交易辅助工具')).toBeInTheDocument())
  })
})
