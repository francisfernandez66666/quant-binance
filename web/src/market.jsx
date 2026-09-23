// ── 全局市场维度 market.jsx ──
// 文件职责（PLAN §11.2 全局 App）：市场切换 Tab（全部 | A股 | 美股 | 加密货币）的
// 状态容器 + 组件。当前市场经 React Context 下发给持仓/量化/信号等页面做数据过滤与
// 格式化；持久化双通道：路由 query `?m=`（可分享链接）+ localStorage（刷新/进页兜底）。
// 缺省 ALL=全部：存量 CN 页面展示行为零变更（Tab 不选即不过滤）。
// English: global market switcher (All | CN | US | Crypto) — context provider + tab bar,
// persisted in the route query (?m=) and localStorage; default ALL keeps legacy behavior.
import React, { createContext, useCallback, useContext, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { MARKET_LABELS, MARKETS } from './utils.market.js'

// 「全部」伪市场值：数据层不过滤、格式化按行内 market 自适应
export const MARKET_ALL = 'ALL'
const STORAGE_KEY = 'liangzai_market'

// Tab 顺序即 PLAN §11.2 顶栏顺序（CN | 加密 | 美股 归并展示，全市场缺省在前）
const TAB_ITEMS = [
  { value: MARKET_ALL, label: '全部' },
  { value: 'CN', label: MARKET_LABELS.CN },
  { value: 'US', label: MARKET_LABELS.US },
  { value: 'CRYPTO', label: MARKET_LABELS.CRYPTO },
]

// 默认值即「未挂 Provider」时的兜底：ALL 不过滤、match 恒真——保证存量测试夹具/局部挂载
// （仅渲染单页不进 App 外壳）不因缺 Provider 抛错，行为等价"全部市场"。
const MarketContext = createContext({ market: MARKET_ALL, setMarket: () => {}, match: () => true })

// 初值解析优先级：URL query ?m= > localStorage > ALL（非法值一律回落 ALL）
function readInitialMarket(search) {
  const q = new URLSearchParams(search).get('m')
  if (q && (q === MARKET_ALL || MARKETS.includes(q))) return q
  try {
    const ls = localStorage.getItem(STORAGE_KEY)
    if (ls && (ls === MARKET_ALL || MARKETS.includes(ls))) return ls
  } catch (_) { /* 隐私模式下 localStorage 抛错：静默回落 */ }
  return MARKET_ALL
}

/**
 * 市场上下文 Provider：挂在 App 最外层（Router 之内——需读写 location.query）。
 * setMarket 同时写 localStorage 并把 `?m=` 落到当前路由（replace，不留历史记录栈垃圾）。
 */
export function MarketProvider({ children }) {
  const location = useLocation()
  const navigate = useNavigate()
  const [market, setMarketState] = useState(() => readInitialMarket(location.search))

  const setMarket = useCallback((next) => {
    if (!MARKETS.includes(next) && next !== MARKET_ALL) return
    setMarketState(next)
    try { localStorage.setItem(STORAGE_KEY, next) } catch (_) {}
    // query 持久化：ALL 也显式写出（m=ALL），避免"删参数"分支导致分享链接语义漂移
    const params = new URLSearchParams(location.search)
    params.set('m', next)
    navigate({ pathname: location.pathname, search: params.toString() }, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname, location.search, navigate])

  // match(rowMarket, code)：供列表过滤——'ALL' 恒真；优先行内 market 字段，
  // 缺字段时由代码形态推断（与后端 InferMarketOf 同规则）
  const match = useCallback((rowMarket, code) => {
    if (market === MARKET_ALL) return true
    const m = rowMarket || inferMarketForFilter(String(code || '').trim().toUpperCase())
    return m === market
  }, [market])

  const value = useMemo(() => ({ market, setMarket, match }), [market, setMarket, match])
  return <MarketContext.Provider value={value}>{children}</MarketContext.Provider>
}

// 消费钩子：页面里 const { market, setMarket, match } = useMarket()
export function useMarket() {
  return useContext(MarketContext)
}

// 过滤器兜底推断（与 utils.market.parseCode 同规则的精简版，避免本模块引入格式化依赖）
function inferMarketForFilter(s) {
  if (!s) return 'CN'
  if (/^[0-9]{6}\.(SH|SZ|BJ)$/.test(s)) return 'CN'
  if (s.indexOf('.') >= 0) return 'US'
  if (/(USDT|USDC|BUSD|FDUSD|TUSD|USDD|DAI|BTC|ETH|BNB|EUR|TRY|JPY|AUD)$/.test(s)) return 'CRYPTO'
  if (/^[A-Z][A-Z0-9.]{0,9}$/.test(s)) return 'US'
  return 'CN'
}

/**
 * 市场切换 Tab（顶栏组件）：轻量自绘分段按钮，不引入额外 TDesign 组件依赖，
 * 样式跟随 CSS 变量（--app-accent）以适配深浅主题。
 */
export function MarketTabs() {
  const { market, setMarket } = useMarket()
  return (
    <div role="tablist" aria-label="市场切换" style={{ display: 'inline-flex', gap: 2, border: '1px solid var(--app-border)', borderRadius: 6, padding: 2, background: 'var(--app-bg, transparent)' }}>
      {TAB_ITEMS.map((it) => {
        const active = market === it.value
        return (
          <button key={it.value} role="tab" aria-selected={active} type="button"
            onClick={() => setMarket(it.value)}
            style={{
              border: 'none', cursor: 'pointer', fontSize: 12, lineHeight: '20px',
              padding: '0 8px', borderRadius: 4,
              background: active ? 'var(--app-accent, #0052d9)' : 'transparent',
              color: active ? '#fff' : 'var(--app-text-2, inherit)',
            }}>
            {it.label}
          </button>
        )
      })}
    </div>
  )
}
