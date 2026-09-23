// 文件职责：/api/v3/exchangeInfo（及美股 sapi 同族端点）响应 → BinanceSymbolRule 索引。
// 加市场任务第 3 项的解析层：三字段映射见 binance_symbol_rules.go 文件头，本文件只负责
// "从 filters 数组里认出这三类 filter"。
//
// 三条纪律：
//  1. 一次响应建全表索引（exchangeInfo 本来就返回全量 symbols），逐票查询不再发请求；
//  2. 认不出的 filterType 直接跳过——**不猜**：把未知的 minQty 当 stepSize 用会让下单量
//     算成天文数字，比"这条规则未知"严重得多；
//  3. 非 TRADING 状态的品种保留在表里但带上 Status（§9 的 tradingStatus 证据闸自己判），
//     这里不做交易许可判断——规则表管"怎么下单"，状态闸管"能不能下单"，两件事不混。
package data

import (
	"strings"
)

// parseBinanceExchangeInfo 响应 → map[symbol]BinanceSymbolRule（全脏则返回空表，
// 由 store 的空表保护挡住覆盖）。
func parseBinanceExchangeInfo(raw map[string]any) map[string]BinanceSymbolRule {
	out := map[string]BinanceSymbolRule{}
	list, ok := raw["symbols"].([]any)
	if !ok || len(list) == 0 {
		return out
	}
	for _, item := range list {
		symMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rule, ok := parseBinanceSymbolRule(symMap)
		if !ok {
			continue
		}
		out[rule.Symbol] = rule
	}
	return out
}

// parseBinanceSymbolRule 单品种规则（symbol 缺失/无任一有效字段则 false）。
func parseBinanceSymbolRule(m map[string]any) (BinanceSymbolRule, bool) {
	sym := normalizeBinanceSymbol(mapAnyString(m, "symbol", "s", "code"))
	if sym == "" {
		return BinanceSymbolRule{}, false
	}
	r := BinanceSymbolRule{Symbol: sym, Status: strings.ToUpper(strings.TrimSpace(mapAnyString(m, "status", "st")))}
	for _, f := range filterList(m["filters"]) {
		switch strings.ToUpper(strings.TrimSpace(mapAnyString(f, "filterType", "type"))) {
		case "LOT_SIZE":
			// ⚠ 只认 stepSize：minQty 是"最小下单量"、stepSize 是"数量步长"，两者语义不同
			// （BTC 常见 stepSize=0.001 而 minQty=0.00001）。曾用 minQty 兜底 stepSize，
			// 会让 RoundQty 按错口径取整、下单量被交易所拒（Agent B 缺陷④修复）。
			// LOT_SIZE 缺 stepSize 时保持 0=未知，由 MARKET_LOT_SIZE 兜底或 §9 闸拒单。
			r.StepSize = parseBinanceStep(mapAnyString(f, "stepSize"))
		case "MARKET_LOT_SIZE":
			if r.StepSize <= 0 {
				r.StepSize = parseBinanceStep(mapAnyString(f, "stepSize"))
			}
		case "PRICE_FILTER":
			r.TickSize = parseBinanceStep(mapAnyString(f, "tickSize"))
		case "NOTIONAL", "MIN_NOTIONAL":
			// minNotional（新）与 notional（老字段名）都认；两者都为 0 即"未知"，保持 0 不猜。
			r.MinNotional = firstPositiveAny(
				parseBinanceStep(mapAnyString(f, "minNotional")),
				parseBinanceStep(mapAnyString(f, "notional")))
		}
	}
	return r, r.HasEvidence()
}

// filterList filters 字段 → []map（形态异常时返回 nil，绝不 panic）。
func filterList(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
