// ── 统一日志出口 web/src/utils/logger.js ──
// §FIX1012-M10：收敛生产代码里散落的 console.error/warn。
// 背景：原先 Research.jsx 等 god component 直接 console.*，格式不统一、无级别开关、
// 无生产环境收敛——一旦某处把调试 console 留在生产分支，线上控制台会刷屏且无从降级。
// 收敛到本模块后：① 统一 [模块] 前缀，便于按模块检索；② 保留 error/warn 的异常分支语义
// （首个参数固定为模块标签，其余参数原样透传，含 Error 实例，不丢 stack）；
// ③ 经 VITE_LOG_LEVEL 可在生产环境降级（默认开发全开 'debug'；生产设 'warn' 则只留 warn+error）。
// English: single log sink — replaces scattered console.* in production code with a runtime level gate.
//
// 设计取舍：本模块内部仍用 console.* 输出（浏览器没有更底层的日志原语），但不启用 no-console
// 规则，故 lint 不会因本文件报错；对外暴露的 logger.* 才是生产代码应该调用的唯一出口。
// 这样「生产代码不得直接 console.*」的纪律由 verify_changes.sh §FIX1012-M10 静态锁兜底。

const LEVELS = { debug: 0, info: 1, warn: 2, error: 3 }

// 运行期级别：优先 Vite 注入的 import.meta.env.VITE_LOG_LEVEL，其次 Node 的 process.env，
// 两者皆无则回落 'debug'（开发环境全开）。生产构建若未注入该变量，仍保持全开——
// 这是保守选择：宁可多打也不丢错误分支；需要收敛时显式设 VITE_LOG_LEVEL=warn 即可。
function readLevel() {
  if (typeof import.meta !== 'undefined' && import.meta.env && import.meta.env.VITE_LOG_LEVEL) {
    return import.meta.env.VITE_LOG_LEVEL
  }
  if (typeof process !== 'undefined' && process.env && process.env.VITE_LOG_LEVEL) {
    return process.env.VITE_LOG_LEVEL
  }
  return 'debug'
}

// 当前级别是否允许输出 level。level 数值 ≥ 最低级别阈值才放行。
function shouldLog(level) {
  const min = LEVELS[readLevel()] ?? LEVELS.debug
  return LEVELS[level] >= min
}

// 实际落盘：拼接 [prefix] 标签后透传给对应级别的 console 方法。
// 用 console[level] 动态派发，避免重复写四遍分支；console.debug/info/warn/error 在浏览器均存在。
function sink(level, prefix, args) {
  if (!shouldLog(level)) return
  const tag = prefix ? `[${prefix}]` : ''
  console[level](tag, ...args)
}

export const logger = {
  error: (prefix, ...args) => sink('error', prefix, args),
  warn: (prefix, ...args) => sink('warn', prefix, args),
  info: (prefix, ...args) => sink('info', prefix, args),
  debug: (prefix, ...args) => sink('debug', prefix, args),
}
