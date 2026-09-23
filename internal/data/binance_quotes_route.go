// 文件职责：币安行情 feed 的**流名拼装与解析器路由**（PLAN §2.4 连接方式一节）。
//
//	订阅 URL：组合流 /stream?streams=a@miniTicker/b@miniTicker（StreamURLCombined 在
//	binance_ws.go）；美股 price 是**全市场单流**，故 Symbols 只作注入侧过滤白名单，
//	不参与流名（见 binanceStreamNames 的 US 分支）。
//	解析路由：市场 + StreamKind 两个维度决定用哪个解析器（现货三型 / 美股两型），
//	未知 StreamKind 一律回落该市场缺省型——**不猜流型**，宁可给缺省也不给随机行为。
//	信封剥离：现货单票流与美股流都可能是 {stream,data} 组合形态，故两条路径共用
//	ParseBinanceCombinedStream 先剥一层；裸 payload 时该函数原样透传。
//
// English: stream-name building + parser routing for the Binance quote feed (spot three stream
// types, equity price/@quote, combined-stream envelope stripping). Unknown StreamKind falls back
// to each market's default rather than guessing.
package data

import (
	"fmt"
	"strings"
)

// binanceTickerParser 按市场+流型选内置解析器（未知流型回落各自缺省型）。
func binanceTickerParser(market, kind string) func([]byte) ([]binanceTick, error) {
	key := strings.ToLower(strings.TrimSpace(kind))
	if market == "CRYPTO" {
		switch key {
		case SpotStreamTicker:
			return singleTickerParser(parseSpotTicker)
		case SpotStreamBookTicker:
			return singleTickerParser(parseSpotBookTicker)
		default:
			return singleTickerParser(parseSpotMiniTicker)
		}
	}
	equity := parseEquityPrice
	if key == EquityStreamQuote {
		equity = parseEquityQuote
	}
	// 美股流也可能是组合流信封形态（/stream?streams=price）→ 统一先剥信封再解析。
	return func(payload []byte) ([]binanceTick, error) {
		_, data, err := ParseBinanceCombinedStream(payload)
		if err != nil {
			return nil, err
		}
		return equity(data)
	}
}

// singleTickerParser 把单票解析器包成多 tick 返回形态，并统一剥组合流信封；
// 不可用 tick（symbol 空/价非正）在此判 error，由 feed 计数 + 节流日志，不影响通道存活。
func singleTickerParser(one func([]byte) (binanceTick, error)) func([]byte) ([]binanceTick, error) {
	return func(payload []byte) ([]binanceTick, error) {
		_, data, err := ParseBinanceCombinedStream(payload)
		if err != nil {
			return nil, err
		}
		t, perr := one(data)
		if perr != nil {
			return nil, perr
		}
		if !t.valid() {
			return nil, fmt.Errorf("tick 不可用（symbol=%q price=%.8g）", t.Symbol, t.Price)
		}
		return []binanceTick{t}, nil
	}
}

// binanceStreamNames 生成订阅流名（现货小写 symbol@kind；美股 price 为全市场单流）。
// 美股 {SYM}@quote 形态的流名按官方口径用**大写** symbol（§2.4 表格 {SYMBOL}@quote），
// 与现货小写不同——这个差异是字节级契约，别"顺手统一"。
func binanceStreamNames(market, kind string, symbols []string) []string {
	key := strings.TrimSpace(kind)
	if market == "US" {
		if key == "" {
			key = EquityStreamPrice
		}
		if key == EquityStreamPrice {
			return []string{EquityStreamPrice} // 全市场价快照：单流即覆盖 universe
		}
		out := make([]string, 0, len(symbols))
		for _, s := range symbols {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, strings.ToUpper(s)+"@"+key)
			}
		}
		return out
	}
	if key == "" {
		key = SpotStreamMiniTicker
	}
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, strings.ToLower(s)+"@"+key)
		}
	}
	return out
}
