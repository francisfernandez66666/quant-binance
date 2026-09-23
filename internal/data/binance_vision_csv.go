// 文件职责：data.binance.vision 归档 zip 的**解压与 CSV 表头识别**（Phase 3 第 1 项，PLAN §2.8）。
// 归档内是一份 CSV（首列 openTime，其后 open/high/low/close/volume/closeTime/quoteVolume…），
// 但**列序与是否带表头随市场/年份不同**（GAP §G-1 未全部实测），故：
//  1. 认得出表头就按表头定位列（不靠下标猜），认不出退回"前 8 列即 OHLCV"缺省序；
//  2. 时间口径由调用方显式声明（unit），空则按 PLAN 的 vision 微秒口径；
//  3. 脏行与同日重复行丢弃（跨月分片重叠会给出同一天两根 K线，留先到的那根，绝不静默追加）。
package data

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// parseBinanceVisionZip 解压内存中的 zip 并解析其中的 CSV 分片。
func parseBinanceVisionZip(raw []byte, unit BinanceEpochUnit) ([]KLine, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("binance vision 解压失败: %w", err)
	}
	var out []KLine
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return nil, fmt.Errorf("binance vision 打开 %s 失败: %w", f.Name, oerr)
		}
		rows, rerr := readAllCSV(io.LimitReader(rc, 64<<20))
		_ = rc.Close()
		if rerr != nil {
			return nil, fmt.Errorf("binance vision CSV 解析失败 %s: %w", f.Name, rerr)
		}
		out = append(out, parseBinanceVisionCSV(rows, unit)...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("binance vision: 归档内无可解析 K线（列序或时间口径与预期不符）")
	}
	return out, nil
}

// parseBinanceVisionCSV 按表头（或缺省列序）把 CSV 行转成 K线。
func parseBinanceVisionCSV(rows [][]string, unit BinanceEpochUnit) []KLine {
	if len(rows) == 0 {
		return nil
	}
	idx := visionDefaultColumns()
	start := 0
	if looksLikeVisionHeader(rows[0]) {
		idx = visionHeaderColumns(rows[0])
		start = 1
	}
	out := make([]KLine, 0, len(rows)-start)
	seen := make(map[string]bool, len(rows))
	for _, r := range rows[start:] {
		k, ok := visionRowToKLine(r, idx, unit)
		if !ok {
			continue
		}
		key := k.Date.Format(timeLayoutVisionDay)
		if seen[key] {
			continue // 同日重复：保留先到，避免同一天两根 K线污染 MA/RSI
		}
		seen[key] = true
		out = append(out, k)
	}
	return out
}

// visionColumns 列下标（-1=该列不存在）。
type visionColumns struct {
	OpenTime, Open, High, Low, Close, Volume, QuoteVol int
}

// timeLayoutVisionDay 归档日去重键（UTC 日）。
const timeLayoutVisionDay = "2006-01-02"

// visionDefaultColumns 缺省列序：openTime,open,high,low,close,volume,closeTime,quoteVolume。
func visionDefaultColumns() visionColumns {
	return visionColumns{OpenTime: 0, Open: 1, High: 2, Low: 3, Close: 4, Volume: 5, QuoteVol: 7}
}

// readAllCSV 逐行读（encoding/csv 无 ReadAll；EOF 是正常结束不算错误）。
func readAllCSV(r io.Reader) ([][]string, error) {
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1 // 归档里偶有尾列缺失的行，按行长容忍后在映射层再判
	var out [][]string
	for {
		row, err := rd.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, row)
	}
}
