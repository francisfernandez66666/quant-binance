// 文件职责：§P1-e market_session.go 单测——三市场时段规则矩阵 + 市场键归一化 + CN 包装零漂移。
// English: §P1-e unit tests for market_session.go — per-market session matrix, key normalization,
// and CN wrapper identity against the legacy trade_time predicates.
package data

import (
	"testing"
	"time"

	"quant-trading-v2/internal/cntime"
)

// TestSessionCNIdentity CN 规则完全委托现有实现：多时点与旧谓词逐一等值（零漂移锁）。
func TestSessionCNIdentity(t *testing.T) {
	s := Session("CN")
	if s.Loc() != cntime.Loc {
		t.Fatalf("CN 时区必须是 cntime 北京单源")
	}
	if s.CloseSweepHHMM() != 1452 {
		t.Fatalf("CN 收盘清单应为 1452, got %d", s.CloseSweepHHMM())
	}
	// 覆盖盘前/早盘/午休/午盘/盘后/周末/凌晨的代表性时刻，与旧谓词逐点等值。
	instants := []time.Time{
		time.Date(2026, 9, 23, 8, 40, 0, 0, cntime.Loc),  // 周三盘前
		time.Date(2026, 9, 23, 10, 0, 0, 0, cntime.Loc),  // 早盘
		time.Date(2026, 9, 23, 12, 0, 0, 0, cntime.Loc),  // 午休
		time.Date(2026, 9, 23, 14, 30, 0, 0, cntime.Loc), // 午盘
		time.Date(2026, 9, 23, 16, 0, 0, 0, cntime.Loc),  // 盘后
		time.Date(2026, 9, 26, 10, 0, 0, 0, cntime.Loc),  // 周六
		time.Date(2026, 9, 23, 3, 0, 0, 0, cntime.Loc),   // 凌晨
	}
	for _, ts := range instants {
		if s.Active(ts) != IsActiveSession(ts) {
			t.Fatalf("Active 漂移 @%v: %v vs %v", ts, s.Active(ts), IsActiveSession(ts))
		}
		if s.TradingDay(ts) != IsTradingDay(ts) {
			t.Fatalf("TradingDay 漂移 @%v: %v vs %v", ts, s.TradingDay(ts), IsTradingDay(ts))
		}
	}
}

// TestSessionCrypto 7×24 恒真 + UTC 记账 + 无收盘清单。
func TestSessionCrypto(t *testing.T) {
	s := Session("CRYPTO")
	if s.Loc() != time.UTC {
		t.Fatalf("CRYPTO 记账时区应为 UTC")
	}
	if s.CloseSweepHHMM() != -1 {
		t.Fatalf("CRYPTO 无收盘清单，应返回 -1")
	}
	for _, ts := range []time.Time{
		time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC), // 周六凌晨
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),  // 元旦
		time.Date(2026, 9, 23, 10, 0, 0, 0, cntime.Loc),
	} {
		if !s.Active(ts) || !s.TradingDay(ts) {
			t.Fatalf("CRYPTO 恒开市 @%v", ts)
		}
	}
}

// nyT 纽约墙钟构造器（夏令时/冬令时由时区库自动处理）。
func nyT(t *testing.T, y time.Month, day, hh, mm, wdWant int) time.Time {
	t.Helper()
	l, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("运行环境缺 tzdata: %v", err)
	}
	ts := time.Date(2026, y, day, hh, mm, 0, 0, l)
	if int(ts.Weekday()) != wdWant {
		t.Fatalf("夹具日期星期不符（测试前提破坏）: %v", ts)
	}
	return ts
}

