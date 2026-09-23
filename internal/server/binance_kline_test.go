// binance_kline_test.go — §BINANCE-P5 新市场 K 线端点（GET /api/binance/kline）参数矩阵行为锁。
// 钉死四件事：
//
//	① 两链分轨：market 归一后是 CN（含缺省/未知值）一律 400——CN K 线只许走 /api/kline，
//	   这条口一旦放过，前端就能拿币安链数据画 A 股图（GAP §G-8 混轨事故）；
//	② count 闸：缺省 180、上限 500、非法值回落缺省（与 /api/kline 同口径，防一次拉穿研究库）；
//	③ 空态如实 []（不是 null、不是 200+error）：前端 lightweight-charts 要的是「有轴无图」；
//	④ 出参形状：date 统一 YYYY-MM-DD、价格两位裁剪、微定价（<1）保留 8 位、量取整、时间升序尾段。
//
// 未接入研究库 = 503 fail-closed（沿用 binance_api_test 的白屏教训）。
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quant-trading-v2/internal/store"
)

// seedDailySeries 往研究库 daily 表灌一根标的的日线：日期自 start 起逐日 +1（自然日，
// 加密 7×24 无交易日历；美股构造出的周末日期对裁剪断言无影响），OHLC 按 base 逐根 +1 递增，
// 便于断言「取的是尾段最近 N 根」而不必比对整表。
func seedDailySeries(t *testing.T, db *store.DB, tsCode string, start time.Time, n int, base float64) {
	t.Helper()
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i).Format("20060102")
		p := base + float64(i)
		rows = append(rows, map[string]any{
			"ts_code": tsCode, "trade_date": d,
			"open": p, "high": p + 0.5, "low": p - 0.4, "close": p + 0.2,
			"vol": 1000.25 + float64(i), "amount": p * 1000,
		})
	}
	if _, err := db.InsertRows("daily", store.TableColumns("daily"), rows); err != nil {
		t.Fatalf("seed daily %s: %v", tsCode, err)
	}
}

// getKline 直调 handler 打一次端点（绕开 mux——鉴权由路由行保证，见 server.go）。
func getKline(s *Server, query string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.handleBinanceKline(rr, httptest.NewRequest(http.MethodGet, "/api/binance/kline"+query, nil))
	return rr
}

// decodeKline 解出参数组（必须是 JSON 数组，空态亦须是 []）。
func decodeKline(t *testing.T, rr *httptest.ResponseRecorder) []binanceKlineBar {
	t.Helper()
	var out []binanceKlineBar
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("出参非数组/解码失败: %v body=%s", err, rr.Body.String())
	}
	return out
}

// TestBinanceKlineRejectsCN market=CN / 缺省 / 未知值（Normalize 落 CN）/ 空 code 全拒 400。
func TestBinanceKlineRejectsCN(t *testing.T) {
	s, db, _ := newTestResearchServer(t)
	seedDailySeries(t, db, "600519.SH", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 3, 100)

	cases := []struct{ name, query string }{
		{"显式 CN", "?market=CN&code=600519.SH"},
		{"缺 market", "?code=BTCUSDT"},
		{"空 market", "?market=&code=BTCUSDT"},
		{"未知值归一为 CN", "?market=A%E8%82%A1&code=BTCUSDT"},
		{"小写 cn", "?market=cn&code=600519.SH"},
		{"缺 code", "?market=CRYPTO"},
		{"空白 code", "?market=CRYPTO&code=%20"},
	}
	for _, tc := range cases {
		rr := getKline(s, tc.query)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: 期望 400，实际 %d body=%s", tc.name, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"error"`) {
			t.Fatalf("%s: 400 应带 {\"error\":…}，body=%s", tc.name, rr.Body.String())
		}
	}
}

