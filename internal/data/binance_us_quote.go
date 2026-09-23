// 文件职责：币安**美股（equity）**行情 WS payload 解析器（Phase 3 行情面，PLAN §2.4）。
// 两条流：
//
//	price        —— 全 symbol 最新价快照（服务端 3s 轮询式推送，一条帧含全市场）；
//	{SYM}@quote  —— 逐票盘口，ap=ask price / bp=bid price（每 symbol ≤200ms 一推）。
//
// ⚠ **未实测契约**（GAP_BINANCE_READINESS §G-1：本机 nbstream.binance.com 不可达，须首尔出口
// 实测；PLAN §13 Q7 同题）。因此本文件把"字段口径不确定"收敛成两处可改点：
//  1. tickFromEquityMap 宽容取键（symbol/Symbol/SYMBOL/s/code、price/p/last/px）；
//  2. 订阅基址可注入（BinanceQuoteFeedOptions.WsBase），httptest mock server 能整段回放。
//
// 真机对不上时只改键名表 + 补一条表驱动用例，其余链路不动。
//
// 反假绿姿势：price 数组里无价条目静默跳过，但**整帧一条都不合格**时返回 error（"订阅成功、
// 永远 0 命中"是这类未实测通道最危险的失败形态，必须能被 Healthy/计数看见）。
//
// English: parsers for the UNVERIFIED Binance US equity streams (full-market `price` snapshot
// and per-symbol `@quote` book). Key lookup is intentionally tolerant and the base URL is
// injectable until the Seoul-box probe closes GAP §G-1; an all-unparseable frame errors out so
// "subscribed but never matching" can never look green.
package data

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseEquityPrice 解析美股 price 流：既吃单对象 {symbol,price}，也吃数组 [{symbol,price},...]。
func parseEquityPrice(raw []byte) ([]binanceTick, error) {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "[") {
		var arr []map[string]any
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("equity price 数组解析失败: %w", err)
		}
		out := make([]binanceTick, 0, len(arr))
		for _, item := range arr {
			if t, ok := tickFromEquityMap(item); ok {
				out = append(out, t)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("equity price 数组无带价条目（%d 项）", len(arr))
		}
		return out, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("equity price 对象解析失败: %w", err)
	}
	if t, ok := tickFromEquityMap(obj); ok {
		return []binanceTick{t}, nil
	}
	return nil, fmt.Errorf("equity price 缺 symbol/price 字段")
}

// parseEquityQuote 解析 {SYM}@quote 逐票盘口（ap/bp；as/bs 档位量本项目不消费）。
func parseEquityQuote(raw []byte) ([]binanceTick, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("equity quote 解析失败: %w", err)
	}
	t, ok := tickFromEquityMap(obj)
	if !ok {
		return nil, fmt.Errorf("equity quote 缺 symbol/ap|bp 字段")
	}
	return []binanceTick{t}, nil
}

// tickFromEquityMap 从宽松 JSON 映射里抽一只美股 tick（symbol + 任一正价即可用）。
// 键名兼容是**未实测契约的防御面**（见文件头），不是功能开关：真机确认后应收紧成单键。
func tickFromEquityMap(m map[string]any) (binanceTick, bool) {
	if len(m) == 0 {
		return binanceTick{}, false
	}
	sym := ""
	for _, k := range []string{"symbol", "Symbol", "SYMBOL", "s", "code"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			sym = strings.ToUpper(strings.TrimSpace(v))
			break
		}
	}
	if sym == "" {
		return binanceTick{}, false
	}
	t := binanceTick{
		Symbol:    sym,
		Ask:       anyFloat(m["ap"]),
		Bid:       anyFloat(m["bp"]),
		Price:     firstPositiveAny(anyFloat(m["price"]), anyFloat(m["p"]), anyFloat(m["last"]), anyFloat(m["px"])),
		PrevClose: anyFloat(m["pc"]),
		Open:      anyFloat(m["op"]),
		High:      anyFloat(m["hi"]),
		Low:       anyFloat(m["lo"]),
		Volume:    anyFloat(m["v"]),
		Amount:    anyFloat(m["q"]),
		TimeMs:    int64(anyFloat(m["timestamp"])),
	}
	if t.Price <= 0 && t.Bid > 0 && t.Ask > 0 {
		t.Price = (t.Bid + t.Ask) / 2 // @quote 流常态：仅有盘口价时用中间价
	}
	if t.Price > 0 && t.PrevClose > 0 {
		t.ChangePct = (t.Price/t.PrevClose - 1) * 100
	}
	return t, t.valid()
}
