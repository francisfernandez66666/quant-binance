// ── 币安配置表单 BinanceConfigPanel.jsx ──
// 文件职责（PLAN §11.2 Settings / §4.2 BinanceConfig）：设置页的 Binance 配置卡片，
// 镜像 QMT 配置表单惯例读写 GET|POST /api/config/binance（internal/server/binance_config.go）：
//   · api_key/api_secret 掩码回显——GET 只回 *_masked 与 has_api_key，输入框留占位提示；
//     用户不改则不提交该键（后端指针字段 nil=保持原值，脱敏哨兵/空串也不会覆盖真值，双保险）；
//   · stock/spot/risk_gate 为整档案替换——保存时总是回传完整三段（未编辑字段用 GET 原值
//     合并，避免 strategies/blacklist 等本表单不展示的键被清零）；
//   · 校验单一权威在后端 config.ValidateBinance：400 的 msg 直接展示，不在前端复写规则。
// English: Binance settings form mirroring the QMT form conventions — masked credential echo,
// whole-profile round-trip for stock/spot/risk_gate, server-side validation as single authority.
import React, { useEffect, useRef, useState } from 'react'
import { Card, Input, InputNumber, Button, Tag, MessagePlugin } from 'tdesign-react'
import ToggleSw from './ToggleSw.jsx'
import * as bapi from '../api/binance.js'

const rowStyle = { display: 'flex', alignItems: 'center', gap: 12, padding: '6px 0', flexWrap: 'wrap' }
const labelStyle = { width: 170, flexShrink: 0, color: 'var(--app-muted-2)', fontSize: 13 }
const inlineLabelStyle = { fontSize: 12, color: 'var(--app-muted-2)', marginLeft: 8 }
const subTitleStyle = { fontWeight: 700, fontSize: 13, margin: '10px 0 2px' }

// 表单初值空壳：GET 回来后整对象覆盖；提交前用 baseRef（服务端原值）做合并底座
const emptyForm = () => ({
  enabled: false, data_plane: false, mode: 'manual', testnet: true, quote_asset: 'USDC',
  timeout_sec: 10, miss_heartbeat_sec: 120, cancel_stale_sec: 120, paper_separate: true,
  halted: false, disclaimer_signed_at: '',
  api_key: '', api_secret: '',
})

