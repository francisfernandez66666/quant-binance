// 文件职责：币安 24h 快照 REST 出口（Phase 3 第 1 项，PLAN §6.9 静默兜底 + §2.8 端点）。
// WS 静默超限（>90s）后消费方不该拿旧价下单，而是轮询一次 REST 快照——LatestQuote 就是这个
// 兜底入口，返回 **binanceTick**（与 WS 解析层同一原子类型，两通道口径不分叉，装配层可直接
// 喂给 feed 的 pending 缓冲）。
//
// 反假绿：响应里没有正价时返回 error，不把 0 价 tick 交给上层（0 价进快照会伪装成"平价停牌"，
// 比一次可观测的失败更危险）。
package data

import (
	"context"
	"fmt"
	"net/url"
)

// Ticker24hr /api/v3/ticker/24hr 的强类型视图（现货口径；美股键名未实测时走 LatestQuote 的
// 宽容 map 解析，两者并存是有意的：见 GAP §G-1）。
type Ticker24hr struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"lastPrice"`
	Open      float64 `json:"openPrice"`
	High      float64 `json:"highPrice"`
	Low       float64 `json:"lowPrice"`
	PrevClose float64 `json:"prevClosePrice"`
	Volume    float64 `json:"volume"`
	Amount    float64 `json:"quoteVolume"`
	ChangePct float64 `json:"priceChangePercent"`
	CloseMs   int64   `json:"closeTime"`
}

// LatestQuote 拉单票 24h 快照并转成 binanceTick（§6.9 轮询兜底入口）。
func (c *BinanceRestClient) LatestQuote(ctx context.Context, symbol string) (binanceTick, error) {
	sym := normalizeBinanceSymbol(symbol)
	if sym == "" {
		return binanceTick{}, fmt.Errorf("binance ticker: symbol 为空")
	}
	// 响应先进宽容 map 再按族取值：现货 24h ticker 长字段名为主、WS 短键兜底（美股形状待 Q5 实测收敛）。
	var raw map[string]any
	if err := c.getJSON(ctx, c.tickerPath(), url.Values{"symbol": {sym}}, &raw); err != nil {
		return binanceTick{}, err
	}
	t := binanceTick{
		Symbol: sym,
		// ChangePct 用 signedField：涨跌为负是常态，不能走"只取正数"的 fieldNum。
		Price:     fieldNum(raw, "lastPrice", "C", "c", "p"),
		Open:      fieldNum(raw, "openPrice", "O", "o"),
		High:      fieldNum(raw, "highPrice", "H", "h"),
		Low:       fieldNum(raw, "lowPrice", "L", "l"),
		PrevClose: fieldNum(raw, "prevClosePrice", "pc"),
		Volume:    fieldNum(raw, "volume", "V", "v"),
		Amount:    fieldNum(raw, "quoteVolume", "Q", "q"),
		TimeMs:    int64(fieldNum(raw, "closeTime", "E")),
		ChangePct: signedField(raw, "priceChangePercent", "P"),
	}
	if !t.valid() {
		return binanceTick{}, fmt.Errorf("binance ticker %s: 响应无有效最新价（0 价不注入）", sym)
	}
	return t, nil
}

// tickerPath 24h 快照端点前缀（美股路径未实测，基址可注入覆盖）。
func (c *BinanceRestClient) tickerPath() string {
	if c.Equity {
		return "/sapi/v1/equity/ticker/24hr"
	}
	return "/api/v3/ticker/24hr"
}

// signedField 取"允许为负"的数值字段（涨跌幅会为负，套不进 fieldNum 的正数口径）。
// 判存在性用两次逗号断言：键缺失=0（未知），键存在但值为 0=真 0。
func signedField(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		v, present := m[k]
		if !present {
			continue
		}
		if s, ok := v.(string); ok {
			return jsonFloat(s)
		}
		return anyFloat(v)
	}
	return 0
}
