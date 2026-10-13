// ── §F6 Playwright 冒烟测试配置 ──
// 默认复用已运行的前端（vite dev 或生产 preview 或广州部署实例），不做 webServer 自启，避免本地/CI 端口冲突。
// §FIX1012-M4：E2E_AUTOSTART=1 时改为自起 vite dev（端口取自 E2E_BASE_URL，默认 5173），
// 且带 reuseExistingServer:true——已在跑的栈不会被重启/抢占，端口冲突风险由"先探再起"消掉。
// 注意：autostart 只负责前端 dev server；后端与 mock 仍需 bash scripts/uat_bootstrap.sh up 起好。
// baseURL 走环境变量 E2E_BASE_URL，默认 http://localhost:5173（vite dev 端口）。仅跑 chromium，
// 关闭动画截图、失败重试 1 次、无头模式（CI）/有头（本地 PW_HEADED=1）。
// 首次需一次性下载浏览器内核：npx playwright install chromium
// English: §F6 Playwright smoke config — attaches to an already-running frontend by default (no
// self-managed webServer to avoid port clashes in local/CI). With E2E_AUTOSTART=1 it self-starts
// vite dev on the E2E_BASE_URL port, reusing an existing server when one is already listening.
// baseURL from E2E_BASE_URL (default vite dev 5173). Chromium only, headless by default
// (PW_HEADED=1 for headed), 1 retry. One-time `npx playwright install chromium`.
import { defineConfig, devices } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:5173'
const AUTOSTART = !!process.env.E2E_AUTOSTART
const BASE_PORT = new URL(BASE).port || '5173'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL: BASE,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    headless: !process.env.PW_HEADED,
  },
  // §FIX1012-M4：默认无 webServer（保持"复用已运行前端"的原设计）；E2E_AUTOSTART=1 才自起。
  // --strictPort：端口被非 HTTP 服务占用时立刻明确报错，而不是静默换端口让 Playwright 超时。
  ...(AUTOSTART ? {
    webServer: {
      command: `npm run dev -- --port ${BASE_PORT} --strictPort`,
      url: BASE,
      reuseExistingServer: true,
      timeout: 120_000,
      stdout: 'pipe',
      stderr: 'pipe',
    },
  } : {}),
  projects: [
    // setup：仅登录一次并落 storageState，规避后端 10/min 匿名登录频控（避免多用例各自登录触发 429 假失败）
    { name: 'setup', testMatch: /auth\.setup\.mjs/ },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], storageState: '.auth/state.json' },
      dependencies: ['setup'],
    },
  ],
})