// TestSessionUSRTH RTH 缺省口径：周一至五 09:30–16:00（含头掐尾），周末恒假。
func TestSessionUSRTH(t *testing.T) {
	s := Session("US")
	if s.CloseSweepHHMM() != -1 {
		t.Fatalf("US 收盘清单由回报/日历驱动，应返回 -1")
	}
	// 2026-09-23（周三，EDT）
	if !s.Active(nyT(t, time.September, 23, 9, 30, 3)) {
		t.Fatal("09:30 开盘应活跃（含头）")
	}
	if s.Active(nyT(t, time.September, 23, 9, 29, 3)) {
		t.Fatal("09:29 盘前应不活跃")
	}
	if !s.Active(nyT(t, time.September, 23, 15, 59, 3)) {
		t.Fatal("15:59 应活跃")
	}
	if s.Active(nyT(t, time.September, 23, 16, 0, 3)) {
		t.Fatal("16:00 收盘应不活跃（掐尾）")
	}
	if s.Active(nyT(t, time.September, 26, 10, 0, 6)) {
		t.Fatal("周六应不活跃")
	}
	if s.TradingDay(nyT(t, time.September, 27, 10, 0, 0)) {
		t.Fatal("周日非交易日（P1 weekday 粗判）")
	}
	if !s.TradingDay(nyT(t, time.September, 23, 3, 0, 3)) {
		t.Fatal("周三应是交易日")
	}
	// UTC 墙钟换算一致性：EDT=UTC-4，13:30Z=09:30 纽约。
	if !s.Active(time.Date(2026, 9, 23, 13, 35, 0, 0, time.UTC)) {
		t.Fatal("UTC 13:35 应为纽约盘中 09:35")
	}
	if s.Active(time.Date(2026, 9, 23, 11, 35, 0, 0, time.UTC)) {
		t.Fatal("UTC 11:35 应为纽约盘前 07:35")
	}
	// 冬令时（EST=UTC-5）自动偏移：1 月 11:35Z=06:35 盘前、13:35Z=08:35 仍盘前、14:35Z=09:35 盘中。
	if !s.Active(time.Date(2026, 1, 5, 14, 35, 0, 0, time.UTC)) {
		t.Fatal("冬令时 UTC 14:35 应为纽约盘中 09:35")
	}
}

// TestSessionUSVariants EXTENDED=04:00–20:00、24H=恒真、未知值回落 RTH。
func TestSessionUSVariants(t *testing.T) {
	ext := SessionUS("EXTENDED")
	if !ext.Active(nyT(t, time.September, 23, 4, 0, 3)) {
		t.Fatal("EXTENDED 04:00 应活跃")
	}
	if ext.Active(nyT(t, time.September, 23, 3, 59, 3)) {
		t.Fatal("EXTENDED 03:59 应不活跃")
	}
	if !ext.Active(nyT(t, time.September, 23, 19, 59, 3)) {
		t.Fatal("EXTENDED 19:59 应活跃")
	}
	if ext.Active(nyT(t, time.September, 23, 20, 0, 3)) {
		t.Fatal("EXTENDED 20:00 应不活跃")
	}
	h24 := SessionUS("24H")
	if !h24.Active(nyT(t, time.September, 26, 2, 0, 6)) {
		t.Fatal("24H 周六凌晨也应活跃")
	}
	// 未知/空值 → RTH 缺省（最保守）。
	for _, w := range []string{"", "rth", " bogus "} {
		v := SessionUS(w)
		if v.Active(nyT(t, time.September, 23, 9, 29, 3)) != false || !v.Active(nyT(t, time.September, 23, 10, 0, 3)) {
			t.Fatalf("tradingSession=%q 应回落 RTH 口径", w)
		}
	}
}

// TestNormalizeMarketKey 市场键归一化：大小写/去空格/未知值缺省 CN（加市场不换市场）。
func TestNormalizeMarketKey(t *testing.T) {
	cases := map[string]string{
		"":         "CN",
		" cn ":     "CN",
		"Crypto":   "CRYPTO",
		"crypto":   "CRYPTO",
		"us":       "US",
		"US":       "US",
		"JP":       "CN", // 未知市场缺省落 CN（与 store 空值口径一致）
		"BTCUSDT":  "CN", // 代码串不是市场键
		" CRYPTO ": "CRYPTO",
	}
	for in, want := range cases {
		if got := NormalizeMarketKey(in); got != want {
			t.Fatalf("NormalizeMarketKey(%q)=%s, want %s", in, got, want)
		}
	}
	// Session 对未知/空一律 CN 规则
	if _, ok := Session("").(cnSessionRule); !ok {
		t.Fatalf("Session(\"\") 应为 CN 规则")
	}
	if _, ok := Session("JP").(cnSessionRule); !ok {
		t.Fatalf("Session(未知) 应兜底 CN 规则")
	}
}
