// 文件职责：币安行情 WS 的**共用原子类型与数值工具**（Phase 3 行情面，PLAN §2.4/§2.8）。
// binanceTick 是现货（miniTicker/ticker/bookTicker）与美股（price/@quote）两种流解析后的
// 统一中间态，再由 binance_quotes.go 装配成本仓库的 MarketSnapshot（key=symbol 大写）。
//
// 数值口径三条硬规矩（与 §P1-5 / §M2 同族，反例都是"假绿/假新鲜"）：
//  1. 币安价格原样是字符串，解析失败=0，**绝不猜值**；非正价的 tick 判不可用（停牌/无成交）；
//  2. PrevClose 与 Close 分离：只有源真给了参考价才填（现货 @ticker 的 "o"=24h 前开盘、
//     美股 "pc"），否则留 0 让下游按"未知"处理；
//  3. TimeMs=0 表示源未带事件时间，装配层用落地时间兜底（绝不当"更旧"处理）。
//
// English: shared tick atom + numeric helpers for the Binance WS feed. Prices never get guessed
// (string->float or 0), PrevClose stays separated from Close per §P1-5, and 0 event time means
// "source didn't say" (the feed falls back to arrival time).
package data

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 现货/美股流型（BinanceQuoteFeedOptions.StreamKind 取值）。
const (
	SpotStreamMiniTicker = "miniTicker" // <sym>@miniTicker：1s 一推，OCHL+量（缺省）
	SpotStreamTicker     = "ticker"     // <sym>@ticker：24h 口径，含涨跌幅
	SpotStreamBookTicker = "bookTicker" // <sym>@bookTicker：仅最优买卖价

	EquityStreamPrice = "price" // 全市场最新价快照（服务端 3s 轮询，PLAN §2.4）
	EquityStreamQuote = "quote" // {SYM}@quote：逐票 bp/ap（≤200ms）
)

// binanceTick 两市场共用的最小行情原子（解析层产物 → StockInfo 装配的中间态）。
// Bid/Ask 在本仓库 StockInfo 无对应字段，只留在 tick 层供价差/滑点估算（SpreadBps 出口）。
type binanceTick struct {
	Symbol    string  // 统一大写后的标的（BTCUSDT / AAPL）= MarketSnapshot 的 key
	Price     float64 // 最新价（现货 c；bookTicker/仅盘口时取买卖中间价）
	Bid       float64 // 买价（bookTicker/@quote 才有）
	Ask       float64 // 卖价
	Open      float64 // 24h/当日开盘
	High      float64 // 24h/当日最高
	Low       float64 // 24h/当日最低
	PrevClose float64 // 参考价（>0 才可信，§P1-5 口径）
	Volume    float64 // 基础量（币→股）
	Amount    float64 // 计价额（USDT/USD）
	ChangePct float64 // 涨跌幅（%，现货 @ticker 的 P）
	TimeMs    int64   // 事件时间（epoch 毫秒 UTC；0=源未给，落地时间兜底）
}

// valid tick 是否可用：必须有 symbol 且价格为正（0 价=停牌/无成交，宁可不注入）。
func (t binanceTick) valid() bool {
	return strings.TrimSpace(t.Symbol) != "" && t.Price > 0
}

// ParseBinanceCombinedStream 拆组合流信封 {"stream":"...","data":{...}}。
// 裸流（PLAN §2.4 "/ws/<stream> 裸 payload"，含数组形态）时 stream=""、data=原文。
func ParseBinanceCombinedStream(raw []byte) (stream string, data []byte, err error) {
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "{") {
		return "", raw, nil
	}
	var env struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	if e := json.Unmarshal(raw, &env); e != nil {
		return "", nil, fmt.Errorf("组合流信封解析失败: %w", e)
	}
	if env.Stream == "" || len(env.Data) == 0 {
		return env.Stream, raw, nil // 无信封：单票裸 JSON，原文即业务 payload
	}
	return env.Stream, env.Data, nil
}

// jsonFloat 币安字符串数值 → float64（空/非法=0，绝不猜值）。
func jsonFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// anyFloat JSON 值（number/string/json.Number）→ float64；非数值返回 0。
func anyFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0
		}
		return f
	case string:
		return jsonFloat(n)
	default:
		return 0
	}
}

// firstPositiveAny 返回第一个正数（多键名兼容用；全非正则 0）。
func firstPositiveAny(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
