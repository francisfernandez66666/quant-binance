// 文件职责：data.binance.vision 归档 CSV 的**表头识别与单行映射**（Phase 3 第 1 项，PLAN §2.8）。
// 从 binance_vision_csv.go 拆出，只放"列名怎么认、一行怎么变成 KLine"这两件纯函数，
// 便于用表驱动用例覆盖不同年份/市场的列名变体（改键名不用碰解压层）。
package data

import (
	"strconv"
	"strings"
)

// looksLikeVisionHeader 首行是否表头：常见列名直接命中；否则"首列不能转成数字"即判表头。
func looksLikeVisionHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	first := strings.ToLower(strings.TrimSpace(row[0]))
	switch first {
	case "open_time", "opentime", "start", "start_at", "date", "time_key":
		return true
	}
	if _, err := strconv.ParseFloat(first, 64); err == nil {
		return false
	}
	return true
}

// visionHeaderColumns 按表头名定位列；关键列缺失则退回缺省列序（宁可错序也不猜出半截数据）。
func visionHeaderColumns(header []string) visionColumns {
	col := visionColumns{OpenTime: -1, Open: -1, High: -1, Low: -1, Close: -1, Volume: -1, QuoteVol: -1}
	for i, name := range header {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "open_time", "opentime", "start", "start_at", "date", "time_key":
			if col.OpenTime < 0 {
				col.OpenTime = i
			}
		case "open":
			col.Open = i
		case "high":
			col.High = i
		case "low":
			col.Low = i
		case "close":
			col.Close = i
		case "volume", "asset_volume", "base_asset_volume":
			col.Volume = i
		case "quote_volume", "qav", "quote_asset_volume":
			col.QuoteVol = i
		}
	}
	if col.OpenTime < 0 || col.Open < 0 || col.Close < 0 {
		return visionDefaultColumns()
	}
	if col.High < 0 {
		col.High = col.Open + 1
	}
	if col.Low < 0 {
		col.Low = col.Open + 2
	}
	return col
}

// visionRowToKLine 单行 → KLine。列越界按空值处理；时间/收价非法或高低倒挂=脏行（false）。
// 归档里部分品种不返回 high/low（只有 OHLC 四价的年景），此时用 close 兜成"无振幅"，
// 而不是丢弃整根——但只在 close 合法时兜，且注释写明这是源缺列而非实测量。
func visionRowToKLine(row []string, idx visionColumns, unit BinanceEpochUnit) (KLine, bool) {
	at := func(i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	ts, err := strconv.ParseInt(at(idx.OpenTime), 10, 64)
	if err != nil {
		return KLine{}, false
	}
	u := unit
	if u == "" {
		u = BinanceEpochVision
	}
	k := KLine{
		Date:   normalizeBinanceEpoch(ts, u),
		Open:   jsonFloat(at(idx.Open)),
		High:   jsonFloat(at(idx.High)),
		Low:    jsonFloat(at(idx.Low)),
		Close:  jsonFloat(at(idx.Close)),
		Volume: jsonFloat(at(idx.Volume)),
		Amount: jsonFloat(at(idx.QuoteVol)),
	}
	if k.High <= 0 {
		k.High = k.Close
	}
	if k.Low <= 0 {
		k.Low = k.Close
	}
	return k, klineRowSane(k)
}
