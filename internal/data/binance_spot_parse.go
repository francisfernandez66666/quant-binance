// 文件职责：币安**现货**行情 WS payload 解析器（Phase 3 行情面，PLAN §2.4/§2.8/§7）。
// 三种流型统一解析成 binanceTick（类型见 binance_tick.go）：
//
//	<sym>@miniTicker（缺省，1s 一推，OCHL+量）、<sym>@ticker（24h 口径，含涨跌幅 P）、
//	<sym>@bookTicker（最优买卖价，最省带宽）。
//
// 纯函数、无 IO —— 本仓库 go.mod 零 websocket 依赖（见 binance_ws.go 头注释），解析层
// 因此可用表驱动单测全量覆盖，真 socket 只在装配层注入 WsDialFunc 之后才需要。
//
// 口径提醒（§P1-5 昨收分离）：miniTicker 无昨收 → PrevClose 留 0，其 ChangePct 只是"相对
// 24h 开盘"的近似展示值；需可信参考价的下游请用 @ticker（其 "o" 即 24h 前开盘价）。
//
// English: pure parsers for the three spot market streams (miniTicker / 24hr ticker /
// bookTicker) into the shared binanceTick; PrevClose is filled only where the stream really
// carries a reference price.
package data

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseSpotMiniTicker 解析 @miniTicker / @24hrMiniTicker（字段 e,E,s,o,c,h,l,v,Q）。
func parseSpotMiniTicker(raw []byte) (binanceTick, error) {
	var m struct {
		T int64  `json:"E"`
		S string `json:"s"`
		O string `json:"o"`
		C string `json:"c"`
		H string `json:"h"`
		L string `json:"l"`
		V string `json:"v"`
		Q string `json:"Q"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return binanceTick{}, fmt.Errorf("miniTicker 解析失败: %w", err)
	}
	t := binanceTick{
		Symbol: strings.ToUpper(m.S), Price: jsonFloat(m.C), Open: jsonFloat(m.O),
		High: jsonFloat(m.H), Low: jsonFloat(m.L), Volume: jsonFloat(m.V),
		Amount: jsonFloat(m.Q), TimeMs: m.T,
	}
	if t.Price > 0 && t.Open > 0 {
		t.ChangePct = (t.Price/t.Open - 1) * 100 // 近似口径：相对 24h 开盘（见文件头提醒）
	}
	return t, nil
}

// parseSpotTicker 解析 @ticker（24hr 统计：o=24h 前开盘、c=最新、P=百分比涨跌、Q=计价额）。
func parseSpotTicker(raw []byte) (binanceTick, error) {
	var m struct {
		T int64  `json:"E"`
		S string `json:"s"`
		O string `json:"o"`
		C string `json:"c"`
		H string `json:"h"`
		L string `json:"l"`
		V string `json:"v"`
		Q string `json:"Q"`
		P string `json:"P"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return binanceTick{}, fmt.Errorf("ticker 解析失败: %w", err)
	}
	t := binanceTick{
		Symbol: strings.ToUpper(m.S), Price: jsonFloat(m.C), Open: jsonFloat(m.O),
		High: jsonFloat(m.H), Low: jsonFloat(m.L), Volume: jsonFloat(m.V),
		Amount: jsonFloat(m.Q), ChangePct: jsonFloat(m.P), TimeMs: m.T,
	}
	if t.Open > 0 {
		// 24hr ticker 的 "o" 是本流唯一可信的参考收盘价（§P1-5：PrevClose 与 Close 分离）。
		t.PrevClose = t.Open
	}
	return t, nil
}

// parseSpotBookTicker 解析 @bookTicker（s/b/a = symbol/bidPx/askPx；档位量 B/A 不消费）。
// 无最新价 → Price 取买卖中间价，Bid/Ask 原样保留供价差/滑点估算（BinanceQuoteFeed.SpreadBps）。
func parseSpotBookTicker(raw []byte) (binanceTick, error) {
	var m struct {
		S string `json:"s"`
		B string `json:"b"`
		A string `json:"a"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return binanceTick{}, fmt.Errorf("bookTicker 解析失败: %w", err)
	}
	t := binanceTick{Symbol: strings.ToUpper(m.S), Bid: jsonFloat(m.B), Ask: jsonFloat(m.A)}
	switch {
	case t.Bid > 0 && t.Ask > 0:
		t.Price = (t.Bid + t.Ask) / 2
	case t.Bid > 0:
		t.Price = t.Bid
	case t.Ask > 0:
		t.Price = t.Ask
	}
	return t, nil
}