// TestBinanceKlineCountClamp count 缺省 180 / 上限 500 / 非法值回落缺省 / 显式小值生效。
func TestBinanceKlineCountClamp(t *testing.T) {
	s, db, _ := newTestResearchServer(t)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDailySeries(t, db, "BTCUSDT", start, 600, 40000)
	// 另一只票只灌 30 根：验证「库里根数 < count」时如实返回全部，不补空行。
	seedDailySeries(t, db, "AAPL.US", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 30, 200)

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"缺省 180", "?market=CRYPTO&code=BTCUSDT", 180},
		{"上限 500（请求 9999）", "?market=CRYPTO&code=BTCUSDT&count=9999", 500},
		{"count=0 回落缺省", "?market=CRYPTO&code=BTCUSDT&count=0", 180},
		{"count=-5 回落缺省", "?market=CRYPTO&code=BTCUSDT&count=-5", 180},
		{"count=abc 回落缺省", "?market=CRYPTO&code=BTCUSDT&count=abc", 180},
		{"显式 5 生效", "?market=CRYPTO&code=BTCUSDT&count=5", 5},
		{"count=500 命中上限如实 500 根", "?market=CRYPTO&code=BTCUSDT&count=500", 500},
	}
	for _, tc := range cases {
		rr := getKline(s, tc.query)
		if rr.Code != 200 {
			t.Fatalf("%s: HTTP %d body=%s", tc.name, rr.Code, rr.Body.String())
		}
		got := decodeKline(t, rr)
		if len(got) != tc.want {
			t.Fatalf("%s: 根数=%d 期望 %d", tc.name, len(got), tc.want)
		}
		// 尾段语义：最后一根恒为库里最新日（2024-01-01 + 599 天）
		last := start.AddDate(0, 0, 599).Format("2006-01-02")
		if got[len(got)-1].Date != last {
			t.Fatalf("%s: 末根=%s 期望最新日 %s（必须取最近 N 根，不是最旧 N 根）", tc.name, got[len(got)-1].Date, last)
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Date >= got[i].Date {
				t.Fatalf("%s: 第 %d 根日期非升序 %s -> %s", tc.name, i, got[i-1].Date, got[i].Date)
			}
		}
	}

	// count=5 的裁剪位：首根应是 start+595 天（最近 5 根）
	got := decodeKline(t, getKline(s, "?market=CRYPTO&code=BTCUSDT&count=5"))
	if first := start.AddDate(0, 0, 595).Format("2006-01-02"); got[0].Date != first {
		t.Fatalf("count=5 首根=%s 期望 %s", got[0].Date, first)
	}

	// 根数不足 count：如实 30 根、末根是该票库内最新日（2026-08-01 + 29 天），不补空行
	short := decodeKline(t, getKline(s, "?market=US&code=AAPL.US&count=400"))
	if len(short) != 30 || short[29].Date != "2026-08-30" {
		t.Fatalf("根数不足时应回 30 根且末根最新，实际 %d 根 末=%s", len(short), short[len(short)-1].Date)
	}
}

// TestBinanceKlineEmptyIsArray 无数据如实 []（US 库里没这根票 / 库里有别的键却问这根）：
// 空态必须是空数组而不是 null/500——前端按「有轴无图」渲染。
func TestBinanceKlineEmptyIsArray(t *testing.T) {
	s, db, _ := newTestResearchServer(t)
	seedDailySeries(t, db, "BTCUSDT", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), 4, 60000)

	for _, q := range []string{
		"?market=US&code=NOPE.US",
		"?market=CRYPTO&code=DOGEUSDT",
	} {
		rr := getKline(s, q)
		if rr.Code != 200 {
			t.Fatalf("%s: 无数据应 200，实际 %d body=%s", q, rr.Code, rr.Body.String())
		}
		if body := strings.TrimSpace(rr.Body.String()); body != "[]" {
			t.Fatalf("%s: 空态出参必须是 [] 实际 %s", q, body)
		}
	}

	// 研究库未接入 → 503 fail-closed（绝不 200+error 让前端把错误体当数据渲染）
	rr := getKline(&Server{}, "?market=CRYPTO&code=BTCUSDT")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("未接入研究库应 503，实际 %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestBinanceKlineOutputShape 出参形状与裁剪口径：market/code 大小写不敏感、
// date=YYYY-MM-DD、常规价两位、微定价 8 位、量取整；US 点分键独立可读。
func TestBinanceKlineOutputShape(t *testing.T) {
	s, db, _ := newTestResearchServer(t)
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	seedDailySeries(t, db, "AAPL.US", start, 3, 230.123)
	// 低价币：微定价（<1）不得被两位压成 0
	if _, err := db.InsertRows("daily", store.TableColumns("daily"), []map[string]any{
		{"ts_code": "SHIBUSDT", "trade_date": "20260923", "open": 0.0000123456, "high": 0.0000129999,
			"low": 0.0000120001, "close": 0.0000127777, "vol": 0.4, "amount": 1},
	}); err != nil {
		t.Fatalf("seed micro: %v", err)
	}

	rr := getKline(s, "?market=us&code=aapl.us&count=3")
	if rr.Code != 200 {
		t.Fatalf("HTTP %d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeKline(t, rr)
	if len(got) != 3 {
		t.Fatalf("根数=%d 期望 3", len(got))
	}
	if got[2].Date != "2026-09-22" {
		t.Fatalf("date 应为 YYYY-MM-DD，实际 %s", got[2].Date)
	}
	// 230.123+2 = 232.123 → 两位 232.12；close=+0.2 → 232.32；vol=1002.25 → 1002
	if got[2].Open != 232.12 || got[2].Close != 232.32 {
		t.Fatalf("价格两位裁剪失配: %+v", got[2])
	}
	if got[2].Volume != 1002 {
		t.Fatalf("量应取整 1002，实际 %v", got[2].Volume)
	}
	if got[0].High <= got[0].Low || got[0].Open <= 0 {
		t.Fatalf("OHLC 字段串了: %+v", got[0])
	}

	micro := decodeKline(t, getKline(s, "?market=CRYPTO&code=SHIBUSDT"))
	if len(micro) != 1 {
		t.Fatalf("微定价根数=%d 期望 1", len(micro))
	}
	if micro[0].Open != 0.00001235 || micro[0].Close != 0.00001278 {
		t.Fatalf("低价币应保留 8 位小数，实际 %+v", micro[0])
	}
	if micro[0].Volume != 0 {
		t.Fatalf("微量应取整为 0，实际 %v", micro[0].Volume)
	}
}
