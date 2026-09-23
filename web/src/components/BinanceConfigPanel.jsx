// ── 币安配置表单 BinanceConfigPanel.jsx ──
// 文件职责（PLAN §11.2 Settings / §4.2 BinanceConfig）：设置页的 Binance 配置卡片，
// 镜像 QMT 配置表单惯例读写 GET|POST /api/config/binance（internal/server/binance_config.go）：
//   · api_key/api_secret 掩码回显——GET 只回 *_masked 与 has_api_key，输入框留占位提示；
//     用户不改则不提交该键（后端指针字段 nil=保持原值，脱敏哨兵/空串也不会覆盖真值，双保险）；
//   · stock/spot/risk_gate 为整档案替换——保存时总是回传完整三段（未编辑字段用 GET 原值
//     合并，避免 strategies/blacklist 等本表单不展示的键被清零）；
//   · 校验单一权威在后端 config.ValidateBinance：400 的 msg 直接展示，不在前端复写规则；
//   · §战法批-5 新增三块表单：派发参数 dispatch（整档替换、七键全展示无隐蔽键）、
//     事件源与大模型打分 events（GET 是派生掩码视图——提交时按视图键全量往返，两把密钥
//     走「留空=保持」哨兵，与 api_key 同口径）、市场档案内 paper/paper_cash 纸面柜台行。
// English: §strategy-batch-5 adds the dispatch form (full round-trip), the events/LLM scorer
// form (masked keys keep-the-old convention) and per-market paper desk rows.
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
  // §MR-4B 杠杆红线确认戳：leverage>1 且交易面开启时后端 validateBinance 要求非空才放行。
  leverage_acknowledged_at: '',
  api_key: '', api_secret: '',
  // §战法批-5 两把事件侧密钥：仅输入新值时提交（留空=后端保持原值，掩码哨兵双保险）
  cp_token: '', llm_key: '',
})

