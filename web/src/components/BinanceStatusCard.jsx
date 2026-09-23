// ── 币安链路状态卡 BinanceStatusCard.jsx ──
// 文件职责（PLAN §11.2 Quant 执行路径卡扩）：与 QMT 链路状态卡并列的 Binance 状态双卡之一，
// 展示 GET /api/binance/state 实发契约：controllers（各市场快照/执行器/披露位）、
// reporters（回报三条腿健康度）、feeds（§P3 行情/状态 WS 观测位）。
// fail-soft 约定：端点未上线（404/网络错）不炸页——卡片降级为"未接入"占位并保留重试按钮。
// English: Binance chain-status card beside the QMT one; fail-soft placeholder; consumes the
// real state contract (controllers/reporters/feeds), never renders a fake green light.
import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Card, Button, Tag, MessagePlugin } from 'tdesign-react'
import * as bapi from '../api/binance.js'

const POLL_MS = 60000 // 与实盘持仓兜底轮询同频；回报类事件后续走 SSE 接线

export default function BinanceStatusCard() {
  const [state, setState] = useState(null)     // /api/binance/state 响应（形状见 §6.2 契约）
  const [unavailable, setUnavailable] = useState(false) // 端点未上线/不可达 → 降级占位
  const [cfgLite, setCfgLite] = useState(null) // 状态端点缺席时回落展示的配置摘要
  const [loading, setLoading] = useState(false)
  const timer = useRef(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const st = await bapi.fetchBinanceState()
      setState(st || {})
      setUnavailable(false)
    } catch (_) {
      // 404/网络失败：降级读配置端点（已上线），至少展示 enabled/mode/testnet/halted
      setState(null)
      setUnavailable(true)
      try {
        const c = await bapi.fetchBinanceConfig()
        setCfgLite(c || null)
      } catch (_2) { setCfgLite(null) }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    timer.current = setInterval(load, POLL_MS)
    return () => { if (timer.current) clearInterval(timer.current) }
  }, [load])

  // kill-switch：与 QMT 卡同惯例——按钮直发，后端广播 SSE 告警回显
  async function halt(next) {
    try {
      await bapi.binanceHalt(next)
      MessagePlugin[next ? 'warning' : 'info'](next ? '币安熔断已置位（撤在途+拒新单）' : '币安熔断已解除')
      load()
    } catch (e) {
      MessagePlugin.error('熔断操作失败：' + (e && e.message ? e.message : '端点未上线'))
    }
  }

  const info = state || cfgLite || {}
  const enabled = info.enabled ?? false
  const halted = info.halted ?? false
  // §P3 契约对齐：/api/binance/state 实发形状是 controllers（按市场：snapshot+executor+披露位）
  // /reporters（回报三条腿健康度）/feeds（行情与状态 WS 观测位）——旧 §6.2 设想形状
  // （connected/ws_subscriptions/rate_limit_remaining）从未进后端，这里按真实键消费，
  // 缺席键一律降级为"未接入"占位，绝不渲染假绿灯。
  const ctl = (state && state.controllers) || {}
  const reps = (state && state.reporters) || {}
  const feeds = (state && state.feeds) || []
  const mkts = ['US', 'CRYPTO'].filter((m) => ctl[m])
  const wired = mkts.length > 0

  const row = (k, v) => (
    <div style={{ display: 'flex', gap: 8, fontSize: 13, padding: '3px 0' }}>
      <span style={{ width: 120, color: 'var(--app-muted-2)', flexShrink: 0 }}>{k}</span>
      <span>{v}</span>
    </div>
  )

  return (
    <Card title="币安接入状态（美股 Stocks + 加密现货）" style={{ marginBottom: 14 }}
      headerExtra={<Button size="xs" variant="outline" loading={loading} onClick={load}>刷新</Button>}>
      {row('总开关', <Tag theme={enabled ? 'success' : 'default'} size="small">{enabled ? '已启用' : '未启用'}</Tag>)}
      {row('模式/环境', `${info.mode || '-'} · ${info.testnet ? 'testnet' : 'prod'}`)}
      {row('链路', state
        ? <Tag theme={wired ? 'success' : 'warning'} size="small">{wired ? `已接入 ${mkts.length}/2 市场` : '通道未装配'}</Tag>
        : <Tag theme="warning" size="small">状态端点未上线（/api/binance/state）</Tag>)}
      {mkts.map((m) => row(m === 'US' ? '美股通道' : '加密通道', (() => {
        const c = ctl[m] || {}
        const snap = c.snapshot || {}
        if (!c.executor) return 'Noop（凭证缺失，记账不真下）'
        if (snap.tripped) return `熔断中（${snap.trip_reason || '原因未记录'}）`
        return snap.last_probe_ok ? '探测正常 ●' : '探测未成 ○'
      })()))}
      {mkts.map((m) => reps[m] && row(`${m} 回报腿`, reps[m].ws ? (reps[m].ws_healthy ? 'WS 健康 ●' : 'WS 静默/断线 ○') : 'REST 轮询（无 WS 腿）'))}
      {feeds.length > 0 && row('WS feed', feeds.map((f) => (
        <span key={f.name} style={{ marginRight: 10 }}>
          {f.name}:{f.healthy ? '●' : '○'}
          {f.silence_ms >= 0 ? ` ${Math.round(f.silence_ms / 1000)}s` : ''}
          {(f.unconfirmed || []).length ? ` 未确认${f.unconfirmed.length}` : ''}
        </span>
      )))}
      {row('披露签署', info.disclaimer_signed_at ? info.disclaimer_signed_at : '未签署')}
      {row('紧急停止', (
        <>
          {/* 文案用「未熔断/熔断中」而非「正常」——避免与 QMT 状态卡的既有「正常」文本
              在测试与页内二义性选择器（getByText）上相撞（§BINANCE-P4 零回归约束）。 */}
          <Tag theme={halted ? 'danger' : 'success'} size="small">{halted ? '⛔ 熔断中' : '未熔断'}</Tag>{' '}
          <Button size="xs" theme={halted ? 'primary' : 'danger'} variant="outline"
            onClick={() => halt(!halted)}>{halted ? '解除' : '置位'}</Button>
        </>
      ))}
      {unavailable && (
        <div style={{ fontSize: 12, color: 'var(--app-muted)', marginTop: 6 }}>
          运行时端点尚未接入：当前展示配置摘要（/api/config/binance）。委托/撤单（/api/binance/orders、
          /api/binance/cancel/&#123;id&#125;）上线后此卡自动升级。
        </div>
      )}
    </Card>
  )
}
