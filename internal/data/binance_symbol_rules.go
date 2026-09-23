// 文件职责：币安 **交易规则查表**（加市场任务第 3 项：把 risk.SymbolRules 的三字段落到
// 币安 exchangeInfo 上；PLAN §2.8/§9、GAP §G-1 可注入端点）。
//
// 为什么在 internal/data 里再定义一个规则结构体，而不直接用 risk.SymbolRules：
//
//	import 方向是 risk → data（risk/gate.go 引用 data 类型），data 反向 import risk 会成环。
//	故这里定义 **字段 1:1 对齐** 的 BinanceSymbolRule，装配层一行转换即可喂给
//	Gate.SetSymbolRulesSource（签名 func(symbol string) (risk.SymbolRules, error)）。
//
// 三字段与 exchangeInfo 的映射（PLAN §9 下单前置校验的三条硬约束）：
//
//	MinNotional ← filters.NOTIONAL.minNotional（现货）或 MARKET_LOT_SIZE.minNotional；
//	              ⚠ 币安部分老品种不下发 NOTIONAL，此时为 0=未知，由消费方决定回退策略，
//	              这里**绝不猜一个 10 USDT 兜底**（猜出来的下限会让闸门锁在假安全上）；
//	StepSize    ← filters.LOT_SIZE.stepSize（数量步长，决定股数/币数取整）；
//	TickSize    ← filters.PRICE_FILTER.tickSize（价格步长，决定限价单价格取整）。
//
// 缓存：TTL 缺省 6h（规则改动极少，但熔断/上币会改），过期后首次查询触发一次刷新；
// 刷新失败时**保留旧值**并记节流日志——规则宁可旧也不要空（空=闸门无据可查）。
package data

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BinanceSymbolRule 交易规则（字段与 risk.SymbolRules 一一对应，见文件头映射说明）。
type BinanceSymbolRule struct {
	Symbol      string
	MinNotional float64 // 0=源未下发（未知）
	StepSize    float64 // 0=未知
	TickSize    float64 // 0=未知
	FetchedAt   time.Time
	Status      string // 交易所品种状态 TRADING/BREAK/SETTLING（空=未知）
}

// HasEvidence 是否拿到过任何一条规则证据（全 0 且无状态=未确认，§9 闸据此拒单）。
func (r BinanceSymbolRule) HasEvidence() bool {
	return r.MinNotional > 0 || r.StepSize > 0 || r.TickSize > 0 || strings.TrimSpace(r.Status) != ""
}

// RoundQtyDown 按 stepSize 向下取整数量（step<=0 时原样返回=未知不取整）。
// 向下而非四舍五入：向上会让实际数量超过可用资金/持仓，制造拒单或超卖。
func (r BinanceSymbolRule) RoundQtyDown(qty float64) float64 {
	return roundToStepDown(qty, r.StepSize)
}

// RoundPrice 按 tickSize 取整价格（tick<=0 时原样返回）。
func (r BinanceSymbolRule) RoundPrice(price float64) float64 {
	return roundToStepDown(price, r.TickSize)
}

// roundToStepDown 通用向下步长取整（浮点误差用 1e-9 相对容差收口）。
func roundToStepDown(v, step float64) float64 {
	if step <= 0 || v <= 0 {
		return v
	}
	steps := v / step
	floored := float64(int64(steps + 1e-9))
	if floored*steps < 0 {
		return v // 负数场景（不适用价格/数量）：不动原值
	}
	out := floored * step
	if diff := out - v; diff > 1e-9*maxAbs(v, step) {
		out -= step // 浮点向上偏了：再降一格
	}
	if out < 0 {
		return 0
	}
	return out
}

// maxAbs 两数绝对值取大（容差基准）。
func maxAbs(a, b float64) float64 {
	return firstPositiveAny(positiveAbs(a), positiveAbs(b))
}

// positiveAbs 绝对值（0 保留 0）。
func positiveAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// parseBinanceStep 规则字符串（"0.00001000"）→ float（非法/空=0=未知）。
func parseBinanceStep(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// exchangeInfoPath 规则端点前缀（美股走 sapi，路径未实测，基址可注入）。
func (c *BinanceRestClient) exchangeInfoPath() string {
	if c.Equity {
		return "/sapi/v1/equity/market/exchangeInfo"
	}
	return "/api/v3/exchangeInfo"
}

// symbolRuleError 统一错误文案（排障时能一眼看出是哪条链缺据）。
func symbolRuleError(symbol string, why string) error {
	return fmt.Errorf("binance rules %s: %s", normalizeBinanceSymbol(symbol), why)
}

// 并发安全的规则表（查表/刷新见 binance_symbol_rules_cache.go，解析见 binance_exchange_info.go）。
type binanceRuleCache struct {
	ttl      time.Duration
	now      func() time.Time
	mu       sync.RWMutex
	rules    map[string]BinanceSymbolRule
	loadedAt time.Time
	loadDone chan struct{} // 非 nil 且有协程在刷时：等待者靠它收敛（finishLoad 时换新的）
	loading  bool
}
