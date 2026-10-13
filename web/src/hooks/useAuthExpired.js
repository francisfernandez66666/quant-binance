import { useEffect, useRef } from 'react'

// §H7 / §FIX1012-M5：auth:expired 监听抽成独立 hook。
// 原实现住在 App.jsx 里（loggedInRef 值镜像 + authExpiredHandler 闭包镜像 + 挂载转发壳），
// 测它必须 import 整个 App.jsx + tdesign 链——单文件在高负载下 >60s（2026-10-12 实测 226s），
// 让本该是单元测试的用例退化成重量级集成测试，还把 §H6/§H7 门禁段拖脆。
// 下沉后行为不变（监听器挂载只注册一次，执行时经 ref 取最新登录态/最新闭包，绕开首帧
// 陈旧闭包），但可用 renderHook 直接测，无 App/tdesign 依赖。
// English: auth:expired listener extracted from App.jsx so it can be unit-tested without the
// App shell. Register-once + forward-through-refs keeps the latest loggedIn/closure, avoiding
// the stale first-frame closure that swallowed the logout prompt (§H7).
export function useAuthExpired(loggedIn, onExpired) {
  const loggedInRef = useRef(loggedIn)
  const handlerRef = useRef(onExpired)
  // 每个 commit 后把最新值/最新闭包镜像进 ref（无 deps）
  useEffect(() => {
    loggedInRef.current = loggedIn
    handlerRef.current = onExpired
  })
  // 挂载只注册一次，执行时经 ref 取最新闭包，绕开首帧陈旧闭包；卸载按同一壳引用注销
  useEffect(() => {
    const forward = () => {
      if (handlerRef.current) handlerRef.current()
    }
    window.addEventListener('auth:expired', forward)
    return () => window.removeEventListener('auth:expired', forward)
  }, [])
  return { loggedInRef }
}
