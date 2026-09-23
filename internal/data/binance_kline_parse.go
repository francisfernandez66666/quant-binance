// 文件职责：币安 K线**行级解析**（Phase 3 第 1 项，PLAN §2.8）。
// 与 HTTP 层（binance_kline.go）、口径层（binance_time.go）拆开，让"脏行不冒充有效 K线"
// 这条纪律单独可测：任何一行缺时间、缺正价或高低倒挂都返回 false，由调用方丢弃并计数，
// 绝不填 0 造出一根"看起来有"的 K线（策略侧 MA/RSI 会被 0 价直接带偏）。
package data

// parseBinanceKlineRow 单行 K线 → data.KLine。兼容两种形态：
//   - 现货 array-of-arrays [openTime,"o","h","l","c","v",closeTime,"qv",…]（PLAN §2.8 实测形状）；
//   - 美股对象形态 {"openTime":…,"open":…}（⚠ 未实测，GAP §G-1，故多键名宽容）。
func parseBinanceKlineRow(row any) (KLine, bool) {
	switch v := row.(type) {
	case []any:
		if len(v) < 8 {
			return KLine{}, false
		}
		k := KLine{
			Date:   normalizeBinanceEpoch(int64(anyFloat(v[0])), BinanceEpochMS),
			Open:   anyFloat(v[1]),
			High:   anyFloat(v[2]),
			Low:    anyFloat(v[3]),
			Close:  anyFloat(v[4]),
			Volume: anyFloat(v[5]),
			Amount: anyFloat(v[7]),
		}
		return k, klineRowSane(k)
	case map[string]any:
		k := KLine{
			Date:   normalizeBinanceEpoch(firstInt64Any(v, "openTime", "startTime", "start_time", "t"), BinanceEpochMS),
			Open:   fieldNum(v, "open", "o"),
			High:   fieldNum(v, "high", "h"),
			Low:    fieldNum(v, "low", "l"),
			Close:  fieldNum(v, "close", "c"),
			Volume: fieldNum(v, "volume", "v"),
			Amount: fieldNum(v, "quoteVolume", "qav", "amount"),
		}
		return k, klineRowSane(k)
	default:
		return KLine{}, false
	}
}

// klineRowSane K线行合法性三查：时间已知、收价>0、高低不倒挂。任一不满足=脏行。
func klineRowSane(k KLine) bool {
	return !k.Date.IsZero() && k.Close > 0 && k.High >= k.Low
}