export default function BinanceConfigPanel() {
  const [form, setForm] = useState(emptyForm())
  const [masked, setMasked] = useState({ api_key_masked: '', api_secret_masked: '' })
  const [hasKey, setHasKey] = useState({ key: false, secret: false })
  // baseRef：GET 下发的完整基线（stock/spot/risk_gate 原档案），保存时在其上叠加表单编辑
  const baseRef = useRef(null)
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)

  // 装载：拉 /api/config/binance 全量档案入 baseRef；dead 标志防卸载后 setState（组件卸载竞态）
  useEffect(() => {
    let dead = false
    bapi.fetchBinanceConfig().then((c) => {
      if (dead || !c) return
      baseRef.current = c
      setForm({
        ...emptyForm(),
        enabled: !!c.enabled, data_plane: !!c.data_plane, mode: c.mode || 'manual', testnet: !!c.testnet,
        quote_asset: c.quote_asset || 'USDC',
        timeout_sec: c.timeout_sec ?? 10, miss_heartbeat_sec: c.miss_heartbeat_sec ?? 120,
        cancel_stale_sec: c.cancel_stale_sec ?? 120, paper_separate: !!c.paper_separate,
        halted: !!c.halted, disclaimer_signed_at: c.disclaimer_signed_at || '',
      })
      setMasked({ api_key_masked: c.api_key_masked || '', api_secret_masked: c.api_secret_masked || '' })
      setHasKey({ key: !!c.has_api_key, secret: !!c.api_secret_masked })
      setLoaded(true)
    }).catch(() => { if (!dead) MessagePlugin.warning('币安配置读取失败：后端 /api/config/binance 不可达') })
    return () => { dead = true }
  }, [])

  const set = (k, v) => setForm((f) => ({ ...f, [k]: v }))

  // 子档案编辑：只覆盖表单展示的键，其余键（strategies/blacklist…）从基线原样带回；
  // __edited 哨兵标记该段已被本表单接管（提交前剥除）
  function patchProfile(which, fields) {
    setForm((f) => {
      const base = (baseRef.current && baseRef.current[which]) || {}
      const cur = f[which] ? f[which] : { ...base }
      return { ...f, [which]: { ...cur, ...fields, __edited: true } }
    })
  }
  // 读取段值：表单已接管用接管值，否则回落 GET 基线
  const prof = (which) => form[which] || (baseRef.current && baseRef.current[which]) || {}

  async function save() {
    setSaving(true)
    try {
      const body = {
        enabled: form.enabled, data_plane: form.data_plane, mode: form.mode, testnet: form.testnet,
        timeout_sec: form.timeout_sec, miss_heartbeat_sec: form.miss_heartbeat_sec,
        cancel_stale_sec: form.cancel_stale_sec, quote_asset: form.quote_asset,
        paper_separate: form.paper_separate,
      }
      // 凭证：仅在用户真的输入了新值时提交（留空=保持，后端也有掩码哨兵兜底）
      if (form.api_key.trim()) body.api_key = form.api_key.trim()
      if (form.api_secret.trim()) body.api_secret = form.api_secret.trim()
      // 整档案替换语义：剥掉 __edited 哨兵后全量回传
      const strip = (p) => { const { __edited, ...rest } = p || {}; return rest }
      body.stock = strip(prof('stock'))
      body.spot = strip(prof('spot'))
      body.risk_gate = strip(prof('risk_gate'))
      const res = await bapi.updateBinanceConfig(body)
      if (res) {
        baseRef.current = res
        // 保存成功后凭证输入框清空、回显换新一批掩码
        setForm((f) => ({ ...f, api_key: '', api_secret: '', stock: undefined, spot: undefined, risk_gate: undefined, halted: !!res.halted }))
        setMasked({ api_key_masked: res.api_key_masked || '', api_secret_masked: res.api_secret_masked || '' })
        setHasKey({ key: !!res.has_api_key, secret: !!res.api_secret_masked })
      }
      MessagePlugin.success('币安配置已保存')
    } catch (e) {
      // 后端 ValidateBinance 的 400 msg 即校验单一权威，直接透传展示
      MessagePlugin.error('保存失败：' + (e && e.message ? e.message : '配置校验未通过'))
    } finally {
      setSaving(false)
    }
  }

  // kill-switch：halted 即时生效（不入待生效队列），与保存分离
  async function toggleHalt(next) {
    try {
      await bapi.updateBinanceConfig({ halted: next })
      set('halted', next)
      MessagePlugin[next ? 'warning' : 'info'](next ? '币安紧急停止已置位：新委托将被拒' : '币安紧急停止已解除')
    } catch (e) {
      MessagePlugin.error('操作失败：' + (e && e.message ? e.message : 'halted 设置未生效'))
    }
  }

  const stock = prof('stock')
  const spot = prof('spot')
  const gate = prof('risk_gate')
  const quoteSymbolsText = Array.isArray(spot.quote_symbols) ? spot.quote_symbols.join(',') : (spot.quote_symbols || '')

  return (
    <Card title="币安接入（美股 Stocks + 加密货币现货）" style={{ marginBottom: 14 }}>
      {!loaded && <div style={{ fontSize: 12, color: 'var(--app-muted)' }}>读取 /api/config/binance …</div>}
      {loaded && (
        <>
          <div style={rowStyle}>
            <span style={labelStyle}>总开关 enabled</span>
            <ToggleSw checked={!!form.enabled} onChange={(v) => set('enabled', v)} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>缺省关：关闭时不装配币安 Controller，CN 链路不受影响</span>
            {form.halted && <Tag theme="danger">⛔ 紧急停止中</Tag>}
            <Button size="xs" theme={form.halted ? 'default' : 'danger'} variant="outline"
              onClick={() => toggleHalt(!form.halted)}>{form.halted ? '解除熔断' : '置位紧急停止'}</Button>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>数据面 data_plane</span>
            <ToggleSw checked={!!form.data_plane} onChange={(v) => set('data_plane', v)} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>§MR-1 无凭证即可装配行情/K线/情绪/事件观测链；结构上不能下单（真交易只认 enabled）。改开关需重启引擎重新装配 Controller</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>运行模式 mode</span>
            {['manual', 'auto'].map((m) => (
              <Button key={m} size="xs" variant={form.mode === m ? 'base' : 'outline'}
                theme={form.mode === m ? 'primary' : 'default'} onClick={() => set('mode', m)}>
                {m === 'manual' ? 'manual 手动确认' : 'auto 自动下单'}
              </Button>
            ))}
            <span style={inlineLabelStyle}>testnet</span>
            <ToggleSw checked={!!form.testnet} onChange={(v) => set('testnet', v)} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>testnet.binance.vision（仅现货有意义）</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>API Key</span>
            <Input style={{ width: 280 }} value={form.api_key} onChange={(v) => set('api_key', v)}
              placeholder={hasKey.key ? `已配置 ${masked.api_key_masked}（留空保持不变）` : '输入 API Key'} />
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>API Secret</span>
            <Input style={{ width: 280 }} type="password" value={form.api_secret} onChange={(v) => set('api_secret', v)}
              placeholder={hasKey.secret ? `已配置 ${masked.api_secret_masked}（留空保持不变）` : '输入 API Secret'} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>回显恒为掩码；建议 Key 只勾 Spot/Stocks 交易权限、开 IP 白名单</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>REST 超时 timeout_sec</span>
            <InputNumber value={form.timeout_sec} onChange={(v) => set('timeout_sec', v)} min={1} max={120} theme="column" style={{ width: 120 }} />
            <span style={inlineLabelStyle}>心跳缺失 miss_heartbeat_sec</span>
            <InputNumber value={form.miss_heartbeat_sec} onChange={(v) => set('miss_heartbeat_sec', v)} min={10} max={3600} theme="column" style={{ width: 120 }} />
            <span style={inlineLabelStyle}>在途超时撤 cancel_stale_sec（-1=关）</span>
            <InputNumber value={form.cancel_stale_sec} onChange={(v) => set('cancel_stale_sec', v)} min={-1} max={3600} theme="column" style={{ width: 120 }} />
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>美股计价币 quote_asset</span>
            <Input style={{ width: 140 }} value={form.quote_asset} onChange={(v) => set('quote_asset', v)} placeholder="USDC" />
            <span style={inlineLabelStyle}>独立纸面资金池 paper_separate</span>
            <ToggleSw checked={!!form.paper_separate} onChange={(v) => set('paper_separate', v)} />
            <span style={inlineLabelStyle}>美股披露已签</span>
            <span style={{ fontSize: 12 }}>{form.disclaimer_signed_at ? form.disclaimer_signed_at : '未签署（启动探测回填）'}</span>
          </div>

          <div style={subTitleStyle}>美股子市场 stock（Binance Stocks /sapi/v1/equity）</div>
          <div style={rowStyle}>
            <span style={labelStyle}>子开关 enabled</span>
            <ToggleSw checked={!!stock.enabled} onChange={(v) => patchProfile('stock', { enabled: v })} />
            <span style={inlineLabelStyle}>时段 trading_session</span>
            {['RTH', 'EXTENDED', '24H'].map((s) => (
              <Button key={s} size="xs" variant={stock.trading_session === s ? 'base' : 'outline'}
                theme={stock.trading_session === s ? 'primary' : 'default'}
                onClick={() => patchProfile('stock', { trading_session: s })}>{s}</Button>
            ))}
            <span style={inlineLabelStyle}>有效期 time_in_force</span>
            {['DAY', 'GTC'].map((t) => (
              <Button key={t} size="xs" variant={stock.time_in_force === t ? 'base' : 'outline'}
                theme={stock.time_in_force === t ? 'primary' : 'default'}
                onClick={() => patchProfile('stock', { time_in_force: t })}>{t}</Button>
            ))}
          </div>
          <ProfileDiscipline profName="stock" p={stock} patch={patchProfile} moneyHint="USD" />

          <div style={subTitleStyle}>加密子市场 spot（Binance Spot 现货 7×24）</div>
          <div style={rowStyle}>
            <span style={labelStyle}>子开关 enabled</span>
            <ToggleSw checked={!!spot.enabled} onChange={(v) => patchProfile('spot', { enabled: v })} />
            <span style={inlineLabelStyle}>监控币对 quote_symbols（逗号分隔）</span>
            <Input style={{ width: 320 }} defaultValue={quoteSymbolsText}
              onChange={(v) => patchProfile('spot', { quote_symbols: String(v).split(',').map((x) => x.trim()).filter(Boolean) })}
              placeholder="BTCUSDT,ETHUSDT" />
          </div>
          <ProfileDiscipline profName="spot" p={spot} patch={patchProfile} moneyHint="USDT" />

          <div style={subTitleStyle}>风控闸 risk_gate（新市场阈值独立于 A 股档）</div>
          <div style={rowStyle}>
            <span style={labelStyle}>行情陈旧硬闸 stale_quote_ms</span>
            <InputNumber value={gate.stale_quote_ms ?? 0} onChange={(v) => patchProfile('risk_gate', { stale_quote_ms: v })} min={0} theme="column" style={{ width: 130 }} />
            <span style={inlineLabelStyle}>日内亏损熔断 %（0=关）</span>
            <InputNumber value={gate.day_loss_limit_pct ?? 0} onChange={(v) => patchProfile('risk_gate', { day_loss_limit_pct: v })} min={0} step={0.5} theme="column" style={{ width: 110 }} />
            <span style={inlineLabelStyle}>单票集中度上限 %</span>
            <InputNumber value={gate.single_stock_value_pct ?? 0} onChange={(v) => patchProfile('risk_gate', { single_stock_value_pct: v })} min={0} max={100} theme="column" style={{ width: 110 }} />
            <span style={inlineLabelStyle}>单笔委托金额帽</span>
            <InputNumber value={gate.max_order_amount ?? 0} onChange={(v) => patchProfile('risk_gate', { max_order_amount: v })} min={0} theme="column" style={{ width: 130 }} />
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 12 }}>
            <Button theme="primary" loading={saving} onClick={save}>保存币安配置</Button>
          </div>
        </>
      )}
    </Card>
  )
}

