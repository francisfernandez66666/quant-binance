// ── 实盘链路三态判定 gatewayLinkState.js ──
// 文件职责：把「A股总开关 + QMT 网关快照」收敛成一个链路判定，供三处展示位同源使用——
//   量化交易页·链路状态卡、持仓页·实盘持仓头、仪表盘·系统状态行。
// 为什么要多一个 frozen 态（2026-09-24 owner 裁决「广州不部署、A股链路冻结维护」）：
//   执行机不再是部署目标之后网关永不再上报，但配置里的 rules.qmt.enabled 可能仍留着历史值
//   true——于是界面把"这台机器上根本不会再有网关"显示成"网关开着但连不通"，人就会去排查一条
//   不存在的链路。三态各说各话：frozen=这条链路已冻结；off=链路在但没启用；
//   down=已启用、应当在线却探不到。
// cn_master 从哪来：App.jsx 拉 /api/status 时经 setCnMaster 写入本模块缓存——不额外发请求，
//   也不新引一套状态容器；null（老服务端不回该字段、或首轮未到达）时一律不判 frozen，
//   保持改动前的展示口径，避免把"没取到"当成"已冻结"。

let cnMaster = null

// 订阅者集合：cn_master 刻意不装进 React 状态容器（避免再引一套全局 store），但"缓存变了"
// 这件事必须能叫醒把它读进 useMemo 依赖的组件。缺这一腿会出 Z4 抓到的形态：
// 仪表盘首帧在 App 的状态轮询到达之前算出 qmtLine=''（此时 cnMaster 还是 null=未知），
// 而网关 503 时 qmtState 永远不再变化 → useMemo 永不重算 → 「实盘链路」整行静默消失，
// 恰好复刻 §LINTGATE 那条"链路行永不渲染"的事故形状。
const listeners = new Set()

export function subscribeCnMaster(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

// setCnMaster 只认显式布尔（与 App.jsx 对 /api/status 的读法一致），其余值归 null=未知；
// 值未变化时不广播（App 每 5s 轮询一次，无变化就唤醒会造成无意义重渲染）。
export function setCnMaster(v) {
  const next = typeof v === 'boolean' ? v : null
  if (next === cnMaster) return
  cnMaster = next
  for (const fn of listeners) fn()
}

export function getCnMaster() {
  return cnMaster
}

export const LINK_FROZEN = 'frozen'
export const LINK_OFF = 'off'
export const LINK_DOWN = 'down'
export const LINK_LIVE = 'live'
export const LINK_UNKNOWN = 'unknown'

// 冻结横幅文案：三处展示位共用一句，避免各页写法漂移成三种"看起来都像正常"的说法。
export const FROZEN_HINT = 'A股实盘链路已冻结维护：广州执行机不再是部署目标，网关不再上报；下列各项显示为「未生效」，不是正常运行。'

// 单行横幅短文案（仪表盘系统卡「实盘链路：」行前缀之后接这一段，故不再重复"实盘链路"四字）。
export const FROZEN_HINT_SHORT = 'A股链路已冻结维护（广州执行机不再是部署目标，网关不再上报）'

/* gatewayVerdict 判定顺序即优先级：冻结 > 未启用 > 已启用但探不到 > 在线 > 未知。
   "未探测过"不算 down：下行探测只在连续竞价窗口跑（桥心跳由 QMT tick 驱动），盘前/午休静默
   属正常，计入失联会每天误报——这条口径沿用改动前 Quant.jsx 的 probeNever 判据。 */
export function gatewayVerdict(state) {
  if (cnMaster === false) return LINK_FROZEN
  if (!state) return LINK_UNKNOWN
  if (!state.enabled) return LINK_OFF
  const probed = Boolean(state.last_probe_at) && !String(state.last_probe_at).startsWith('0001-')
  if (probed && state.last_probe_ok === false) return LINK_DOWN
  return LINK_LIVE
}

// 未生效占位（冻结态下取代"正常/已启用/休市未探测"这类看起来像运行中的值）
export const NOT_IN_EFFECT = '未生效'
