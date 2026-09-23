// ── 市场维度格式化工具 utils.market.js ──
// 文件职责（PLAN_BINANCE_MULTI_ASSET_20260922 §7/§11）：三市场（CN A股 / US 美股 /
// CRYPTO 加密货币）的货币、数量格式化与代码→市场解析纯函数。
// 边界约定：utils.js 的 fmtCNY2/fmtMoney（无市场参数版）保留不动，存量 CN 页零风险；
// 需要市场维度的展示位改从本模块导入 fmtMoneyM/fmtQty/parseCode。
// parseCode 判定顺序逐条镜像 Go internal/config/broker.go InferMarketOf（只读镜像，不改 Go）：
//   1) 6位数字+.SH/.SZ/.BJ → CN；2) 其余含 "." → US 点分代码（BRK.B）；
//   3) 币安计价币对尾缀白名单 → CRYPTO；4) 纯大写字母开头 ticker（≤10位）→ US；
//   5) 缺省 CN——与存量链路默认市场一致（加市场不换市场）。
// English: market-aware money/qty/code parsing. The code rules mirror Go InferMarketOf
// in judgment order; CN pages keep using the legacy fmtCNY2 untouched.

// 市场枚举（与 Go 侧 validTsCode / RealPosition.Market 取值一致）
export const MARKET_CN = 'CN'
export const MARKET_US = 'US'
export const MARKET_CRYPTO = 'CRYPTO'
export const MARKETS = [MARKET_CN, MARKET_US, MARKET_CRYPTO]

// 中文名映射（Tab 与表头展示）
export const MARKET_LABELS = { CN: 'A股', US: '美股', CRYPTO: '加密货币' }

// ── 与 Go broker.go 同源的正则（判定顺序即消歧顺序）──
const CN_TS_CODE_RE = /^[0-9]{6}\.(SH|SZ|BJ)$/
// 币安计价币尾缀白名单：纯字母冲突（BTCUSDT vs AAPL 同形）靠尾缀区分，仅做兜底推断
const QUOTE_PAIR_RE = /(USDT|USDC|BUSD|FDUSD|TUSD|USDD|DAI|BTC|ETH|BNB|EUR|TRY|JPY|AUD)$/
const US_TICKER_RE = /^[A-Z][A-Z0-9.]{0,9}$/

/**
 * 代码 → { market, symbol }（镜像 Go InferMarketOf + 剥离后缀的裸码）。
 * "600519.SH"→{CN,'600519'}；"BTCUSDT"→{CRYPTO,'BTCUSDT'}；"AAPL"→{US,'AAPL'}。
 * @param {string} code - 任意形态标的代码（大小写不敏感，Go 侧同样先 ToUpper）
 * @returns {{market: string, symbol: string}}
 */
export function parseCode(code) {
  const s = String(code == null ? '' : code).trim().toUpperCase()
  if (!s) return { market: MARKET_CN, symbol: '' }
  if (CN_TS_CODE_RE.test(s)) return { market: MARKET_CN, symbol: s.replace(/\..*$/, '') }
  if (s.indexOf('.') >= 0) return { market: MARKET_US, symbol: s }
  if (QUOTE_PAIR_RE.test(s)) return { market: MARKET_CRYPTO, symbol: s }
  if (US_TICKER_RE.test(s)) return { market: MARKET_US, symbol: s }
  return { market: MARKET_CN, symbol: s }
}

// 便捷谓词：代码是否属于某市场（行数据缺 market 字段时的兜底判定）
export function marketOf(code) { return parseCode(code).market }

/**
 * 金额格式化（带市场维度）：
 *  - CN     ¥ + 2 位小数（与 fmtCNY2 同口径）；
 *  - US     $ + 2 位小数；
 *  - CRYPTO $ + 最多 8 位小数去尾零（USDT/USDC 以 $ 展示，tooltip 标币种由调用处决定）。
 * @param {number|string|null} v - 原始金额
 * @param {string} [market] - 'CN' | 'US' | 'CRYPTO'，缺省 CN（加市场不换默认）
 * @returns {string} 如 "¥435.00" / "$12.50" / "$0.00012345"，非法值 "-"
 */
export function fmtMoneyM(v, market = MARKET_CN) {
  if (v === null || v === undefined || isNaN(Number(v))) return '-'
  const n = Number(v)
  switch (market) {
    case MARKET_CRYPTO: return '$' + trimTrailingZeros(n.toFixed(8)) // 微定价：0.0001234500 → 0.00012345
    case MARKET_US: return '$' + n.toFixed(2)
    default: return '¥' + n.toFixed(2)
  }
}

/**
 * 数量格式化（带市场维度）：
 *  - CN     整手（100 的整数倍为常态）→ 整数股展示，四舍五入；
 *  - US     整股直显，碎股保留至多 4 位小数去尾零；
 *  - CRYPTO 小数量给 4 位有效数字、常规至多 8 位小数去尾零。
 * @param {number|string|null} v - 原始数量
 * @param {string} [market] - 市场，缺省 CN
 */
export function fmtQty(v, market = MARKET_CN) {
  if (v === null || v === undefined || isNaN(Number(v))) return '-'
  const n = Number(v)
  if (market === MARKET_CN) return String(Math.round(n)) // A 股整手：不出现小数
  if (market === MARKET_US) return n % 1 === 0 ? String(n) : trimTrailingZeros(n.toFixed(4))
  // CRYPTO：<1 用 4 位有效数字（0.00123456 → 0.001235），否则最多 8 位小数去尾零
  return n < 1 ? String(Number(n.toPrecision(4))) : trimTrailingZeros(n.toFixed(8))
}

/**
 * 计价单位词（持仓/委托表"数量"列后缀）：CN 股、US 股（碎股）、CRYPTO 枚。
 */
export function qtyUnit(market = MARKET_CN) {
  if (market === MARKET_CRYPTO) return '枚'
  return '股'
}

/**
 * 按市场取整委托数量（前端预览用；权威取整在后端 roundQty——stepSize/lot 规则）：
 *  - CN：整手 floor(qty/100)*100；
 *  - US：默认整股（碎股开关由配置侧决定，这里给整股预览）；
 *  - CRYPTO：原样返回（stepSize 对齐在后端）。
 */
export function roundQtyPreview(v, market = MARKET_CN) {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0) return 0
  if (market === MARKET_CN) return Math.floor(n / 100) * 100
  if (market === MARKET_US) return Math.floor(n)
  return n
}

// 去尾零：'0.0001234500'→'0.00012345'、'12.50'→'12.5'；整数值不留小数点
function trimTrailingZeros(s) {
  if (s.indexOf('.') < 0) return s
  return s.replace(/0+$/, '').replace(/\.$/, '')
}
