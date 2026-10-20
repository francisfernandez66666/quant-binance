// ── 前端单测降级配置 vitest.serial.config.js（§FIX1012-M1 扶正 → §AUDIT1019-M4 迁移）──
// 用途：机器资源不足时默认的 forks 并发 worker 起不来（全部文件
// "Failed to start forks worker" / "Timeout waiting for worker to respond"，tests 0ms），
// 此时 verify_changes.sh 的 vitest_gate 用本配置降级重跑——单线程 + 禁文件并行，
// worker 启动压力最小。断言集与原配置完全一致（同 setupFiles / include），
// 只改执行拓扑，不放宽任何判定；testTimeout 抬到 60s 是给串行+过载留 headroom，
// 真实逻辑缺陷仍会失败，不掩盖问题。
// 触发路径：scripts/verify_changes.sh 的 vitest_gate()（裸 npm test 非零退出且输出含
// worker 启动类错误 + 零用例执行时）——正常机器走 vitest.config.js，永远到不了这里。
//
// §AUDIT1019-M4（2026-10-20）：poolOptions 嵌套形态已随 Vitest 4 的 pool rework **被移除**
// （运行期报 `DEPRECATED: test.poolOptions was removed in Vitest 4`），原写法
// `poolOptions: { threads: { singleThread: true } }` 实际被忽略——降级路径并不是真单线程，
// 在 worker 完全起不来的机器上与默认配置一同失败（第三轮审计 4 配置矩阵实测确认）。
// 故迁移为顶层 `singleThread: true`（Vitest 4 pool rework 后的新位置，语法实测可接受）。
//
// ★ 诚实边界（写给后人，别产生幻觉）：本配置修的是"语法正确 + 意图清晰"，
//   **不能替代机器资源**。2026-10-20 实测：本机 8GB 物理内存、swap 已用 6.77/8GB、
//   空闲仅 ~62MB 时，forks 默认 / threads maxWorkers=2 / 顶层 singleThread / 本配置
//   四种全部 worker 起不来（各 2 文件 120s 超时）。内存有余量时本配置可正常串行跑完
//   （凌晨实测 33 分钟 8/56 文件、0 失败）。判定仍以 vitest_gate 的三条件 + WARN+SKIP 为准。
// English: serial fallback config — same test set as vitest.config.js, only the execution
// topology changes (threads pool, single thread, no file parallelism). Used by
// verify_changes.sh's vitest_gate() when the default forks workers cannot start.
// §AUDIT1019-M4: migrated off the Vitest-4-removed poolOptions nesting to the top-level
// singleThread. Caveat: correct syntax does not create memory — when the box is out of
// memory even this config cannot spawn a worker; vitest_gate's WARN+SKIP remains the arbiter.
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
    singleThread: true,
    fileParallelism: false,
  },
})