// 单市场纪律参数行（fixed_amount/max_positions/daily_max_buys/daily_budget_amount）——
// stock/spot 形状相同（BinanceMarketProfile），抽成一个渲染块避免两套字段漂移。
function ProfileDiscipline({ profName, p, patch, moneyHint }) {
  return (
    <div style={rowStyle}>
      <span style={labelStyle}>单笔固定金额 fixed_amount</span>
      <InputNumber value={p.fixed_amount ?? 0} onChange={(v) => patch(profName, { fixed_amount: v })} min={0} theme="column" style={{ width: 130 }} />
      <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>{moneyHint}</span>
      <span style={inlineLabelStyle}>最大持仓 max_positions</span>
      <InputNumber value={p.max_positions ?? 0} onChange={(v) => patch(profName, { max_positions: v })} min={0} theme="column" style={{ width: 110 }} />
      <span style={inlineLabelStyle}>日买笔数上限</span>
      <InputNumber value={p.daily_max_buys ?? 0} onChange={(v) => patch(profName, { daily_max_buys: v })} min={0} theme="column" style={{ width: 110 }} />
      <span style={inlineLabelStyle}>日预算 daily_budget_amount</span>
      <InputNumber value={p.daily_budget_amount ?? 0} onChange={(v) => patch(profName, { daily_budget_amount: v })} min={0} theme="column" style={{ width: 130 }} />
    </div>
  )
}