export default function BinanceConfigPanel() {
  const [form, setForm] = useState(emptyForm())
  const [masked, setMasked] = useState({ api_key_masked: '', api_secret_masked: '', cp: '', llm: '' })
  const [hasKey, setHasKey] = useState({ key: false, secret: false, cp: false, llm: false })
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
        leverage_acknowledged_at: c.leverage_acknowledged_at || '',
      })
      setMasked({
        api_key_masked: c.api_key_masked || '', api_secret_masked: c.api_secret_masked || '',
        cp: (c.events && c.events.cryptopanic_token_masked) || '', llm: (c.events && c.events.llm_api_key_masked) || '',
      })
      setHasKey({
        key: !!c.has_api_key, secret: !!c.api_secret_masked,
        cp: !!(c.events && c.events.has_cryptopanic_token), llm: !!(c.events && c.events.has_llm_api_key),
      })
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
      // §MR-4B 红线戳只在有值时提交（后端标量局部合并：不下发=不改，防止误清历史确认）。
      if (form.leverage_acknowledged_at) body.leverage_acknowledged_at = form.leverage_acknowledged_at
      // 凭证：仅在用户真的输入了新值时提交（留空=保持，后端也有掩码哨兵兜底）
      if (form.api_key.trim()) body.api_key = form.api_key.trim()
      if (form.api_secret.trim()) body.api_secret = form.api_secret.trim()
      // 整档案替换语义：剥掉 __edited 哨兵后全量回传
      const strip = (p) => { const { __edited, ...rest } = p || {}; return rest }
      body.stock = strip(prof('stock'))
      body.spot = strip(prof('spot'))
      body.risk_gate = strip(prof('risk_gate'))
      // §战法批-5 派发参数：后端为整档替换，本表单覆盖全部七键（无隐蔽键，往返零丢失）。
      const d = prof('dispatch')
      body.dispatch = {
        enabled: !!d.enabled, bear_enabled: !!d.bear_enabled,
        min_confidence: d.min_confidence ?? 0, take_profit_pct: d.take_profit_pct ?? 0,
        stop_loss_pct: d.stop_loss_pct ?? 0, every_sec: d.every_sec ?? 0,
        max_live_orders: d.max_live_orders ?? 0,
      }
      // 事件源：GET 下发的是派生掩码视图（无 *_token 明文键），整档替换语义下
      // 按视图键全量往返；两把密钥只在用户输入新值时携带（留空=后端 mergeBinanceEvents 保持原值）。
      const ev = prof('events')
      body.events = {
        edgar_user_agent: ev.edgar_user_agent || '',
        cryptopanic_currencies: Array.isArray(ev.cryptopanic_currencies) ? ev.cryptopanic_currencies : [],
        llm_base_url: ev.llm_base_url || '', llm_model: ev.llm_model || '',
        llm_timeout_sec: ev.llm_timeout_sec ?? 0,
      }
      if (form.cp_token.trim()) body.events.cryptopanic_token = form.cp_token.trim()
      if (form.llm_key.trim()) body.events.llm_api_key = form.llm_key.trim()
      const res = await bapi.updateBinanceConfig(body)
      if (res) {
        baseRef.current = res
        // 保存成功后凭证输入框清空、回显换新一批掩码
        setForm((f) => ({ ...f, api_key: '', api_secret: '', cp_token: '', llm_key: '', stock: undefined, spot: undefined, risk_gate: undefined, dispatch: undefined, events: undefined, halted: !!res.halted }))
        setMasked({
          api_key_masked: res.api_key_masked || '', api_secret_masked: res.api_secret_masked || '',
          cp: (res.events && res.events.cryptopanic_token_masked) || '', llm: (res.events && res.events.llm_api_key_masked) || '',
        })
        setHasKey({
          key: !!res.has_api_key, secret: !!res.api_secret_masked,
          cp: !!(res.events && res.events.has_cryptopanic_token), llm: !!(res.events && res.events.has_llm_api_key),
        })
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
  // §战法批-5 两段派生视图：dispatch=后端原结构七键；events=GET 掩码视图（token 永不回明文）
  const disp = prof('dispatch')
  const ev = prof('events')
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
          {/* §MR-4B 加密产品线行：product_type 空=现货（出厂缺省，逐字节旧行为）；umfutures=USDT 本位永续。
              leverage 0=执行器按 1x 最保守预设；liq_dist_min_pct 0=强平距离闸不生效。 */}
          <div style={rowStyle}>
            <span style={labelStyle}>加密产品线 product_type</span>
            {[['', '现货 spot'], ['umfutures', 'USDT 本位永续']].map(([v, label]) => (
              <Button key={v} size="xs" variant={(spot.product_type || '') === v ? 'base' : 'outline'}
                theme={(spot.product_type || '') === v ? 'primary' : 'default'}
                onClick={() => patchProfile('spot', { product_type: v })}>{label}</Button>
            ))}
            <span style={inlineLabelStyle}>起始杠杆 leverage（0=按 1 倍保守设定，上限 20）</span>
            <InputNumber value={spot.leverage ?? 0} onChange={(v) => patchProfile('spot', { leverage: v })} min={0} max={20} theme="column" style={{ width: 100 }} />
            <span style={inlineLabelStyle}>强平距离下限 %（0=闸关）</span>
            <InputNumber value={spot.liq_dist_min_pct ?? 0} onChange={(v) => patchProfile('spot', { liq_dist_min_pct: v })} min={0} max={100} step={0.5} theme="column" style={{ width: 110 }} />
          </div>
          {/* 杠杆红线：>1 必须先本地确认（后端校验是单一权威，这里只给确认入口与拦截预告）。 */}
          {(spot.leverage ?? 0) > 1 && !form.leverage_acknowledged_at && (
            <div style={rowStyle}>
              <span style={labelStyle}>杠杆风险确认</span>
              <Button size="xs" theme="warning" variant="outline"
                onClick={() => set('leverage_acknowledged_at', new Date().toISOString())}>
                我已知悉杠杆与强平风险，确认启用杠杆&gt;1
              </Button>
              <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>未确认时保存会被后端拒绝（交易面开启 + 杠杆&gt;1 须先留确认时间戳）</span>
            </div>
          )}

          {/* §战法批-5 派发链参数：出厂全关（enabled=false 时信号只停留在观测页，一单不发）。
              门槛/节拍/帽留 0 即走后端缺省（0.6 / 600s / 5 单），解析器是单一权威。 */}
          <div style={subTitleStyle}>战法派发 dispatch（信号→下单自动链，出厂关闭）</div>
          <div style={rowStyle}>
            <span style={labelStyle}>派发总闸 enabled</span>
            <ToggleSw checked={!!disp.enabled} onChange={(v) => patchProfile('dispatch', { enabled: v })} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>开=每个节拍自动评估标的池并下单（mode=auto 才真投单；manual 只出摘要）；需先有台：enabled 真面或该市场 paper 纸面柜台</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>空头腿 bear_enabled</span>
            <ToggleSw checked={!!disp.bear_enabled} onChange={(v) => patchProfile('dispatch', { bear_enabled: v })} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>开=均线死叉/RSI 过热破位/利空事件可产「卖出开空」信号；借券闸仍逐单把关（真面无融券证据=拒开空）</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>置信门槛 min_confidence</span>
            <InputNumber value={disp.min_confidence ?? 0} onChange={(v) => patchProfile('dispatch', { min_confidence: v })} min={0} max={1} step={0.05} theme="column" style={{ width: 110 }} />
            <span style={inlineLabelStyle}>节拍秒 every_sec（0=缺省 600）</span>
            <InputNumber value={disp.every_sec ?? 0} onChange={(v) => patchProfile('dispatch', { every_sec: v })} min={0} max={86400} theme="column" style={{ width: 110 }} />
            <span style={inlineLabelStyle}>单轮新单帽 max_live_orders（0=缺省 5）</span>
            <InputNumber value={disp.max_live_orders ?? 0} onChange={(v) => patchProfile('dispatch', { max_live_orders: v })} min={0} max={100} theme="column" style={{ width: 110 }} />
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>持仓止盈 % take_profit_pct（0=关）</span>
            <InputNumber value={disp.take_profit_pct ?? 0} onChange={(v) => patchProfile('dispatch', { take_profit_pct: v })} min={0} step={0.5} theme="column" style={{ width: 110 }} />
            <span style={inlineLabelStyle}>止损 % stop_loss_pct（0=关）</span>
            <InputNumber value={disp.stop_loss_pct ?? 0} onChange={(v) => patchProfile('dispatch', { stop_loss_pct: v })} min={0} step={0.5} theme="column" style={{ width: 110 }} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>退出腿每轮扫描持仓（多空都算，按方向判盈亏），不占新单帽</span>
          </div>

          {/* §战法批-5 事件源与大模型打分：三把打分键齐备才装配打分器，任一空缺=关键词基线
              （fail-open 不拦事件腿）；密钥回显恒掩码，留空=保持。 */}
          <div style={subTitleStyle}>事件源与大模型打分 events（利空做空/利多买入场外信号）</div>
          <div style={rowStyle}>
            <span style={labelStyle}>EDGAR User-Agent</span>
            <Input style={{ width: 320 }} defaultValue={ev.edgar_user_agent || ''}
              onChange={(v) => patchProfile('events', { edgar_user_agent: v })}
              placeholder="name@example.com（SEC 政策要求含联系邮箱；空=US 事件腿不装配）" />
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>CryptoPanic Key</span>
            <Input style={{ width: 280 }} type="password" value={form.cp_token} onChange={(v) => set('cp_token', v)}
              placeholder={hasKey.cp ? `已配置 ${masked.cp}（留空保持不变）` : '输入 CryptoPanic API Key'} />
            <span style={inlineLabelStyle}>关注币种（逗号分隔）</span>
            <Input style={{ width: 180 }} defaultValue={(ev.cryptopanic_currencies || []).join(',')}
              onChange={(v) => patchProfile('events', { cryptopanic_currencies: String(v).split(',').map((x) => x.trim()).filter(Boolean) })}
              placeholder="BTC,ETH" />
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>大模型 Base URL</span>
            <Input style={{ width: 280 }} defaultValue={ev.llm_base_url || ''}
              onChange={(v) => patchProfile('events', { llm_base_url: v })}
              placeholder="https://api.deepseek.com/v1（OpenAI 兼容 chat 面）" />
            <span style={inlineLabelStyle}>模型 model</span>
            <Input style={{ width: 160 }} defaultValue={ev.llm_model || ''}
              onChange={(v) => patchProfile('events', { llm_model: v })} placeholder="deepseek-chat" />
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>大模型 API Key</span>
            <Input style={{ width: 280 }} type="password" value={form.llm_key} onChange={(v) => set('llm_key', v)}
              placeholder={hasKey.llm ? `已配置 ${masked.llm}（留空保持不变）` : '输入打分用 LLM Key'} />
            <span style={inlineLabelStyle}>超时秒 llm_timeout_sec（0=缺省 15）</span>
            <InputNumber value={ev.llm_timeout_sec ?? 0} onChange={(v) => patchProfile('events', { llm_timeout_sec: v })} min={0} max={120} theme="column" style={{ width: 100 }} />
            <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>Base URL+Key+模型三键齐备才启用精修打分；任一缺席或打分失败=自动回落关键词基线，事件腿不断链</span>
          </div>

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
      {/* §MR-4A 做空保证金率：0=该市场禁做空（出厂缺省）；0~1 之间才放行开空（借券闸读它）。 */}
      <span style={inlineLabelStyle}>做空保证金率 short_margin_rate</span>
      <InputNumber value={p.short_margin_rate ?? 0} onChange={(v) => patch(profName, { short_margin_rate: v })} min={0} max={1} step={0.05} theme="column" style={{ width: 110 }} />
      <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>0=禁做空；0.5=开空冻结五成权益</span>
      {/* §战法批-5 纸面柜台：无密钥按现价即时成交的四方向模拟台（买/卖/开空/平空）。
          真面 enabled 时永远让位（PaperActive 谓词），两态互斥不需要用户操心。 */}
      <span style={inlineLabelStyle}>纸面柜台 paper</span>
      <ToggleSw checked={!!p.paper} onChange={(v) => patch(profName, { paper: v })} />
      <span style={inlineLabelStyle}>纸面初始资金 paper_cash（0=缺省 10 万）</span>
      <InputNumber value={p.paper_cash ?? 0} onChange={(v) => patch(profName, { paper_cash: v })} min={0} theme="column" style={{ width: 130 }} />
      <span style={{ fontSize: 12, color: 'var(--app-muted)' }}>{moneyHint}；开=本市场信号在模拟台成交（重启后资金连续）</span>
    </div>
  )
}
