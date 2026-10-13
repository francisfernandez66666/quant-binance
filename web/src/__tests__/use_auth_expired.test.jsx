// ── §H7 / §FIX1012-M5 auth:expired 监听 hook 级回归用例 ──
// 断言目标从 App 壳下沉到 useAuthExpired hook（ref 镜像 + 转发壳）：不 import App、
// 不 import tdesign，全部用例 <2s。原 App 壳集成锚保留在 auth_expired_h7.test.jsx
// （慢用例，只进全量 npm test，不进 §H6/§H7 门禁段的 targeted 过滤）。
// English: §H7 regression at hook level — the once-registered auth:expired listener must
// observe the latest loggedIn state and the latest closure, without importing the App shell.
import { describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAuthExpired } from '../hooks/useAuthExpired.js'

function fireExpired() {
  act(() => { window.dispatchEvent(new Event('auth:expired')) })
}

describe('useAuthExpired - auth:expired 读最新登录态（§H7 / §FIX1012-M5）', () => {
  it('未登录态收到 expired 事件：静默跳过，不调用 onExpired', () => {
    const onExpired = vi.fn()
    renderHook(() => useAuthExpired(false, onExpired))
    fireExpired()
    expect(onExpired).not.toHaveBeenCalled()
  })

  it('登录后（rerender 置真）收到 expired 事件：真正执行登出回调', () => {
    const onExpired = vi.fn()
    const { rerender } = renderHook(
      ({ li }) => useAuthExpired(li, onExpired),
      { initialProps: { li: false } },
    )
    rerender({ li: true }) // 登录成功：loggedIn false→true
    fireExpired()
    expect(onExpired).toHaveBeenCalledTimes(1)
  })

  it('回调闭包读到的 loggedInRef.current 是最新值（旧首帧 false 冻结→必红）', () => {
    const seen = []
    const { rerender, result } = renderHook(
      ({ li }) => useAuthExpired(li, () => seen.push(result.current.loggedInRef.current)),
      { initialProps: { li: false } },
    )
    rerender({ li: true })
    fireExpired()
    expect(seen).toEqual([true])
  })

  it('转发壳执行的是最新闭包（rerender 换回调后旧回调不再被调用）', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = renderHook(
      ({ cb }) => useAuthExpired(true, cb),
      { initialProps: { cb: first } },
    )
    rerender({ cb: second })
    fireExpired()
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })

  it('卸载后监听器注销：再派发事件不调用回调', () => {
    const onExpired = vi.fn()
    const { unmount } = renderHook(() => useAuthExpired(true, onExpired))
    unmount()
    fireExpired()
    expect(onExpired).not.toHaveBeenCalled()
  })
})
