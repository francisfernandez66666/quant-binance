// §AUDIT1019-M8 尾巴：从 web/src/pages/Research.jsx（1889 行 god component）抽出的纯格式化/派生函数。
//
// 这些函数零组件状态依赖（不读 useState / useRef / props 之外的内容），只吃入参返回展示值，
// 故可独立单测。抽出后 Research.jsx 改为从此处 import，调用点零改动；
// 行为必须与抽出前逐字节等价（中文文案、空值占位符、阈值 0.3 等一律不动）。
//
// 仍留在 Research.jsx 内的同族函数（读组件状态，不宜抽）：
//   factorName(id)      —— 读 factorMeta.current
//   factorRule(c)       —— 调 factorName，间接读 factorMeta
//   btPct(id)           —— 读 backtestProgress[id]
export function fmtPctGlobal(v) {
  if (v === null || v === undefined || isNaN(v)) return '-'
  return Number(v).toFixed(2) + '%'
}

export function pct(v) {
  if (v === null || v === undefined || isNaN(v)) return 0
  return Math.min(100, Math.round(v * 100))
}

export function fmtRows(v) {
  // 格式化计数：千分位展示样本/持仓等整数
  if (v === null || v === undefined || isNaN(v)) return '-'
  return Number(v).toLocaleString('zh-CN')
}

export function fmt(v) {
  // 格式化数值：保留 4 位小数（因子/权重精度用）
  if (v === null || v === undefined || isNaN(v)) return '-'
  return Number(v).toFixed(4)
}

export function signClass(v) {
  // 根据正负返回 pos/neg 样式类，用于盈亏着色
  if (v === null || v === undefined || isNaN(v)) return ''
  return Number(v) >= 0 ? 'pos' : 'neg'
}

export function signColor(v) {
  // 根据正负返回红/绿颜色（红涨绿跌）
  if (v === null || v === undefined || isNaN(v)) return 'var(--app-faint)'
  return Number(v) >= 0 ? 'var(--app-up)' : 'var(--app-down)'
}

export function fmtNum(v, digits) {
  // 格式化数值：指定小数位，空值返回占位符
  if (v === null || v === undefined || isNaN(v)) return '-'
  const d = digits === undefined ? 0 : digits
  // 常量 d：局部定义
  return Number(v).toFixed(d)
}

export function weightList(c) {
  // 解析战法权重对象为可读的「因子名×权重」列表
  try {
    const w = JSON.parse(c.weights || '{}')
    // 常量 w：局部定义
    const wm = (w && typeof w === 'object' && w.weights && typeof w.weights === 'object' && !Array.isArray(w.weights)) ? w.weights : w
    // 常量 wm：局部定义
    if (!wm || typeof wm !== 'object') return []
    const entries = Object.entries(wm).filter(([, v]) => typeof v === 'number' && isFinite(v))
    // 常量 entries：局部定义
    return entries.sort((a, b) => b[1] - a[1])
  } catch (_) { return [] }
}

export function depthSummary(c) {
  // 汇总盘口扫描深度指标为简要文案
  try { return JSON.parse(c.weights || '{}') } catch (_) { return {} }
}

export function kindLabel(k) {
  // 将研究类型（factor/pattern/weights/depth）翻译为中文标签
  const m = { factor: '因子战法', pattern: '形态战法', weights: '权重优化', depth: '盘口扫描' }
  // 常量 m：局部定义
  return m[k] || k
}

export function factorDirs(c) {
  // 解析因子方向（多/空）为可读文案
  try {
    const w = JSON.parse(c.weights || '{}')
    // 常量 w：局部定义
    if (w && typeof w === 'object' && w.directions && typeof w.directions === 'object') return w.directions
  } catch (_) {}
  return {}
}

export function parseReason(c, key) {
  // 解析命中原因字段，拆出模式/方向/阈值等子项
  const reason = c.reason || ''
  // 常量 reason：局部定义
  const pats = {
    // 常量 pats：局部定义
    '样本内IR': /样本内IR=(-?\d+\.?\d*)/,
    '样本外IR': /样本外IR=(-?\d+\.?\d*)/,
    反推超额: /反推超额=(-?\d+\.?\d*)/,
  }
  const m = reason.match(pats[key])
  // 常量 m：局部定义
  return m ? parseFloat(m[1]) : null
}

export function fmtPct(v) {
  // 将 0~1 比率格式化为百分比字符串
  if (v === null || v === undefined || isNaN(v)) return '-'
  const s = (v * 100).toFixed(1)
  // 常量 s：局部定义
  return (v >= 0 ? '+' : '') + s + '%'
}

export function verdict(c) {
  // 根据样本内/样本外阈值判定战法回测结论（通过/失败）
  const insample = parseReason(c, '样本内IR')
  // 常量 insample：局部定义
  const outsample = parseReason(c, '样本外IR')
  // 常量 outsample：局部定义
  const gen = parseReason(c, '反推超额')
  // 常量 gen：局部定义
  const thr = 0.3
  // 常量 thr：局部定义
  const passed = (insample !== null ? insample >= thr : true) &&
    // 常量 passed：局部定义
    (outsample !== null ? outsample >= thr : true) &&
    (gen !== null ? gen > 0 : true)
  if (passed) return { ok: true, text: '电脑用两段互不相干的历史行情分别验证过，这条规律都能跑赢，不是碰运气。' }
  return { ok: false, text: '这条规律在验证中没站稳，不建议直接拿来实盘。' }
}

export function plainLines(c) {
  // 将候选理由多行文本拆分为纯文本行数组
  const insample = parseReason(c, '样本内IR')
  // 常量 insample：局部定义
  const outsample = parseReason(c, '样本外IR')
  // 常量 outsample：局部定义
  const gen = parseReason(c, '反推超额')
  // 常量 gen：局部定义
  const lines = []
  // 常量 lines：局部定义
  if (insample !== null) lines.push('先拿前半段历史行情回放：这套打分的选股效果明显（稳定度 ' + fmt(insample) + '，越高越靠谱）。')
  if (outsample !== null) lines.push('再拿一段完全没参与挑规律的行情回放：效果仍然明显（稳定度 ' + fmt(outsample) + '）。这一步是防止规律只对老数据灵、换市场就失灵。')
  if (gen !== null) lines.push('最后对比「按这套规律选出的股票」和「随便买」：选的比平均多赚 ' + fmtPct(gen) + '，说明规律确实挑得出好股票。')
  if (lines.length === 0) lines.push(/通过护栏/.test(c.reason || '') ? '电脑验证通过，这条规律可以试试。' : '这条规律未通过验证。')
  return lines
}

export function btTested(c) {
  // 判断该候选是否已跑过回测
  if (typeof c.backtest_done === 'boolean') return c.backtest_done
  return c.avg_excess !== 0
}
