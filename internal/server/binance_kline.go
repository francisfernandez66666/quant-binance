// 文件职责：§BINANCE-P5（新市场页面积试点）历史 K 线读取端点——
// GET /api/binance/kline?market=US|CRYPTO&code=BTCUSDT|AAPL.US&count=N：
// 从研究库 daily 表（scripts/download_binance_klines.py 落库，ts_code 即币安裸符号 BTCUSDT /
// 美股点分键 AAPL.US，trade_date 为 YYYYMMDD 字符串日键）取该标的最近 N 根日 K，
// 出参数组 [{date,open,high,low,close,volume}]（日期统一 YYYY-MM-DD、价格两位裁剪，
// 对齐 handlers_fix.go fixKLine 的出参风格）。
//
// 边界（两链分轨，CN 零回归）：
//
//	· 只收 US/CRYPTO——market 经 data.NormalizeMarketKey 归一后若是 CN（含空值/未知值），
//	  直接 400：CN K 线仍走 /api/kline（DataCoordinator/新浪链），数据源与口径都不同，
//	  两链绝不在此合流，前端也不许拿本端点喂 CN 图；
//	· 纯读研究库已落档数据，**不做实时兜底**：无数据如实回 []（空态语义=前端有轴无图），
//	  未接入研究库才 503（fail-closed，绝不 200+error，见 binance_api_test 白屏教训）。
//
// English: read-only daily K-line endpoint for the new markets (US/CRYPTO) backed by the
// research DB `daily` table; CN is rejected (400) because CN klines stay on /api/kline.
// Empty result is an honest `[]` (no live fallback); unwired DB yields 503.
package server

import (
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"quant-trading-v2/internal/data"
)

// binanceKlineBar 新市场 K 线单根（前端 lightweight-charts 直接消费的精简形状）。
// 与 fixKLine 同风格但砍掉 amount——专业图只画蜡烛 + 量柱两列，成交额留给付费面。
// （binanceKlineBar is one trimmed daily bar for the new-market pro chart.）
type binanceKlineBar struct {
	Date   string  `json:"date"`   // 交易日（2006-01-02，UTC 日）
	Open   float64 `json:"open"`   // 开盘价
	High   float64 `json:"high"`   // 最高价
	Low    float64 `json:"low"`    // 最低价
	Close  float64 `json:"close"`  // 收盘价
	Volume float64 `json:"volume"` // 成交量（股 / 枚，取整）
}

// 取数根数口径：缺省 180（半年量级）、上限 500（与 /api/kline 同闸）。
// English: default 180 bars, hard cap 500 (same cap as /api/kline).
const (
	binanceKlineDefaultCount = 180
	binanceKlineMaxCount     = 500
	// binanceKlineMaxDate 区间右界哨兵：daily.trade_date 是 YYYYMMDD 字符串，
	// "99999999" 恒大于任何真实日期，等价「一直取到最后一根」。
	binanceKlineMaxDate = "99999999"
)

// handleBinanceKline 处理 GET /api/binance/kline：market=US|CRYPTO、code 必填、count 可选。
// 只读研究库 daily 表最近 N 根，输出时间升序数组（lightweight-charts 要求升序序列）。
func (s *Server) handleBinanceKline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// market 归一后只收 US/CRYPTO：CN（含空值/未知值——NormalizeMarketKey 一律落 CN）直接 400 分轨。
	market := data.NormalizeMarketKey(q.Get("market"))
	if market != "US" && market != "CRYPTO" {
		writeError(w, http.StatusBadRequest, "market 仅支持 US/CRYPTO（CN K 线请走 /api/kline，两链分轨）")
		return
	}

	// code：库内主键原样查（BTCUSDT / AAPL.US），大小写不敏感故先 ToUpper；不做实时兜底。
	code := strings.ToUpper(strings.TrimSpace(q.Get("code")))
	if code == "" {
		writeError(w, http.StatusBadRequest, "缺少 code 参数")
		return
	}

	count := binanceKlineDefaultCount
	if raw := q.Get("count"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			count = n
		}
	}
	if count > binanceKlineMaxCount {
		count = binanceKlineMaxCount
	}

	db := s.researchDB
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, "research db not available")
		return
	}

	// 区间取数 + 尾部裁剪：RawBars 已按 trade_date 升序返回，取最近 N 根即切尾段。
	bars, err := db.RawBars(code, "", binanceKlineMaxDate)
	if err != nil {
		// 读库失败与「库里真没这根票」在前端不可区分（都该是有轴无图），故只记日志回空数组。
		log.Printf("[server] /api/binance/kline %s(%s) 读取 daily 失败: %v", code, market, err)
		writeJSON(w, 200, []binanceKlineBar{})
		return
	}
	// 尾部裁剪到 count 根后逐根投影为响应结构（字段口径与 CN /api/kline 同形）。
	if n := len(bars); n > count {
		bars = bars[n-count:]
	}
	out := make([]binanceKlineBar, 0, len(bars))
	for _, b := range bars {
		out = append(out, binanceKlineBar{
			Date:   binanceTradeDateISO(b.Date),
			Open:   rPrice(b.Open),
			High:   rPrice(b.High),
			Low:    rPrice(b.Low),
			Close:  rPrice(b.Close),
			Volume: r0(b.Vol),
		})
	}
	writeJSON(w, 200, out)
}

// rPrice 价格裁剪：常规价位沿用 fixKLine 的两位口径（r2）；低于 1 的微定价（低价币）
// 保留 8 位小数——否则整条蜡烛被压成 0.00/0.01 阶梯，图就没得读了。
// English: two decimals like fixKLine, except sub-unit micro prices keep eight decimals.
func rPrice(v float64) float64 {
	if math.Abs(v) > 0 && math.Abs(v) < 1 {
		return math.Round(v*1e8) / 1e8
	}
	return r2(v)
}

// binanceTradeDateISO 把库内 YYYYMMDD 字符串日键转成出参 YYYY-MM-DD；
// 长度不合（脏数据/空串）原样透传，由前端 toChartSeries 的日期校验负责丢弃。
// （binanceTradeDateISO converts the stored YYYYMMDD key into YYYY-MM-DD; odd lengths pass through.）
func binanceTradeDateISO(s string) string {
	s = strings.TrimSpace(s)
	if len(s) != 8 {
		return s
	}
	return s[0:4] + "-" + s[4:6] + "-" + s[6:8]
}
