// ── 前端单测降级配置 vitest.serial.config.js（§FIX1012-M1 扶正）──
// 用途：机器负载极高时默认的 forks 并发 worker 起不来（全部文件
// "Failed to start forks worker" / "Timeout waiting for worker to respond"，tests 0ms），
// 此时 verify_changes.sh 的 vitest_gate 用本配置降级重跑——单线程 + 禁文件并行，
// worker 启动压力最小。断言集与原配置完全一致（同 setupFiles / include），
// 只改执行拓扑，不放宽任何判定；testTimeout 抬到 60s 是给串行+过载留 headroom，
// 真实逻辑缺陷仍会失败，不掩盖问题。
// 触发路径：scripts/verify_changes.sh 的 vitest_gate()（裸 npm test 非零退出且输出含
// worker 启动类错误 + 零用例执行时）——正常机器走 vitest.config.js，永远到不了这里。
// English: serial fallback config — same test set as vitest.config.js, only the execution
// topology changes (threads pool, single thread, no file parallelism). Used by
// verify_changes.sh's vitest_gate() when the default forks workers cannot start under load.
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/__tests__/setup.js',
    testTimeout: 60000,
    hookTimeout: 60000,
    include: ['src/__tests__/**/*.test.{js,jsx}'],
    pool: 'threads',
    poolOptions: { threads: { singleThread: true } },
    fileParallelism: false,
  },
})
